package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

// maximumKubernetesIdentityAcknowledgementBytes bounds encoded material plus controlled API metadata.
// The extra byte distinguishes a truncated witness; raw body bytes never enter an owner receipt.
// maximumKubernetesIdentityAcknowledgementBytes ограничивает encoded material и controlled API metadata.
// Дополнительный byte отличает truncated witness; raw body bytes не входят в owner receipt.
const maximumKubernetesIdentityAcknowledgementBytes = maximumKubernetesIdentityBytes*2 + 65536

// kubernetesIdentityRequest consumes owned body bytes and permits only fixed private CoreV1 paths.
// It keeps the trusted TLS/auth transport and rate limiter but bypasses SDK body loggers, retries,
// redirects and cookie storage. This is not an arbitrary URL or general Kubernetes request port.
// kubernetesIdentityRequest принимает ownership body bytes и допускает только fixed private CoreV1 paths.
// Сохраняет trusted TLS/auth transport и rate limiter, исключая SDK body loggers, retries,
// redirects и cookie storage. Это не arbitrary URL либо general Kubernetes request port.
func (store *Store) kubernetesIdentityRequest(ctx context.Context, method, name string, owned []byte) (*http.Response, error) {
	body := newPrivateIdentityRequestBody(owned)
	defer body.Close()
	if ctx == nil || ctx.Err() != nil || len(validation.IsDNS1123Label(store.namespace)) != 0 ||
		(method != http.MethodGet && method != http.MethodPost) ||
		(method == http.MethodPost && name != "") || (method != http.MethodPost &&
		(!strings.HasPrefix(name, "kbi-") || len(validation.IsDNS1123Subdomain(name)) != 0)) {
		return nil, ErrKubernetesIdentityUnavailable
	}
	base, ok := store.client.CoreV1().RESTClient().(*rest.RESTClient)
	if !ok || base == nil || base.Client == nil || base.APIVersion() != corev1.SchemeGroupVersion {
		return nil, ErrKubernetesIdentityUnavailable
	}
	endpoint := base.Get().URL()
	if endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" ||
		endpoint.RawPath != "" || endpoint.Path != "/api/v1" ||
		(endpoint.RawQuery != "" && (base.Client.Timeout <= 0 || endpoint.RawQuery != (url.Values{"timeout": {base.Client.Timeout.String()}}).Encode())) {
		return nil, ErrKubernetesIdentityUnavailable
	}
	endpoint.RawQuery = ""
	endpoint.Path = path.Join(endpoint.Path, "namespaces", store.namespace, "secrets", name)
	client := *base.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Jar = nil
	if client.Timeout <= 0 || client.Timeout > kubernetesIdentityStorageTimeout {
		client.Timeout = kubernetesIdentityStorageTimeout
	}
	var reader io.Reader
	if owned != nil {
		reader = body
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return nil, ErrKubernetesIdentityUnavailable
	}
	request.GetBody = nil
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "kodex-secret-broker-kubernetes-identity")
	if owned != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if limiter := base.GetRateLimiter(); limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return nil, ErrKubernetesIdentityUnavailable
		}
	}
	response, err := client.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, ErrKubernetesIdentityCreateUnknown
	}
	return response, nil
}

// decodeKubernetesIdentityAcknowledgement strictly decodes a complete successful private API response.
// An invalid body returns a terminal bounded witness, never UNKNOWN or a partially trusted locator.
// decodeKubernetesIdentityAcknowledgement строго разбирает complete successful private API response.
// Invalid body возвращает terminal bounded witness, не UNKNOWN либо partially trusted locator.
func decodeKubernetesIdentityAcknowledgement(body []byte) (*corev1.Secret, value.KubernetesIdentityAcknowledgementRejection, error) {
	hash := sha256.Sum256(body)
	witness := value.KubernetesIdentityAcknowledgementRejection{ResponseSHA256: hex.EncodeToString(hash[:]),
		Truncated: len(body) > maximumKubernetesIdentityAcknowledgementBytes}
	if witness.Truncated {
		return nil, witness, &targetrepo.IdentityAcknowledgementRejection{Receipt: witness}
	}
	secret := new(corev1.Secret)
	object, _, err := serializer.NewCodecFactory(scheme.Scheme, serializer.EnableStrict).UniversalDeserializer().Decode(body, nil, secret)
	if err != nil || object != secret || secret.APIVersion != "v1" || secret.Kind != "Secret" {
		clearSecretData(secret)
		return nil, witness, &targetrepo.IdentityAcknowledgementRejection{Receipt: witness}
	}
	witness.ObservedUID = string(secret.UID)
	if !witness.Valid() {
		witness.ObservedUID = ""
	}
	return secret, witness, nil
}

// kubernetesIdentityJSONResponse accepts only a declared JSON response before material admission.
// kubernetesIdentityJSONResponse допускает только declared JSON response до material admission.
func kubernetesIdentityJSONResponse(response *http.Response) bool {
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

// readKubernetesIdentityObject bounds and clears wire bytes before returning an owned decoded object.
// Incomplete transport reads remain ambiguous; completed invalid successful responses are rejected.
// readKubernetesIdentityObject ограничивает и очищает wire bytes до возврата owned decoded object.
// Incomplete transport reads остаются ambiguous; completed invalid successful responses отклоняются.
func (store *Store) readKubernetesIdentityObject(ctx context.Context, name string) (*corev1.Secret, value.KubernetesIdentityAcknowledgementRejection, error) {
	response, err := store.kubernetesIdentityRequest(ctx, http.MethodGet, name, nil)
	if err != nil {
		return nil, value.KubernetesIdentityAcknowledgementRejection{}, ErrKubernetesIdentityUnavailable
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumKubernetesIdentityAcknowledgementBytes+1))
	defer clear(body)
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, value.KubernetesIdentityAcknowledgementRejection{}, ErrKubernetesIdentityUnavailable
	}
	secret, witness, err := decodeKubernetesIdentityAcknowledgement(body)
	if err != nil {
		return nil, witness, err
	}
	if response.StatusCode != http.StatusOK || !kubernetesIdentityJSONResponse(response) {
		clearSecretData(secret)
		return nil, witness, &targetrepo.IdentityAcknowledgementRejection{Receipt: witness}
	}
	return secret, witness, nil
}
