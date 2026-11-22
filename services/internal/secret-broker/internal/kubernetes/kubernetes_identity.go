package kubernetes

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
)

const (
	// kubernetesIdentityPurposeLabel separates private kubeconfig material from runtime projections.
	// kubernetesIdentityPurposeLabel отделяет private kubeconfig material от runtime projections.
	kubernetesIdentityPurposeLabel = "broker-credentials.kodex.dev/purpose"
	// kubernetesIdentityRefAnnotation binds immutable bytes to the exact broker identity revision.
	// kubernetesIdentityRefAnnotation связывает immutable bytes с exact broker identity revision.
	kubernetesIdentityRefAnnotation = "broker-credentials.kodex.dev/identity-ref"
	// kubernetesIdentityTargetAnnotation forbids using the same material under a foreign target.
	// kubernetesIdentityTargetAnnotation запрещает использование material под foreign target.
	kubernetesIdentityTargetAnnotation = "broker-credentials.kodex.dev/target-ref"
	// kubernetesIdentityRevisionAnnotation records the owner's immutable credential revision.
	// kubernetesIdentityRevisionAnnotation хранит immutable credential revision владельца.
	kubernetesIdentityRevisionAnnotation = "broker-credentials.kodex.dev/revision"
	// kubernetesIdentityDigestAnnotation is checked against both owner pins and actual private bytes.
	// kubernetesIdentityDigestAnnotation сверяется с owner pins и actual private bytes.
	kubernetesIdentityDigestAnnotation = "broker-credentials.kodex.dev/content-sha256"
	// kubernetesIdentityDataKey is fixed by purpose, never selected through gateway input.
	// kubernetesIdentityDataKey фиксирован purpose, не выбирается через gateway input.
	kubernetesIdentityDataKey = "kubeconfig"
	// maximumKubernetesIdentityBytes matches the strict embedded-only kubeconfig parser budget.
	// maximumKubernetesIdentityBytes совпадает с бюджетом strict embedded-only kubeconfig parser.
	maximumKubernetesIdentityBytes = managedkubernetes.MaximumCredentialBytes
	// kubernetesIdentityStorageTimeout caps each private storage call below the owner setup lease.
	// kubernetesIdentityStorageTimeout ограничивает каждый private storage call внутри owner setup lease.
	kubernetesIdentityStorageTimeout = 10 * time.Second
)

// ErrKubernetesIdentityUnavailable conceals Kubernetes errors and private credential metadata.
// ErrKubernetesIdentityUnavailable скрывает Kubernetes errors и private credential metadata.
var ErrKubernetesIdentityUnavailable = errors.New("managed Kubernetes private identity is unavailable")

// ReadKubernetesIdentity reads only a separate exact purpose object after fresh owner resolution.
// Generic runtime materialization labels, mutable/replaced objects and extra data keys are rejected.
// Returned bytes stay inside the broker; the caller must clear them after private parsing or use.
// ReadKubernetesIdentity читает только separate exact purpose object после fresh owner resolution.
// Generic runtime labels, mutable/replaced objects и extra data keys отклоняются.
// Байты остаются внутри broker; caller очищает их после private parsing либо использования.
func (store *Store) ReadKubernetesIdentity(ctx context.Context, locator value.KubernetesIdentityLocator) ([]byte, error) {
	if ctx == nil {
		return nil, ErrKubernetesIdentityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if locator.Validate(store.namespace) != nil {
		return nil, value.ErrKubernetesIdentityLocator
	}
	ctx, cancel := context.WithTimeout(ctx, kubernetesIdentityStorageTimeout)
	defer cancel()
	secret, _, err := store.readKubernetesIdentityObject(ctx, locator.Name)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrKubernetesIdentityUnavailable
	}
	defer clearSecretData(secret)
	if secret == nil {
		return nil, ErrKubernetesIdentityUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	provision := value.KubernetesIdentityProvision{IdentityRef: locator.IdentityRef, TargetRef: locator.TargetRef,
		Namespace: locator.Namespace, Revision: locator.Revision, ContentSHA256: locator.ContentSHA256}
	actual, err := kubernetesIdentityLocatorFromSecret(secret, provision)
	if err != nil || actual != locator {
		return nil, ErrKubernetesIdentityUnavailable
	}
	return append([]byte(nil), secret.Data[kubernetesIdentityDataKey]...), nil
}

// kubernetesIdentityLocatorFromSecret verifies exact purpose, immutable topology and actual content.
// It returns metadata only; callers clear the private response object on every outcome.
// kubernetesIdentityLocatorFromSecret проверяет exact purpose, immutable topology и actual content.
// Возвращает только metadata; caller очищает приватный response object при любом результате.
func kubernetesIdentityLocatorFromSecret(secret *corev1.Secret, provision value.KubernetesIdentityProvision) (value.KubernetesIdentityLocator, error) {
	if secret == nil || provision.Validate(provision.Namespace) != nil {
		return value.KubernetesIdentityLocator{}, ErrKubernetesIdentityUnavailable
	}
	name, err := value.KubernetesIdentitySecretName(provision.IdentityRef, provision.Revision)
	_, genericProjection := secret.Labels[managedLabel]
	material := secret.Data[kubernetesIdentityDataKey]
	if err != nil || secret.Namespace != provision.Namespace || secret.Name != name ||
		secret.DeletionTimestamp != nil || secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque ||
		secret.Labels[kubernetesIdentityPurposeLabel] != "KUBERNETES_TARGET" || genericProjection ||
		secret.Annotations[kubernetesIdentityRefAnnotation] != provision.IdentityRef || secret.Annotations[kubernetesIdentityTargetAnnotation] != provision.TargetRef ||
		secret.Annotations[kubernetesIdentityRevisionAnnotation] != strconv.FormatInt(provision.Revision, 10) ||
		secret.Annotations[kubernetesIdentityDigestAnnotation] != provision.ContentSHA256 ||
		len(secret.OwnerReferences) != 0 || len(secret.Data) != 1 || len(secret.StringData) != 0 || len(material) == 0 || len(material) > maximumKubernetesIdentityBytes {
		return value.KubernetesIdentityLocator{}, ErrKubernetesIdentityUnavailable
	}
	digest := sha256.Sum256(material)
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(digest[:])), []byte(provision.ContentSHA256)) != 1 {
		return value.KubernetesIdentityLocator{}, ErrKubernetesIdentityUnavailable
	}
	locator := value.KubernetesIdentityLocator{IdentityRef: provision.IdentityRef, TargetRef: provision.TargetRef,
		Namespace: provision.Namespace, Name: name, Revision: provision.Revision, ContentSHA256: provision.ContentSHA256,
		UID: string(secret.UID), ResourceVersion: secret.ResourceVersion}
	if locator.Validate(provision.Namespace) != nil {
		return value.KubernetesIdentityLocator{}, ErrKubernetesIdentityUnavailable
	}
	return locator, nil
}
