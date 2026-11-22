package kubernetestarget

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// subjectFixture returns metadata for a disposable selected identity, never platform credentials.
// subjectFixture возвращает metadata одноразовой выбранной identity, не platform credentials.
func subjectFixture() authenticationv1.UserInfo {
	return authenticationv1.UserInfo{Username: "synthetic-personal-user", UID: "synthetic-user-uid",
		Groups: []string{"system:authenticated", "synthetic-developers"},
		Extra:  map[string]authenticationv1.ExtraValue{"synthetic.example/identity": {"second", "first"}}}
}

// subjectPin derives a disposable owner pin without printing identity data in failures.
// subjectPin получает disposable owner pin без вывода identity data при ошибках.
func subjectPin(t *testing.T) string {
	t.Helper()
	digest, err := SubjectDigest(subjectFixture())
	if err != nil {
		t.Fatal("derive disposable identity pin")
	}
	return digest
}

// TestAuthorizeUsesExactSelectedIdentity proves real TLS and precise checks for ordinary and UID stream RBAC.
// It does not prove a real account, an owner grant or stream availability.
// TestAuthorizeUsesExactSelectedIdentity проверяет настоящий TLS и exact checks ordinary/UID stream RBAC.
// Не доказывает настоящий account, owner grant либо доступность stream.
func TestAuthorizeUsesExactSelectedIdentity(t *testing.T) {
	requirements := []AccessRequirement{
		{Version: "v1", Resource: "pods", Namespace: "selected", Name: "selected-pod", Subresource: "exec", Verb: "create", Namespaced: true},
		{Version: "v1", Resource: "nodes", Name: "selected-node", Subresource: "proxy", Verb: "get"},
		{Discovery: managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryOpenAPISchema, Group: "apps", Version: "v1", ImmutableSchemaHash: strings.Repeat("f", 128)}, Verb: "get"},
	}
	for _, certificateIdentity := range []bool{false, true} {
		t.Run(map[bool]string{false: "token", true: "certificate"}[certificateIdentity], func(t *testing.T) {
			checked := 0
			handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
					t.Error("review used an unexpected request method or media type")
				}
				writer.Header().Set("Content-Type", "application/json")
				if request.URL.Path == "/apis/authentication.k8s.io/v1/selfsubjectreviews" {
					var input authenticationv1.SelfSubjectReview
					if json.NewDecoder(request.Body).Decode(&input) != nil || input.Kind != "SelfSubjectReview" || input.Status.UserInfo.Username != "" {
						t.Error("identity review injected a subject")
					}
					_ = json.NewEncoder(writer).Encode(authenticationv1.SelfSubjectReview{
						TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "SelfSubjectReview"},
						Status:   authenticationv1.SelfSubjectReviewStatus{UserInfo: subjectFixture()}})
					return
				}
				if request.URL.Path != "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews" || checked >= len(requirements) {
					t.Error("review reached an unregistered API path")
					writer.WriteHeader(http.StatusNotFound)
					return
				}
				var input authorizationv1.SelfSubjectAccessReview
				if json.NewDecoder(request.Body).Decode(&input) != nil {
					t.Error("review request is not JSON")
				}
				wanted := requirements[checked]
				expected := &authorizationv1.ResourceAttributes{Version: wanted.Version, Resource: wanted.Resource,
					Namespace: wanted.Namespace, Name: wanted.Name, Subresource: wanted.Subresource, Verb: wanted.Verb}
				if wanted.Discovery.Kind != "" {
					expectedNonResource := &authorizationv1.NonResourceAttributes{Path: "/openapi/v3/apis/apps/v1", Verb: "get"}
					if input.Kind != "SelfSubjectAccessReview" || input.Spec.ResourceAttributes != nil || !reflect.DeepEqual(input.Spec.NonResourceAttributes, expectedNonResource) {
						t.Error("review widened or changed the registered non-resource path")
					}
				} else if input.Kind != "SelfSubjectAccessReview" || input.Spec.NonResourceAttributes != nil || !reflect.DeepEqual(input.Spec.ResourceAttributes, expected) {
					t.Error("review widened or changed the selected resource check")
				}
				checked++
				input.Status.Allowed = true
				_ = json.NewEncoder(writer).Encode(input)
			})
			credential, calls := transportFixtureWithHandler(t, certificateIdentity, handler)
			if credential.Authorize(context.Background(), subjectPin(t), requirements) != nil || checked != len(requirements) || calls.Load() != int64(len(requirements)+1) {
				t.Fatal("exact selected-identity authorization failed")
			}
		})
	}
}

// TestAuthorizeFailsClosed covers changed identities, ambiguous reviews and explicit remote denials.
// TestAuthorizeFailsClosed проверяет changed identities, неоднозначные reviews и явные remote denials.
func TestAuthorizeFailsClosed(t *testing.T) {
	identity, _ := json.Marshal(authenticationv1.SelfSubjectReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "SelfSubjectReview"},
		Status:   authenticationv1.SelfSubjectReviewStatus{UserInfo: subjectFixture()}})
	for _, test := range []struct {
		name, identity, review, expected string
		status                           int
	}{
		{name: "changed identity", identity: strings.ReplaceAll(string(identity), "synthetic-personal-user", "foreign-personal-user"), expected: identityChanged},
		{name: "anonymous", identity: strings.ReplaceAll(string(identity), "synthetic-personal-user", "system:anonymous"), expected: accessUnknown},
		{name: "invalid UTF-8", identity: strings.ReplaceAll(string(identity), "synthetic-personal-user", "invalid-"+string([]byte{0xff})), expected: accessUnknown},
		{name: "duplicate field", review: `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":false,"allowed":true}}`, expected: accessUnknown},
		{name: "evaluation error", review: `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":true,"evaluationError":"synthetic private diagnostic"}}`, expected: accessUnknown},
		{name: "contradiction", review: `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":true,"denied":true}}`, expected: accessUnknown},
		{name: "denied", review: `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":false,"denied":true}}`, expected: accessDenied},
		{name: "no opinion", review: `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":false}}`, expected: accessUnknown},
		{name: "missing allowed", review: `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"denied":true}}`, expected: accessUnknown},
		{name: "null allowed", review: `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":null,"denied":true}}`, expected: accessUnknown},
		{name: "mismatched spec", review: `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","spec":{"resourceAttributes":{"verb":"get","resource":"secrets"}},"status":{"allowed":true}}`, expected: accessUnknown},
		{name: "wrong kind", review: `{"apiVersion":"authorization.k8s.io/v1","kind":"SubjectAccessReview","status":{"allowed":true}}`, expected: accessUnknown},
		{name: "unknown field", review: `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":true,"allowAll":true}}`, expected: accessUnknown},
		{name: "oversized", review: strings.Repeat(" ", maximumReviewBytes+1), expected: accessUnknown},
		{name: "trailing document", review: `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","status":{"allowed":true}}{}`, expected: accessUnknown},
		{name: "unauthenticated", status: http.StatusUnauthorized, expected: accessDenied},
		{name: "forbidden", status: http.StatusForbidden, expected: accessDenied},
		{name: "unavailable", status: http.StatusServiceUnavailable, expected: accessUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			credential, _ := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/apis/authentication.k8s.io/v1/selfsubjectreviews" {
					if test.identity != "" {
						_, _ = io.WriteString(writer, test.identity)
					} else {
						_, _ = writer.Write(identity)
					}
					return
				}
				if test.status != 0 {
					writer.WriteHeader(test.status)
				}
				_, _ = io.WriteString(writer, test.review)
			}))
			err := credential.Authorize(context.Background(), subjectPin(t), []AccessRequirement{{Version: "v1", Resource: "pods", Namespace: "selected", Name: "selected-pod", Verb: "get", Namespaced: true}})
			if err == nil || err.Error() != test.expected {
				t.Fatal("unproven or denied authorization was accepted or disclosed upstream details")
			}
		})
	}
}

// TestAuthorizationInputCannotWidenScope proves invalid tuples fail before any network request.
// TestAuthorizationInputCannotWidenScope доказывает отказ invalid tuples до сетевого запроса.
func TestAuthorizationInputCannotWidenScope(t *testing.T) {
	credential, calls := transportFixture(t, false)
	for _, requirement := range []AccessRequirement{
		{Version: "v1", Resource: "pods", Verb: "list", Namespaced: true},
		{Version: "v1", Resource: "pods", Namespace: "selected", Verb: "*", Namespaced: true},
		{Version: "v1", Resource: "*", Namespace: "selected", Verb: "list", Namespaced: true},
		{Version: "v1", Resource: "pods", Namespace: "selected", Name: "selected", Subresource: "proxy", Verb: "get", Namespaced: true},
		{Version: "v1", Resource: "nodes", Name: "selected", Subresource: "proxy", Verb: "patch"},
		{Version: "v1", Resource: "pods", Namespace: "selected", Name: "selected", Subresource: "token", Verb: "create", Namespaced: true},
	} {
		if credential.Authorize(context.Background(), subjectPin(t), []AccessRequirement{requirement}) == nil {
			t.Fatal("unsafe authorization tuple was accepted")
		}
	}
	if credential.Authorize(context.Background(), "", nil) == nil || calls.Load() != 0 {
		t.Fatal("invalid owner authorization input reached the API")
	}
}

// TestSubjectDigestCanonicalizesWithoutMutating proves stable group/extra pins and immutable caller data.
// TestSubjectDigestCanonicalizesWithoutMutating проверяет стабильные group/extra pins без изменения caller data.
func TestSubjectDigestCanonicalizesWithoutMutating(t *testing.T) {
	subject := subjectFixture()
	before := subject.DeepCopy()
	first, err := SubjectDigest(subject)
	if err != nil || !reflect.DeepEqual(&subject, before) {
		t.Fatal("subject digest mutated caller metadata")
	}
	subject.Groups[0], subject.Groups[1] = subject.Groups[1], subject.Groups[0]
	subject.Extra["synthetic.example/identity"][0], subject.Extra["synthetic.example/identity"][1] = "first", "second"
	second, err := SubjectDigest(subject)
	if err != nil || first != second {
		t.Fatal("subject ordering changed identity pin")
	}
	subject.Groups = append(subject.Groups, subject.Groups[0])
	if _, err = SubjectDigest(subject); err == nil {
		t.Fatal("ambiguous duplicate subject group was accepted")
	}
}

// TestAuthorizeNeverSubstitutesIdentityForMissingProxyPermission proves a successful Pod exec
// check cannot bypass a denied nodes/proxy check or trigger a privileged credential retry.
// TestAuthorizeNeverSubstitutesIdentityForMissingProxyPermission доказывает, что успешный Pod exec
// check не обходит denied nodes/proxy check и не запускает retry с privileged credential.
func TestAuthorizeNeverSubstitutesIdentityForMissingProxyPermission(t *testing.T) {
	checks := 0
	credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/apis/authentication.k8s.io/v1/selfsubjectreviews" {
			_ = json.NewEncoder(writer).Encode(authenticationv1.SelfSubjectReview{
				TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "SelfSubjectReview"},
				Status:   authenticationv1.SelfSubjectReviewStatus{UserInfo: subjectFixture()}})
			return
		}
		var review authorizationv1.SelfSubjectAccessReview
		if json.NewDecoder(request.Body).Decode(&review) != nil || review.Spec.ResourceAttributes == nil {
			t.Error("invalid compound review request")
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		checks++
		review.Status.Allowed = review.Spec.ResourceAttributes.Resource == "pods"
		review.Status.Denied = !review.Status.Allowed
		_ = json.NewEncoder(writer).Encode(review)
	}))
	err := credential.Authorize(context.Background(), subjectPin(t), []AccessRequirement{
		{Version: "v1", Resource: "pods", Namespace: "selected", Name: "selected-pod", Subresource: "exec", Verb: "create", Namespaced: true},
		{Version: "v1", Resource: "nodes", Name: "selected-node", Subresource: "proxy", Verb: "get"},
	})
	if err == nil || err.Error() != accessDenied || checks != 2 || calls.Load() != 3 {
		t.Fatal("missing proxy permission was bypassed or retried with another identity")
	}
}

// TestAuthorizeHonorsCanceledOwnerContext proves cancellation is observable and causes no API call.
// TestAuthorizeHonorsCanceledOwnerContext доказывает наблюдаемую отмену без API call.
func TestAuthorizeHonorsCanceledOwnerContext(t *testing.T) {
	credential, calls := transportFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := credential.Authorize(ctx, subjectPin(t), []AccessRequirement{
		{Version: "v1", Resource: "pods", Namespace: "selected", Name: "selected-pod", Verb: "get", Namespaced: true},
	})
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatal("canceled owner context was hidden or reached the target API")
	}
}

// TestDiscoveryAccessRequirementRefusesMixedAndUnsafeChecks rejects resource/non-resource union
// ambiguity before any selected credential can reach an API endpoint.
// TestDiscoveryAccessRequirementRefusesMixedAndUnsafeChecks отклоняет resource/non-resource union
// ambiguity до обращения выбранной credential к любому API endpoint.
func TestDiscoveryAccessRequirementRefusesMixedAndUnsafeChecks(t *testing.T) {
	base := AccessRequirement{Discovery: managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryVersion}, Verb: "get"}
	if base.Validate() != nil {
		t.Fatal("registered version read rejected")
	}
	for _, mutate := range []func(*AccessRequirement){
		func(value *AccessRequirement) { value.Resource = "secrets" },
		func(value *AccessRequirement) { value.Name = "selected" },
		func(value *AccessRequirement) { value.Group = "apps" },
		func(value *AccessRequirement) { value.Namespaced = true },
		func(value *AccessRequirement) { value.Namespace = "selected" },
		func(value *AccessRequirement) { value.Subresource = "proxy" },
		func(value *AccessRequirement) { value.Verb = "create" },
		func(value *AccessRequirement) { value.Discovery.Kind = "RAW" },
		func(value *AccessRequirement) { value.Discovery.Version = "v1/../../secrets" },
	} {
		changed := base
		mutate(&changed)
		if changed.Validate() == nil {
			t.Fatal("mixed or unsafe discovery access requirement accepted")
		}
	}
}
