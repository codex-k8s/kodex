package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// streamMaterialFixture supplies fresh owned copies without exposing the source material to callers.
// streamMaterialFixture выдаёт свежие owned copies без раскрытия source material caller.
type streamMaterialFixture struct {
	raw     []byte
	calls   atomic.Int32
	corrupt atomic.Bool
}

// ReadKubernetesIdentity simulates the exact purpose store after the native verifier checks its locator.
// ReadKubernetesIdentity моделирует exact purpose store после проверки locator native verifier.
func (reader *streamMaterialFixture) ReadKubernetesIdentity(context.Context, value.KubernetesIdentityLocator) ([]byte, error) {
	reader.calls.Add(1)
	if reader.corrupt.Load() {
		return []byte("synthetic invalid material"), nil
	}
	return bytes.Clone(reader.raw), nil
}

// streamCredentialBytes serializes only disposable fixture credentials for the actual strict loader.
// streamCredentialBytes сериализует только disposable fixture credentials для actual strict loader.
func streamCredentialBytes(t *testing.T, credential Credential) []byte {
	t.Helper()
	config, _, _ := credentialFixture(t)
	config.CurrentContext = credential.target.Context
	config.Contexts[0].Name = credential.target.Context
	config.Clusters[0].Cluster.Server = credential.target.ServerURL
	config.Clusters[0].Cluster.TLSName = credential.target.TLSServerName
	config.Clusters[0].Cluster.Authority = base64.StdEncoding.EncodeToString(credential.ca)
	config.Users[0].User.Token = credential.token
	config.Users[0].User.Certificate = ""
	config.Users[0].User.Key = ""
	if credential.token == "" {
		raw, err := x509.MarshalPKCS8PrivateKey(credential.cert.PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		defer clear(raw)
		config.Users[0].User.Key = base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}))
		var bundle []byte
		for _, der := range credential.cert.Certificate {
			bundle = append(bundle, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
		}
		config.Users[0].User.Certificate = base64.StdEncoding.EncodeToString(bundle)
	}
	return encodeCredential(t, config)
}

// boundAccessFixture serves real selected-identity TLS reviews and a finite current Pod document.
// boundAccessFixture обслуживает реальные selected-identity TLS reviews и finite current Pod document.
func boundAccessFixture(t *testing.T, certificate bool, scenario string) (*BoundPodStreamAccess, value.KubernetesOperationSnapshot, *streamMaterialFixture, *atomic.Bool, *atomic.Int64) {
	t.Helper()
	denied := &atomic.Bool{}
	podBody := `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"selected-pod","namespace":"selected","uid":"45889f43-30af-40ad-8524-571868dd0920","resourceVersion":"37"},"spec":{"nodeName":"synthetic-node","automountServiceAccountToken":false,"containers":[{"name":"app","image":"example.invalid/image:fixture"}]}}`
	switch scenario {
	case "pod UID":
		podBody = strings.Replace(podBody, "45889f43-30af-40ad-8524-571868dd0920", "55889f43-30af-40ad-8524-571868dd0920", 1)
	case "placement":
		podBody = strings.Replace(podBody, "synthetic-node", "other-node", 1)
	case "container":
		podBody = strings.Replace(podBody, `"name":"app"`, `"name":"other"`, 1)
	case "host access":
		podBody = strings.Replace(podBody, `"nodeName":`, `"hostNetwork":true,"nodeName":`, 1)
	case "service token":
		podBody = strings.Replace(podBody, `"automountServiceAccountToken":false`, `"automountServiceAccountToken":true`, 1)
	case "terminating":
		podBody = strings.Replace(podBody, `"resourceVersion":"37"`, `"resourceVersion":"37","deletionTimestamp":"2026-01-01T00:00:00Z"`, 1)
	case "duplicate":
		podBody = strings.Replace(podBody, `"resourceVersion":"37"`, `"resourceVersion":"37","resourceVersion":"38"`, 1)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/namespaces/kube-system":
			_ = json.NewEncoder(w).Encode(corev1.Namespace{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"}, ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: types.UID("629c395b-32aa-425b-814d-5cd9a9a42da6"), ResourceVersion: "9"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}})
		case "/apis/authentication.k8s.io/v1/selfsubjectreviews":
			subject := subjectFixture()
			if scenario == "subject" {
				subject.Username = "foreign"
			}
			_ = json.NewEncoder(w).Encode(authenticationv1.SelfSubjectReview{TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "SelfSubjectReview"}, Status: authenticationv1.SelfSubjectReviewStatus{UserInfo: subject}})
		case "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
			var review authorizationv1.SelfSubjectAccessReview
			if json.NewDecoder(r.Body).Decode(&review) != nil || review.Spec.ResourceAttributes == nil {
				t.Error("invalid access review")
				w.WriteHeader(400)
				return
			}
			a := review.Spec.ResourceAttributes
			if a.Resource == "nodes" {
				if a.Name != "synthetic-node" || a.Subresource != "proxy" || a.Verb != "get" || a.Namespace != "" {
					t.Error("node access tuple widened")
				}
			} else if a.Resource != "pods" || a.Namespace != "selected" || a.Name != "selected-pod" || a.Verb != "get" || (a.Subresource != "" && a.Subresource != "exec") {
				t.Error("Pod access tuple widened")
			}
			review.Status.Allowed = !(denied.Load() && a.Subresource == "proxy")
			review.Status.Denied = !review.Status.Allowed
			_ = json.NewEncoder(w).Encode(review)
		case "/api/v1/namespaces/selected/pods/selected-pod":
			if r.Method != "GET" {
				t.Error("Pod observation changed method")
			}
			_, _ = w.Write([]byte(podBody))
		default:
			t.Error("authorization opened an unregistered path or stream")
			w.WriteHeader(404)
		}
	})
	credential, calls := transportFixtureWithHandler(t, certificate, handler)
	pin := privilegedPinFixture(t, credential, time.Now())
	reader := &streamMaterialFixture{raw: streamCredentialBytes(t, credential)}
	t.Cleanup(func() { clear(reader.raw) })
	locator := identityLocatorFixture(t, reader.raw, credential.target)
	node := &value.KubernetesNodeTransport{Ref: pin.Ref, TargetRef: pin.TargetID, NodeName: pin.NodeName, NodeUID: pin.NodeUID, Generation: int64(pin.Generation), TargetRevision: int64(pin.TargetRevision), ServerOrigin: pin.ServerURL, TLSServerName: pin.TLSServerName, CertificateAuthoritySHA256: pin.CertificateAuthorityHash, ServerPublicKeySHA256: pin.ServerPublicKeyHash, CertificateAuthorityPEM: bytes.Clone(pin.CertificateAuthorityPEM), AddressIPs: append([]string(nil), pin.AddressPins...), NotBefore: pin.NotBefore, ExpiresAt: pin.ExpiresAt, Protocol: pin.Protocol}
	snapshot := value.KubernetesOperationSnapshot{Identity: locator, IdentityRef: locator.IdentityRef, SubjectDigest: subjectPin(t), Target: credential.target, NodeTransport: node, ExpiresAt: time.Now().Add(time.Minute), ProtectedNamespaces: []string{"kodex-system"}}
	if scenario == "protected namespace" {
		snapshot.ProtectedNamespaces = []string{"selected"}
	}
	pod := PodStreamTarget{Namespace: "selected", Name: "selected-pod", UID: "45889f43-30af-40ad-8524-571868dd0920", NodeName: pin.NodeName, NodeUID: pin.NodeUID, Container: "app"}
	access, err := NewBoundPodStreamAccess(reader, snapshot, "kubernetes.exec", pod, PodStreamOptions{Command: []string{"true"}, Stdout: true})
	if err != nil {
		t.Fatal("construct bound stream access")
	}
	return access, snapshot, reader, denied, calls
}

// TestBoundPodStreamAccessUsesFreshSelectedTLS verifies real TLS, exact RBAC and revocation without opening an effect.
// TestBoundPodStreamAccessUsesFreshSelectedTLS проверяет real TLS, exact RBAC и отзыв без открытия effect.
func TestBoundPodStreamAccessUsesFreshSelectedTLS(t *testing.T) {
	for _, certificate := range []bool{false, true} {
		t.Run(map[bool]string{false: "token", true: "certificate"}[certificate], func(t *testing.T) {
			access, snapshot, reader, denied, calls := boundAccessFixture(t, certificate, "")
			until, err := access.VerifyKubernetesStreamAccess(t.Context(), snapshot)
			if err != nil || !until.After(time.Now()) || until.After(time.Now().Add(5*time.Second)) || reader.calls.Load() != 1 || calls.Load() < 6 {
				t.Fatal("selected identity authorization failed")
			}
			denied.Store(true)
			if _, err = access.VerifyKubernetesStreamAccess(t.Context(), snapshot); err == nil || reader.calls.Load() != 2 {
				t.Fatal("revoked node RBAC retained stream access")
			}
		})
	}
}

// TestBoundPodStreamAccessRejectsChangedPod refuses stale placement, hidden credentials and malformed current evidence.
// TestBoundPodStreamAccessRejectsChangedPod отклоняет stale placement, hidden credentials и malformed current evidence.
func TestBoundPodStreamAccessRejectsChangedPod(t *testing.T) {
	for _, scenario := range []string{"pod UID", "placement", "container", "host access", "service token", "terminating", "duplicate", "subject", "protected namespace"} {
		t.Run(scenario, func(t *testing.T) {
			access, snapshot, _, _, _ := boundAccessFixture(t, false, scenario)
			if until, err := access.VerifyKubernetesStreamAccess(t.Context(), snapshot); err == nil || !until.IsZero() {
				t.Fatal("invalid current target acquired credit")
			}
		})
	}
}

// TestBoundPodStreamAccessPinsPrecedePrivateRead rejects locator/registry changes before credential or target access.
// TestBoundPodStreamAccessPinsPrecedePrivateRead отклоняет locator/registry changes до credential либо target access.
func TestBoundPodStreamAccessPinsPrecedePrivateRead(t *testing.T) {
	for _, scenario := range []string{"UID", "RV", "node generation", "node key", "target route", "expiry", "context"} {
		t.Run(scenario, func(t *testing.T) {
			access, snapshot, reader, _, calls := boundAccessFixture(t, false, "")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "UID":
				snapshot.Identity.UID = "55889f43-30af-40ad-8524-571868dd0920"
			case "RV":
				snapshot.Identity.ResourceVersion = "38"
			case "node generation":
				snapshot.NodeTransport.Generation++
			case "node key":
				snapshot.NodeTransport.ServerPublicKeySHA256 = strings.Repeat("d", 64)
			case "target route":
				snapshot.Target.ServerURL = "https://other.example.invalid"
			case "expiry":
				snapshot.ExpiresAt = time.Now().Add(-time.Second)
			case "context":
				cancel()
			}
			if _, err := access.VerifyKubernetesStreamAccess(ctx, snapshot); err == nil || reader.calls.Load() != 0 || calls.Load() != 0 {
				t.Fatal("changed private pins reached storage or target")
			}
		})
	}
	access, snapshot, reader, _, calls := boundAccessFixture(t, false, "")
	reader.corrupt.Store(true)
	if _, err := access.VerifyKubernetesStreamAccess(t.Context(), snapshot); err == nil || reader.calls.Load() != 1 || calls.Load() != 0 {
		t.Fatal("corrupt identity reached target or fallback")
	}
}

// TestPodStreamDeadlineBoundsCertificateAndPin preserves known validity limits without claiming token retention guarantees.
// TestPodStreamDeadlineBoundsCertificateAndPin сохраняет known validity limits без утверждений о token retention guarantees.
func TestPodStreamDeadlineBoundsCertificateAndPin(t *testing.T) {
	credential, _ := transportFixture(t, true)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.ExpiresAt = now.Add(time.Second)
	until, err := podStreamCredentialDeadline(credential, pin, now)
	if err != nil || !until.Equal(pin.ExpiresAt) {
		t.Fatal("node expiry not enforced")
	}
	if _, err = podStreamCredentialDeadline(credential, pin, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired certificates accepted")
	}
}
