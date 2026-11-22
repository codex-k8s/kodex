package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	strictjson "sigs.k8s.io/json"
)

const (
	// accessUnknown rejects incomplete authentication or authorization without exposing API diagnostics.
	// accessUnknown отклоняет неполную authentication/authorization без раскрытия API diagnostics.
	accessUnknown = "managed Kubernetes identity access is unknown"
	// accessDenied distinguishes a proven API denial from an unavailable or malformed review.
	// accessDenied отличает подтверждённый API denial от недоступного либо неверного review.
	accessDenied = "managed Kubernetes identity access is denied"
	// identityChanged closes admission when the selected credential no longer resolves to its owner pin.
	// identityChanged закрывает допуск, когда выбранная credential больше не соответствует owner pin.
	identityChanged = "managed Kubernetes identity has changed"
	// maximumReviewBytes bounds upstream identity and access-review objects before JSON decoding.
	// maximumReviewBytes ограничивает upstream identity/access-review objects до JSON decoding.
	maximumReviewBytes = 64 << 10
)

// AccessRequirement represents one owner-resolved Kubernetes RBAC check, never an owner grant.
// Namespace scope and names are explicit; privileged stream checks must include both the ordinary
// Pod subresource and the registered nodes/proxy tuple using the very same selected credential.
// AccessRequirement задаёт один owner-resolved Kubernetes RBAC check, но не owner grant.
// Namespace и names явные; privileged streams требуют ordinary Pod subresource и
// зарегистрированный nodes/proxy tuple с той же выбранной credential.
type AccessRequirement struct {
	Discovery                                                    managedkubernetes.DiscoveryAddress
	Group, Version, Resource, Namespace, Name, Subresource, Verb string
	Namespaced                                                   bool
	AllNamespaces                                                bool
}

// Validate rejects wildcard, implicit all-namespace and arbitrary proxy authorization checks.
// This validates a check's shape; the executable operation registry must supply the required set.
// Validate запрещает wildcard, implicit all-namespace и произвольные proxy checks.
// Проверяет форму check; обязательный набор назначает executable operation registry.
func (requirement AccessRequirement) Validate() error {
	if requirement.Discovery != (managedkubernetes.DiscoveryAddress{}) {
		if requirement.Group != "" || requirement.Version != "" || requirement.Resource != "" || requirement.Namespace != "" || requirement.Name != "" || requirement.Subresource != "" || requirement.Namespaced || requirement.AllNamespaces || requirement.Verb != "get" {
			return errors.New(accessUnknown)
		}
		if _, err := requirement.Discovery.RelativeURL(); err != nil {
			return errors.New(accessUnknown)
		}
		return nil
	}
	address := ResourceAddress{Group: requirement.Group, Version: requirement.Version,
		Resource: requirement.Resource, Namespace: requirement.Namespace,
		Name: requirement.Name, Namespaced: requirement.Namespaced, AllNamespaces: requirement.AllNamespaces}
	if address.Validate() != nil {
		return errors.New(accessUnknown)
	}
	if requirement.AllNamespaces && requirement.Verb != "list" && requirement.Verb != "watch" {
		return errors.New(accessUnknown)
	}
	switch requirement.Verb {
	case "get", "list", "watch", "create", "update", "patch", "delete":
	default:
		return errors.New(accessUnknown)
	}
	if requirement.Name == "" && requirement.Verb != "list" && requirement.Verb != "watch" && requirement.Verb != "create" {
		return errors.New(accessUnknown)
	}
	if requirement.Subresource != "" {
		if requirement.Name == "" {
			return errors.New(accessUnknown)
		}
		switch requirement.Subresource {
		case "exec", "attach", "portforward", "log", "eviction", "binding", "ephemeralcontainers":
			if requirement.Group != "" || requirement.Version != "v1" || requirement.Resource != "pods" || !requirement.Namespaced {
				return errors.New(accessUnknown)
			}
		case "proxy":
			if requirement.Group != "" || requirement.Version != "v1" || requirement.Resource != "nodes" || requirement.Namespaced ||
				(requirement.Verb != "get" && requirement.Verb != "create") {
				return errors.New(accessUnknown)
			}
		case "status", "scale", "approval":
		default:
			return errors.New(accessUnknown)
		}
	}
	return nil
}

// SubjectDigest canonicalizes the full authenticated subject, including groups and extras.
// A digest is identity metadata, not authorization; callers must pin it in owner-controlled state.
// SubjectDigest канонизирует полный authenticated subject, включая groups и extras.
// Digest — identity metadata, не полномочия; caller закрепляет его в owner-controlled state.
func SubjectDigest(subject authenticationv1.UserInfo) (string, error) {
	if subject.Username == "" || subject.Username == "system:anonymous" || len(subject.Username) > 1024 ||
		!utf8.ValidString(subject.Username) || !utf8.ValidString(subject.UID) ||
		len(subject.UID) > 1024 || len(subject.Groups) > 128 || len(subject.Extra) > 128 {
		return "", errors.New(accessUnknown)
	}
	copy := subject.DeepCopy()
	slices.Sort(copy.Groups)
	for index, group := range copy.Groups {
		if group == "" || !utf8.ValidString(group) || len(group) > 1024 || group == "system:unauthenticated" || (index > 0 && copy.Groups[index-1] == group) {
			return "", errors.New(accessUnknown)
		}
	}
	for key, values := range copy.Extra {
		if key == "" || !utf8.ValidString(key) || len(key) > 1024 || len(values) > 128 {
			return "", errors.New(accessUnknown)
		}
		slices.Sort(values)
		for index, value := range values {
			if !utf8.ValidString(value) || len(value) > 4096 || (index > 0 && values[index-1] == value) {
				return "", errors.New(accessUnknown)
			}
		}
	}
	raw, err := json.Marshal(copy)
	if err != nil || len(raw) > maximumReviewBytes || !utf8.Valid(raw) {
		return "", errors.New(accessUnknown)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// ObserveIdentity records the actual authenticated subject after verifying the selected cluster.
// It proves neither resource RBAC nor a future execution grant; every operation still calls Authorize.
// ObserveIdentity получает actual authenticated subject после проверки выбранного cluster.
// Не доказывает resource RBAC либо future execution grant; каждая операция всё ещё вызывает Authorize.
func (credential Credential) ObserveIdentity(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", errors.New(accessUnknown)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := credential.VerifyClusterIdentity(ctx); err != nil {
		return "", err
	}
	client, closeIdle, err := credential.NewClient(5 * time.Second)
	if err != nil {
		return "", errors.New(accessUnknown)
	}
	defer closeIdle()
	return credential.observeSubject(ctx, client)
}

// observeSubject resolves only SelfSubjectReview over the already selected private TLS client.
// No username, groups, impersonation or caller-provided subject enters its request.
// observeSubject разрешает только SelfSubjectReview через уже выбранный private TLS client.
// Username, groups, impersonation и caller-provided subject не входят в request.
func (credential Credential) observeSubject(ctx context.Context, client *http.Client) (string, error) {
	identity, err := credential.observeSubjectInfo(ctx, client)
	if err != nil {
		return "", err
	}
	return SubjectDigest(identity)
}

// observeSubjectInfo reads and validates the selected authenticated identity without accepting impersonation or exposing raw response diagnostics.
// observeSubjectInfo читает и проверяет выбранную authenticated identity без impersonation и выдачи raw response diagnostics.
func (credential Credential) observeSubjectInfo(ctx context.Context, client *http.Client) (authenticationv1.UserInfo, error) {
	identity := authenticationv1.SelfSubjectReview{}
	if err := credential.review(ctx, client, "/apis/authentication.k8s.io/v1/selfsubjectreviews",
		&authenticationv1.SelfSubjectReview{TypeMeta: metav1.TypeMeta{APIVersion: "authentication.k8s.io/v1", Kind: "SelfSubjectReview"}}, &identity); err != nil {
		return authenticationv1.UserInfo{}, err
	}
	if identity.APIVersion != "authentication.k8s.io/v1" || identity.Kind != "SelfSubjectReview" {
		return authenticationv1.UserInfo{}, errors.New(accessUnknown)
	}
	if _, err := SubjectDigest(identity.Status.UserInfo); err != nil {
		return authenticationv1.UserInfo{}, err
	}
	return identity.Status.UserInfo, nil
}

// Authorize verifies the pinned subject and every exact access requirement through one private
// selected-identity client. No review is cached and no service identity or impersonation is used.
// A successful return proves only fresh Kubernetes review responses: it does not issue an owner
// grant, prove a UID precondition or authorize a later effect after its lease has changed.
// Authorize проверяет pinned subject и каждый exact access requirement одним private client
// выбранной identity. Review не кэшируется; service identity и impersonation не используются.
// Успех доказывает только свежие Kubernetes reviews, но не выдаёт owner grant, не доказывает
// UID precondition и не разрешает поздний effect после изменения lease.
func (credential Credential) Authorize(ctx context.Context, subjectDigest string, requirements []AccessRequirement) error {
	decoded, err := hex.DecodeString(subjectDigest)
	if ctx == nil || err != nil || len(decoded) != sha256.Size || strings.ToLower(subjectDigest) != subjectDigest ||
		len(requirements) == 0 || len(requirements) > 32 {
		return errors.New(accessUnknown)
	}
	requirements = slices.Clone(requirements)
	for _, requirement := range requirements {
		if requirement.Validate() != nil {
			return errors.New(accessUnknown)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, closeIdle, err := credential.NewClient(30 * time.Second)
	if err != nil {
		return errors.New(accessUnknown)
	}
	defer closeIdle()
	actual, err := credential.observeSubject(ctx, client)
	if err != nil {
		return err
	}
	if actual != subjectDigest {
		return errors.New(identityChanged)
	}
	for _, requirement := range requirements {
		attributes := &authorizationv1.ResourceAttributes{Group: requirement.Group, Version: requirement.Version,
			Resource: requirement.Resource, Namespace: requirement.Namespace, Name: requirement.Name,
			Subresource: requirement.Subresource, Verb: requirement.Verb}
		input := &authorizationv1.SelfSubjectAccessReview{TypeMeta: metav1.TypeMeta{
			APIVersion: "authorization.k8s.io/v1", Kind: "SelfSubjectAccessReview"},
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: attributes}}
		if requirement.Discovery != (managedkubernetes.DiscoveryAddress{}) {
			location, locationErr := requirement.Discovery.RelativeURL()
			if locationErr != nil {
				return errors.New(accessUnknown)
			}
			input.Spec.ResourceAttributes = nil
			input.Spec.NonResourceAttributes = &authorizationv1.NonResourceAttributes{Path: location.Path, Verb: "get"}
		}
		response := authorizationv1.SelfSubjectAccessReview{}
		if err = credential.review(ctx, client, "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews", input, &response); err != nil {
			return err
		}
		if response.APIVersion != input.APIVersion || response.Kind != input.Kind || response.Status.EvaluationError != "" ||
			(response.Status.Allowed && response.Status.Denied) ||
			(!response.Status.Allowed && !response.Status.Denied) ||
			(response.Status.Allowed && !reflect.DeepEqual(input.Spec, response.Spec)) {
			return errors.New(accessUnknown)
		}
		if !response.Status.Allowed {
			return errors.New(accessDenied)
		}
	}
	return nil
}

// review posts only a fixed authentication/authorization resource and rejects oversized,
// ambiguous or malformed upstream JSON before its status can influence admission.
// review отправляет только фиксированный authentication/authorization resource и отвергает
// oversized, неоднозначный либо неверный JSON до использования status для допуска.
func (credential Credential) review(ctx context.Context, client *http.Client, path string, input, output any) error {
	if path != "/apis/authentication.k8s.io/v1/selfsubjectreviews" && path != "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews" {
		return errors.New(accessUnknown)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return errors.New(accessUnknown)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, credential.target.ServerURL+path, bytes.NewReader(raw))
	if err != nil {
		return errors.New(accessUnknown)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New(accessUnknown)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return errors.New(accessDenied)
	}
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return errors.New(accessUnknown)
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, maximumReviewBytes+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || len(raw) > maximumReviewBytes || !utf8.Valid(raw) {
		return errors.New(accessUnknown)
	}
	strictErrors, err := strictjson.UnmarshalStrict(raw, output)
	if err != nil || len(strictErrors) > 0 {
		return errors.New(accessUnknown)
	}
	if _, accessReview := output.(*authorizationv1.SelfSubjectAccessReview); accessReview {
		// Allowed is required upstream; a zero Go boolean cannot distinguish omission from denial.
		// Allowed обязателен upstream; нулевой Go boolean не отличает omission от denial.
		var required struct {
			Status struct {
				Allowed *bool `json:"allowed"`
			} `json:"status"`
		}
		if json.Unmarshal(raw, &required) != nil || required.Status.Allowed == nil {
			return errors.New(accessUnknown)
		}
	}
	return nil
}
