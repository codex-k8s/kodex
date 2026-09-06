package kubernetes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	providerCleanupReceiptLabel  = "provider-credentials.kodex.dev/cleanup-receipt"
	providerCleanupReceiptKey    = "receipt.json"
	providerCleanupReceiptSchema = "kodex.provider-cleanup-receipt.v1"
)

type ProviderCleanupReceiptIdentity struct {
	TaskRef, AccountRef, Kind, TargetDigest string
	Generation                              int64
}

// Recovery identity назначается CP для первого exact target, отдельно от claim.
type ProviderCleanupRecoveryIdentity struct {
	TaskRef                          string
	Generation, LegacyLastGeneration int64
}

func (identity ProviderCleanupRecoveryIdentity) Valid(currentTask string, currentGeneration int64) bool {
	return validProviderCleanupReference(identity.TaskRef, "pcct_") && identity.Generation > 0 &&
		identity.LegacyLastGeneration >= 0 && identity.LegacyLastGeneration <= 32 &&
		(identity.LegacyLastGeneration == 0 || identity.Generation == 1) &&
		(identity.TaskRef != currentTask || identity.Generation <= currentGeneration && identity.LegacyLastGeneration <= currentGeneration)
}

func CredentialCleanupReceiptIdentity(taskRef, accountRef string, generation int64, descriptor ProviderCredentialDescriptor) (ProviderCleanupReceiptIdentity, error) {
	return credentialCleanupReceiptIdentity(taskRef, accountRef, generation, descriptor)
}

type providerCleanupReceiptRecord struct {
	Schema   string
	Identity ProviderCleanupReceiptIdentity
	Result   ProviderCredentialCleanupResult
}

func AuthorizationCleanupReceiptIdentity(target ProviderAuthorizationCleanupTarget) (ProviderCleanupReceiptIdentity, error) {
	if !validAuthorizationCleanupTarget(target) {
		return ProviderCleanupReceiptIdentity{}, ErrProviderCredentialCleanupInvalid
	}
	return ProviderCleanupReceiptIdentity{TaskRef: target.TaskRef, AccountRef: target.AccountRef,
		Kind: target.Kind, Generation: target.Generation, TargetDigest: providerCleanupValueDigest(target)}, nil
}

func credentialCleanupReceiptIdentity(taskRef, accountRef string, generation int64, descriptor ProviderCredentialDescriptor) (ProviderCleanupReceiptIdentity, error) {
	if !validProviderCleanupReference(taskRef, "pcct_") || !validProviderCleanupReference(accountRef, "pacc_") ||
		generation < 1 || !validProviderCredentialCleanupDescriptor(descriptor) {
		return ProviderCleanupReceiptIdentity{}, ErrProviderCredentialCleanupInvalid
	}
	return ProviderCleanupReceiptIdentity{TaskRef: taskRef, AccountRef: accountRef,
		Kind: "CREDENTIAL", Generation: generation, TargetDigest: providerCleanupValueDigest(descriptor)}, nil
}

func (store *Store) ReadProviderCleanupReceipt(ctx context.Context, identity ProviderCleanupReceiptIdentity) (ProviderCredentialCleanupResult, bool, error) {
	if !validProviderCleanupReceiptIdentity(identity) {
		return ProviderCredentialCleanupResult{}, false, ErrProviderCredentialCleanupInvalid
	}
	name := providerCleanupReceiptName(identity)
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ProviderCredentialCleanupResult{}, false, nil
	}
	if err != nil {
		return ProviderCredentialCleanupResult{}, false, errors.New("read provider cleanup receipt")
	}
	defer clearSecretData(secret)
	result, err := store.providerCleanupReceiptFromSecret(secret, identity)
	return result, err == nil, err
}

func (store *Store) CompleteProviderCleanupReceipt(ctx context.Context, identity ProviderCleanupReceiptIdentity, produced *ProviderCredentialDescriptor) (ProviderCredentialCleanupResult, error) {
	if !validProviderCleanupReceiptIdentity(identity) || (produced != nil && !validProviderCredentialCleanupDescriptor(*produced)) {
		return ProviderCredentialCleanupResult{}, ErrProviderCredentialCleanupInvalid
	}
	result := ProviderCredentialCleanupResult{ProducedCredential: produced}
	result.TerminalReceipt = providerCleanupResultReceipt(identity, produced)
	record := providerCleanupReceiptRecord{Schema: providerCleanupReceiptSchema, Identity: identity, Result: result}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) > providerCleanupFenceMaximum {
		return ProviderCredentialCleanupResult{}, ErrProviderCredentialCleanupInvalid
	}
	defer clear(raw)
	immutable := true
	wanted := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: providerCleanupReceiptName(identity), Namespace: store.namespace,
		Labels: map[string]string{providerCleanupReceiptLabel: "true", providerManagedByLabel: providerSecretBrokerManager, providerPartOfLabel: "kodex"}},
		Type: corev1.SecretTypeOpaque, Immutable: &immutable, Data: map[string][]byte{providerCleanupReceiptKey: raw}}
	written, err := store.client.CoreV1().Secrets(store.namespace).Create(ctx, wanted, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		written, err = store.client.CoreV1().Secrets(store.namespace).Get(ctx, wanted.Name, metav1.GetOptions{})
	}
	if err != nil {
		return ProviderCredentialCleanupResult{}, errors.New("persist provider cleanup receipt")
	}
	defer clearSecretData(written)
	stored, err := store.providerCleanupReceiptFromSecret(written, identity)
	if err != nil || !reflect.DeepEqual(stored, result) {
		return ProviderCredentialCleanupResult{}, ErrProviderCredentialCleanupConflict
	}
	return stored, nil
}

func (store *Store) providerCleanupReceiptFromSecret(secret *corev1.Secret, identity ProviderCleanupReceiptIdentity) (ProviderCredentialCleanupResult, error) {
	if secret == nil || secret.Namespace != store.namespace || secret.Name != providerCleanupReceiptName(identity) ||
		secret.UID == "" || secret.ResourceVersion == "" || secret.Immutable == nil || !*secret.Immutable ||
		secret.Type != corev1.SecretTypeOpaque || secret.Labels[providerCleanupReceiptLabel] != "true" ||
		secret.Labels[providerManagedByLabel] != providerSecretBrokerManager || secret.Labels[providerPartOfLabel] != "kodex" ||
		len(secret.Data) != 1 || len(secret.Data[providerCleanupReceiptKey]) == 0 || len(secret.Data[providerCleanupReceiptKey]) > providerCleanupFenceMaximum {
		return ProviderCredentialCleanupResult{}, ErrProviderCredentialCleanupConflict
	}
	var record providerCleanupReceiptRecord
	decoder := json.NewDecoder(bytes.NewReader(secret.Data[providerCleanupReceiptKey]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) || record.Schema != providerCleanupReceiptSchema ||
		record.Identity != identity || (record.Result.ProducedCredential != nil && !validProviderCredentialCleanupDescriptor(*record.Result.ProducedCredential)) ||
		record.Result.TerminalReceipt != providerCleanupResultReceipt(identity, record.Result.ProducedCredential) {
		return ProviderCredentialCleanupResult{}, ErrProviderCredentialCleanupConflict
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(canonical, secret.Data[providerCleanupReceiptKey]) {
		return ProviderCredentialCleanupResult{}, ErrProviderCredentialCleanupConflict
	}
	return record.Result, nil
}

func validProviderCleanupReceiptIdentity(identity ProviderCleanupReceiptIdentity) bool {
	return validProviderCleanupReference(identity.TaskRef, "pcct_") && validProviderCleanupReference(identity.AccountRef, "pacc_") &&
		identity.Generation > 0 && validDigest(identity.TargetDigest) &&
		(identity.Kind == "CREDENTIAL" || identity.Kind == ProviderCleanupAuthorization || identity.Kind == ProviderCleanupAbsence)
}

func providerCleanupReceiptName(identity ProviderCleanupReceiptIdentity) string {
	return "provider-cleanup-" + providerCleanupValueDigest(identity)[:40]
}

func providerCleanupResultReceipt(identity ProviderCleanupReceiptIdentity, produced *ProviderCredentialDescriptor) string {
	return "provider-cleanup:sha256:" + providerCleanupValueDigest(struct {
		Identity ProviderCleanupReceiptIdentity
		Produced *ProviderCredentialDescriptor
	}{identity, produced})
}

func providerCleanupValueDigest(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
