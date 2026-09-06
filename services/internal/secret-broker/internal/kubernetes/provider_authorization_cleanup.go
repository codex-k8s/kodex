package kubernetes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ProviderCleanupAuthorization = "AUTHORIZATION_ATTEMPT"
	ProviderCleanupAbsence       = "AUTHORIZATION_ABSENCE"
	ProviderAuthorizationPresent = "PRESENT"
	ProviderAuthorizationAbsent  = "ABSENT_UNFENCED"
	ProviderAuthorizationFenced  = "CONFIRMED_ABSENT"
	providerCleanupFenceLabel    = "provider-credentials.kodex.dev/cleanup-fence"
	providerCleanupFenceKey      = "fence.json"
	providerCleanupFenceSchema   = "kodex.provider-cleanup-fence.v1"
	providerCleanupFenceMaximum  = 8 << 10
)

type ProviderAuthorizationCleanupTarget struct {
	TaskRef, AccountRef, AuthorizationAttemptRef, MaterializerAttemptRef string
	Kind, UID, ResourceVersion                                           string
	Generation                                                           int64
}

type ProviderAuthorizationCleanupObservation struct {
	State              string
	Target             ProviderAuthorizationCleanupTarget
	ProducedCredential *ProviderCredentialDescriptor
}

type ProviderCredentialCleanupResult struct {
	TerminalReceipt    string
	ProducedCredential *ProviderCredentialDescriptor
}

// Fence содержит только provenance, не прежний auth/user-code material.
// Имя immutable объекта остаётся занятым и после удаления его material.
type providerCleanupFence struct {
	Schema, Kind, AccountRef, AuthorizationAttemptRef, MaterializerAttemptRef string
	SecretName                                                                string
	OriginalUID, OriginalResourceVersion                                      string
}

func validAuthorizationCleanupIdentity(target ProviderAuthorizationCleanupTarget) bool {
	digest := sha256.Sum256([]byte(target.AuthorizationAttemptRef + "\x00" + target.AccountRef))
	return validProviderCleanupReference(target.TaskRef, "pcct_") &&
		validProviderCleanupReference(target.AccountRef, "pacc_") &&
		validProviderCleanupReference(target.AuthorizationAttemptRef, "pauth_") && target.Generation > 0 &&
		target.MaterializerAttemptRef == "pmat_"+hex.EncodeToString(digest[:16])
}

func validAuthorizationCleanupTarget(target ProviderAuthorizationCleanupTarget) bool {
	if !validAuthorizationCleanupIdentity(target) {
		return false
	}
	switch target.Kind {
	case ProviderCleanupAuthorization:
		return providerCredentialUIDPattern.MatchString(target.UID) && validProviderResourceVersion(target.ResourceVersion)
	case ProviderCleanupAbsence:
		return target.UID == "" && target.ResourceVersion == ""
	default:
		return false
	}
}

func (store *Store) ObserveProviderAuthorizationCleanup(ctx context.Context, target ProviderAuthorizationCleanupTarget) (ProviderAuthorizationCleanupObservation, error) {
	result := ProviderAuthorizationCleanupObservation{Target: target}
	if !validAuthorizationCleanupIdentity(target) || target.Kind != "" || target.UID != "" || target.ResourceVersion != "" {
		return ProviderAuthorizationCleanupObservation{}, ErrProviderCredentialCleanupInvalid
	}
	name, _ := providerAttemptName(target.MaterializerAttemptRef)
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		result.State, result.Target.Kind = ProviderAuthorizationAbsent, ProviderCleanupAbsence
	case err != nil:
		return ProviderAuthorizationCleanupObservation{}, errors.New("read authorization cleanup metadata")
	default:
		defer clearSecretData(secret)
		if secret.Labels[providerCleanupFenceLabel] != "" {
			fence, err := store.parseProviderCleanupFence(secret, "AUTHORIZATION", target.AccountRef)
			if err != nil || !fenceMatchesAuthorization(fence, target) {
				return ProviderAuthorizationCleanupObservation{}, ErrProviderCredentialCleanupConflict
			}
			result.State, result.Target.Kind = ProviderAuthorizationFenced, ProviderCleanupAbsence
		} else {
			attempt, err := providerAttemptFromSecret(secret)
			if err != nil || attempt.AccountRef != target.AccountRef || attempt.AttemptRef != target.AuthorizationAttemptRef ||
				attempt.MaterializerAttemptRef != target.MaterializerAttemptRef || secret.Namespace != store.namespace || secret.Name != name {
				return ProviderAuthorizationCleanupObservation{}, ErrProviderCredentialCleanupConflict
			}
			result.State, result.Target.Kind = ProviderAuthorizationPresent, ProviderCleanupAuthorization
			result.Target.UID, result.Target.ResourceVersion = attempt.SecretUID, attempt.ResourceVersion
		}
	}
	result.ProducedCredential, err = store.readAuthorizationProducedCredential(ctx, target, true)
	if err != nil {
		return ProviderAuthorizationCleanupObservation{}, err
	}
	// Один pending tombstone ещё не доказывает, что credential name fenced.
	if result.State == ProviderAuthorizationFenced && result.ProducedCredential == nil {
		credentialName := providerCredentialName(target.AccountRef, target.AuthorizationAttemptRef)
		credentialFence, getErr := store.client.CoreV1().Secrets(store.namespace).Get(ctx, credentialName, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			result.State = ProviderAuthorizationAbsent
			return result, nil
		}
		if getErr != nil {
			return ProviderAuthorizationCleanupObservation{}, ErrProviderCredentialCleanupConflict
		}
		defer clearSecretData(credentialFence)
		if _, err := store.parseProviderCleanupFence(credentialFence, "CREDENTIAL", target.AccountRef); err != nil {
			return ProviderAuthorizationCleanupObservation{}, err
		}
	}
	return result, nil
}

// FenceProviderAuthorization заменяет только exact mutable attempt. При
// подтверждённом отсутствии разрешён исключительно CAS Create tombstone.
func (store *Store) FenceProviderAuthorization(ctx context.Context, target ProviderAuthorizationCleanupTarget) error {
	if !validAuthorizationCleanupTarget(target) {
		return ErrProviderCredentialCleanupInvalid
	}
	name, _ := providerAttemptName(target.MaterializerAttemptRef)
	current, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("read authorization before fencing")
	}
	if apierrors.IsNotFound(err) {
		current = nil
	}
	if current != nil {
		defer clearSecretData(current)
		if current.Labels[providerCleanupFenceLabel] != "" {
			fence, err := store.parseProviderCleanupFence(current, "AUTHORIZATION", target.AccountRef)
			if err != nil || !fenceMatchesAuthorization(fence, target) ||
				(target.Kind == ProviderCleanupAuthorization && (fence.OriginalUID != target.UID || fence.OriginalResourceVersion != target.ResourceVersion)) {
				return ErrProviderCredentialCleanupConflict
			}
			return nil
		}
		attempt, parseErr := providerAttemptFromSecret(current)
		if parseErr != nil || current.Namespace != store.namespace || current.Name != name ||
			attempt.AccountRef != target.AccountRef || attempt.AttemptRef != target.AuthorizationAttemptRef ||
			attempt.MaterializerAttemptRef != target.MaterializerAttemptRef {
			return ErrProviderCredentialCleanupConflict
		}
		if target.Kind != ProviderCleanupAuthorization || attempt.SecretUID != target.UID || attempt.ResourceVersion != target.ResourceVersion {
			return ErrProviderAuthorizationCleanupSnapshotChanged
		}
	} else if target.Kind != ProviderCleanupAbsence {
		return ErrProviderAuthorizationCleanupSnapshotChanged
	}
	wanted := store.providerCleanupFenceSecret(name, providerCleanupFence{
		Schema: providerCleanupFenceSchema, Kind: "AUTHORIZATION", AccountRef: target.AccountRef,
		AuthorizationAttemptRef: target.AuthorizationAttemptRef, MaterializerAttemptRef: target.MaterializerAttemptRef,
		OriginalUID: target.UID, OriginalResourceVersion: target.ResourceVersion,
	})
	defer clearSecretData(wanted)
	var written *corev1.Secret
	if current == nil {
		written, err = store.client.CoreV1().Secrets(store.namespace).Create(ctx, wanted, metav1.CreateOptions{})
	} else {
		wanted.UID, wanted.ResourceVersion = current.UID, current.ResourceVersion
		written, err = store.client.CoreV1().Secrets(store.namespace).Update(ctx, wanted, metav1.UpdateOptions{})
	}
	// API Conflict/AlreadyExists отвергают запись. Ошибка ответа после
	// совершённого Update не даёт такого доказательства и остаётся общей.
	if apierrors.IsAlreadyExists(err) || apierrors.IsConflict(err) {
		return ErrProviderAuthorizationCleanupSnapshotChanged
	}
	if apierrors.IsInvalid(err) {
		return ErrProviderCredentialCleanupConflict
	}
	if err != nil {
		return errors.New("persist authorization cleanup fence")
	}
	defer clearSecretData(written)
	fence, err := store.parseProviderCleanupFence(written, "AUTHORIZATION", target.AccountRef)
	if err != nil || !fenceMatchesAuthorization(fence, target) || fence.OriginalUID != target.UID || fence.OriginalResourceVersion != target.ResourceVersion {
		return ErrProviderCredentialCleanupConflict
	}
	return nil
}

func (store *Store) FenceAuthorizationCredentialName(ctx context.Context, target ProviderAuthorizationCleanupTarget) (*ProviderCredentialDescriptor, error) {
	if !validAuthorizationCleanupTarget(target) {
		return nil, ErrProviderCredentialCleanupInvalid
	}
	pendingName, _ := providerAttemptName(target.MaterializerAttemptRef)
	pending, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, pendingName, metav1.GetOptions{})
	if err != nil {
		return nil, errors.New("read authorization cleanup fence before credential fencing")
	}
	defer clearSecretData(pending)
	fence, err := store.parseProviderCleanupFence(pending, "AUTHORIZATION", target.AccountRef)
	if err != nil || !fenceMatchesAuthorization(fence, target) {
		return nil, ErrProviderCredentialCleanupConflict
	}
	name := providerCredentialName(target.AccountRef, target.AuthorizationAttemptRef)
	wanted := store.providerCleanupFenceSecret(name, providerCleanupFence{
		Schema: providerCleanupFenceSchema, Kind: "CREDENTIAL", AccountRef: target.AccountRef,
		AuthorizationAttemptRef: target.AuthorizationAttemptRef, MaterializerAttemptRef: target.MaterializerAttemptRef,
	})
	defer clearSecretData(wanted)
	created, err := store.client.CoreV1().Secrets(store.namespace).Create(ctx, wanted, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, errors.New("fence authorization credential name")
	}
	clearSecretData(created)
	return store.readAuthorizationProducedCredential(ctx, target, false)
}

func (store *Store) readAuthorizationProducedCredential(ctx context.Context, target ProviderAuthorizationCleanupTarget, allowAbsent bool) (*ProviderCredentialDescriptor, error) {
	name := providerCredentialName(target.AccountRef, target.AuthorizationAttemptRef)
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) && allowAbsent {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("read authorization produced credential metadata")
	}
	defer clearSecretData(secret)
	if secret.Labels[providerCleanupFenceLabel] != "" {
		fence, err := store.parseProviderCleanupFence(secret, "CREDENTIAL", target.AccountRef)
		if err != nil || (fence.AuthorizationAttemptRef != "" && !fenceMatchesAuthorization(fence, target)) {
			return nil, ErrProviderCredentialCleanupConflict
		}
		return nil, nil
	}
	digest := secret.Annotations[providerContentSHAAnnotation]
	descriptor, err := providerCredentialDescriptor(secret, target.AuthorizationAttemptRef, target.AccountRef, digest)
	if err != nil || secret.Namespace != store.namespace || secret.Name != name ||
		!validProviderCredentialCleanupDescriptor(descriptor) || !providerCredentialCleanupSecretMatches(secret, target.AccountRef, descriptor) {
		return nil, ErrProviderCredentialCleanupConflict
	}
	return &descriptor, nil
}

func (store *Store) providerCleanupFenceSecret(name string, fence providerCleanupFence) *corev1.Secret {
	fence.SecretName = name
	raw, _ := json.Marshal(fence)
	immutable := true
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: store.namespace,
			Labels: map[string]string{providerCleanupFenceLabel: "true", providerManagedByLabel: providerSecretBrokerManager, providerPartOfLabel: "kodex"}},
		Type: corev1.SecretTypeOpaque, Immutable: &immutable, Data: map[string][]byte{providerCleanupFenceKey: raw},
	}
}

func (store *Store) parseProviderCleanupFence(secret *corev1.Secret, kind, accountRef string) (providerCleanupFence, error) {
	var fence providerCleanupFence
	if secret == nil || secret.Namespace != store.namespace || secret.UID == "" || secret.ResourceVersion == "" ||
		secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque ||
		secret.Labels[providerCleanupFenceLabel] != "true" || secret.Labels[providerManagedByLabel] != providerSecretBrokerManager ||
		secret.Labels[providerPartOfLabel] != "kodex" || len(secret.Data) != 1 || len(secret.Data[providerCleanupFenceKey]) == 0 || len(secret.Data[providerCleanupFenceKey]) > providerCleanupFenceMaximum {
		return fence, ErrProviderCredentialCleanupConflict
	}
	decoder := json.NewDecoder(bytes.NewReader(secret.Data[providerCleanupFenceKey]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&fence) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) ||
		fence.Schema != providerCleanupFenceSchema || fence.Kind != kind || fence.AccountRef != accountRef ||
		!validProviderCleanupReference(fence.AccountRef, "pacc_") || fence.SecretName != secret.Name ||
		!providerCredentialNamePattern.MatchString(fence.SecretName) || strings.ContainsAny(fence.OriginalResourceVersion, "\x00\r\n") ||
		((fence.OriginalUID == "") != (fence.OriginalResourceVersion == "")) ||
		(fence.OriginalUID != "" && (!providerCredentialUIDPattern.MatchString(fence.OriginalUID) || !validProviderResourceVersion(fence.OriginalResourceVersion))) {
		return providerCleanupFence{}, ErrProviderCredentialCleanupConflict
	}
	canonical, err := json.Marshal(fence)
	if err != nil || !bytes.Equal(canonical, secret.Data[providerCleanupFenceKey]) {
		return providerCleanupFence{}, ErrProviderCredentialCleanupConflict
	}
	return fence, nil
}

func fenceMatchesAuthorization(fence providerCleanupFence, target ProviderAuthorizationCleanupTarget) bool {
	return fence.AccountRef == target.AccountRef && fence.AuthorizationAttemptRef == target.AuthorizationAttemptRef &&
		fence.MaterializerAttemptRef == target.MaterializerAttemptRef
}
