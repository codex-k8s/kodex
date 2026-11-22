package kubernetes

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrKubernetesIdentityCreateUnknown retains possible external materialization after a lost ACK.
// It never authorizes retry, overwrite, generic materialization or a different identity revision.
// ErrKubernetesIdentityCreateUnknown сохраняет возможную materialization после потери ACK.
// Не разрешает повтор, overwrite, общий materialization либо другую identity revision.
var ErrKubernetesIdentityCreateUnknown = targetrepo.ErrIdentityCreateUnknown

// CreateKubernetesIdentity attempts one purpose-only immutable create after owner admission.
// The service must already have parsed these bytes against its exact owner target and persisted
// one-shot effect intent. This storage adapter never guesses owner authority or retries a write.
// CreateKubernetesIdentity делает один purpose-only immutable create после owner admission.
// Service заранее разбирает байты по exact owner target и сохраняет одноразовый effect intent.
// Storage adapter не предполагает owner authority и не повторяет запись.
func (store *Store) CreateKubernetesIdentity(ctx context.Context, provision value.KubernetesIdentityProvision, material []byte) (value.KubernetesIdentityLocator, error) {
	if ctx == nil || provision.Validate(store.namespace) != nil || len(material) == 0 || len(material) > maximumKubernetesIdentityBytes {
		return value.KubernetesIdentityLocator{}, value.ErrKubernetesIdentityLocator
	}
	if err := ctx.Err(); err != nil {
		return value.KubernetesIdentityLocator{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, kubernetesIdentityStorageTimeout)
	defer cancel()
	digest := sha256.Sum256(material)
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(digest[:])), []byte(provision.ContentSHA256)) != 1 {
		return value.KubernetesIdentityLocator{}, value.ErrKubernetesIdentityLocator
	}
	name, err := value.KubernetesIdentitySecretName(provision.IdentityRef, provision.Revision)
	if err != nil {
		return value.KubernetesIdentityLocator{}, value.ErrKubernetesIdentityLocator
	}
	immutable := true
	secret := &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Namespace: provision.Namespace, Name: name,
		Labels: map[string]string{kubernetesIdentityPurposeLabel: "KUBERNETES_TARGET"},
		Annotations: map[string]string{kubernetesIdentityRefAnnotation: provision.IdentityRef, kubernetesIdentityTargetAnnotation: provision.TargetRef,
			kubernetesIdentityRevisionAnnotation: strconv.FormatInt(provision.Revision, 10), kubernetesIdentityDigestAnnotation: provision.ContentSHA256}},
		Type: corev1.SecretTypeOpaque, Immutable: &immutable, Data: map[string][]byte{kubernetesIdentityDataKey: append([]byte(nil), material...)}}
	defer clearSecretData(secret)
	wire, err := json.Marshal(secret)
	if err != nil {
		return value.KubernetesIdentityLocator{}, value.ErrKubernetesIdentityLocator
	}
	// The fixed private HTTP path consumes and clears wire bytes without SDK request/response logging.
	// Fixed private HTTP path принимает и очищает wire bytes без SDK request/response logging.
	response, err := store.kubernetesIdentityRequest(ctx, http.MethodPost, "", wire)
	if err != nil {
		return value.KubernetesIdentityLocator{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumKubernetesIdentityAcknowledgementBytes+1))
	defer clear(body)
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return value.KubernetesIdentityLocator{}, ErrKubernetesIdentityCreateUnknown
	}
	created, witness, err := decodeKubernetesIdentityAcknowledgement(body)
	defer clearSecretData(created)
	if err != nil {
		return value.KubernetesIdentityLocator{}, err
	}
	if response.StatusCode != http.StatusCreated || !kubernetesIdentityJSONResponse(response) {
		return value.KubernetesIdentityLocator{}, &targetrepo.IdentityAcknowledgementRejection{Receipt: witness}
	}
	locator, err := kubernetesIdentityLocatorFromSecret(created, provision)
	if err != nil {
		return value.KubernetesIdentityLocator{}, &targetrepo.IdentityAcknowledgementRejection{Receipt: witness}
	}
	return locator, nil
}

// ObserveKubernetesIdentity reconciles only the exact owner-admitted purpose name without effects.
// A missing, replaced, mutable or foreign object stays unavailable; it never triggers creation.
// ObserveKubernetesIdentity сверяет только exact owner-admitted purpose name без effects.
// Отсутствие, подмена, mutable либо foreign object остаются unavailable и не запускают create.
func (store *Store) ObserveKubernetesIdentity(ctx context.Context, provision value.KubernetesIdentityProvision) (value.KubernetesIdentityLocator, error) {
	if ctx == nil || provision.Validate(store.namespace) != nil {
		return value.KubernetesIdentityLocator{}, value.ErrKubernetesIdentityLocator
	}
	if err := ctx.Err(); err != nil {
		return value.KubernetesIdentityLocator{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, kubernetesIdentityStorageTimeout)
	defer cancel()
	name, err := value.KubernetesIdentitySecretName(provision.IdentityRef, provision.Revision)
	if err != nil {
		return value.KubernetesIdentityLocator{}, value.ErrKubernetesIdentityLocator
	}
	secret, witness, err := store.readKubernetesIdentityObject(ctx, name)
	defer clearSecretData(secret)
	if err != nil {
		return value.KubernetesIdentityLocator{}, err
	}
	locator, err := kubernetesIdentityLocatorFromSecret(secret, provision)
	if err != nil {
		return value.KubernetesIdentityLocator{}, &targetrepo.IdentityAcknowledgementRejection{Receipt: witness}
	}
	return locator, nil
}
