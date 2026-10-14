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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	providerRenewalReceiptLabel  = "provider-credentials.kodex.dev/renewal-receipt"
	providerRenewalReceiptSchema = "kodex.provider-renewal-receipt.v1"
	ProviderRenewalRunning       = "RUNNING"
	ProviderRenewalSucceeded     = "SUCCEEDED"
)

var ErrProviderRenewalConflict = errors.New("provider credential renewal receipt conflicts with stored state")

// Identity — назначенные CP pins одного разрешённого OAuth effect, не browser input.
type ProviderRenewalIdentity struct {
	TaskRef, AccountRef, CredentialRef, RequestDigest, ClaimantID, Fence string
	AccountVersion, CredentialRevision, Generation                       int64
	ExpiresAt                                                            time.Time
	Previous                                                             ProviderCredentialDescriptor
}

type ProviderRenewalReceipt struct {
	Schema          string
	Identity        ProviderRenewalIdentity
	State           string
	Produced        *ProviderCredentialDescriptor
	UID             string `json:"-"`
	ResourceVersion string `json:"-"`
}

func validProviderRenewalIdentity(identity ProviderRenewalIdentity) bool {
	if !validProviderCleanupReference(identity.TaskRef, "pcrt_") || !validProviderCleanupReference(identity.AccountRef, "pacc_") ||
		!providerReferencePattern.MatchString(identity.CredentialRef) || !providerReferencePattern.MatchString(identity.Fence) ||
		len(identity.ClaimantID) < 1 || len(identity.ClaimantID) > 128 || !renewalVisibleASCII(identity.ClaimantID) ||
		identity.AccountVersion < 1 || identity.CredentialRevision < 1 || identity.Generation < 1 ||
		identity.AccountVersion > 9007199254740991 || identity.CredentialRevision > 9007199254740991 || identity.Generation > 9007199254740991 ||
		identity.ExpiresAt.IsZero() || !validProviderCredentialCleanupDescriptor(identity.Previous) {
		return false
	}
	digest, err := hex.DecodeString(identity.RequestDigest)
	return err == nil && len(digest) == sha256.Size && hex.EncodeToString(digest) == identity.RequestDigest
}

func renewalVisibleASCII(value string) bool {
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func providerRenewalReceiptName(identity ProviderRenewalIdentity) string {
	digest := sha256.Sum256([]byte(identity.AccountRef + "\x00" + identity.TaskRef))
	return "provider-renewal-" + hex.EncodeToString(digest[:16])
}

func (store *Store) parseProviderRenewalReceipt(secret *corev1.Secret, identity ProviderRenewalIdentity) (ProviderRenewalReceipt, error) {
	if !validProviderRenewalIdentity(identity) || secret == nil || secret.Name != providerRenewalReceiptName(identity) ||
		secret.Namespace != store.namespace || secret.UID == "" || secret.ResourceVersion == "" ||
		secret.Type != corev1.SecretTypeOpaque || secret.Immutable != nil && *secret.Immutable ||
		secret.Labels[providerRenewalReceiptLabel] != "true" || secret.Labels[providerManagedByLabel] != providerSecretBrokerManager ||
		secret.Labels[providerPartOfLabel] != "kodex" || secret.Annotations[providerAccountRefAnnotation] != identity.AccountRef ||
		secret.Annotations[providerAttemptRefAnnotation] != identity.TaskRef || len(secret.Data) != 1 ||
		len(secret.Data[providerCleanupReceiptKey]) == 0 || len(secret.Data[providerCleanupReceiptKey]) > 4096 {
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	decoder := json.NewDecoder(bytes.NewReader(secret.Data[providerCleanupReceiptKey]))
	decoder.DisallowUnknownFields()
	var result ProviderRenewalReceipt
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF ||
		result.Schema != providerRenewalReceiptSchema || !reflect.DeepEqual(result.Identity, identity) ||
		result.State != ProviderRenewalRunning && result.State != ProviderRenewalSucceeded ||
		(result.State == ProviderRenewalRunning) != (result.Produced == nil) {
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	if result.Produced != nil && (!validProviderCredentialCleanupDescriptor(*result.Produced) ||
		result.Produced.SecretName != providerCredentialName(identity.AccountRef, identity.TaskRef)) {
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(canonical, secret.Data[providerCleanupReceiptKey]) {
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	result.UID, result.ResourceVersion = string(secret.UID), secret.ResourceVersion
	return result, nil
}

// Create — межрепличная одноразовая граница до native процесса. Unknown ACK
// не разрешает эффект; повтор AlreadyExists получает только существующую квитанцию.
func (store *Store) BeginProviderCredentialRenewal(ctx context.Context, identity ProviderRenewalIdentity) (ProviderRenewalReceipt, bool, error) {
	if !validProviderRenewalIdentity(identity) || !identity.ExpiresAt.After(time.Now()) {
		return ProviderRenewalReceipt{}, false, ErrProviderCredentialInputInvalid
	}
	encoded, err := json.Marshal(ProviderRenewalReceipt{Schema: providerRenewalReceiptSchema, Identity: identity, State: ProviderRenewalRunning})
	if err != nil || len(encoded) > 4096 {
		return ProviderRenewalReceipt{}, false, ErrProviderCredentialInputInvalid
	}
	wanted := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: providerRenewalReceiptName(identity), Namespace: store.namespace,
		Labels:      map[string]string{providerRenewalReceiptLabel: "true", providerManagedByLabel: providerSecretBrokerManager, providerPartOfLabel: "kodex"},
		Annotations: map[string]string{providerAccountRefAnnotation: identity.AccountRef, providerAttemptRefAnnotation: identity.TaskRef}},
		Type: corev1.SecretTypeOpaque, Data: map[string][]byte{providerCleanupReceiptKey: encoded}}
	defer clearSecretData(wanted)
	secret, err := store.client.CoreV1().Secrets(store.namespace).Create(ctx, wanted, metav1.CreateOptions{})
	created := err == nil
	if apierrors.IsAlreadyExists(err) {
		secret, err = store.client.CoreV1().Secrets(store.namespace).Get(ctx, wanted.Name, metav1.GetOptions{})
	}
	if err != nil {
		return ProviderRenewalReceipt{}, false, errors.New("persist provider renewal effect receipt")
	}
	defer clearSecretData(secret)
	result, err := store.parseProviderRenewalReceipt(secret, identity)
	return result, created && err == nil, err
}

func (store *Store) ReadProviderCredentialRenewal(ctx context.Context, identity ProviderRenewalIdentity) (ProviderRenewalReceipt, error) {
	if !validProviderRenewalIdentity(identity) {
		return ProviderRenewalReceipt{}, ErrProviderCredentialInputInvalid
	}
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, providerRenewalReceiptName(identity), metav1.GetOptions{})
	if err != nil {
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	defer clearSecretData(secret)
	return store.parseProviderRenewalReceipt(secret, identity)
}

func (store *Store) CompleteProviderCredentialRenewal(ctx context.Context, receipt ProviderRenewalReceipt, produced ProviderCredentialDescriptor) (ProviderRenewalReceipt, error) {
	if !validProviderRenewalIdentity(receipt.Identity) || !validProviderCredentialCleanupDescriptor(produced) ||
		produced.SecretName != providerCredentialName(receipt.Identity.AccountRef, receipt.Identity.TaskRef) || receipt.UID == "" || receipt.ResourceVersion == "" {
		return ProviderRenewalReceipt{}, ErrProviderCredentialInputInvalid
	}
	// Caller metadata не является proof существования произведённого credential.
	actual, found, err := store.RecoverProviderRenewalCredential(ctx, receipt.Identity)
	if err != nil || !found || actual != produced {
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, providerRenewalReceiptName(receipt.Identity), metav1.GetOptions{})
	if err != nil {
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	defer clearSecretData(secret)
	current, err := store.parseProviderRenewalReceipt(secret, receipt.Identity)
	if err != nil || current.UID != receipt.UID {
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	if current.State == ProviderRenewalSucceeded {
		if current.Produced != nil && *current.Produced == produced {
			return current, nil
		}
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	if current.ResourceVersion != receipt.ResourceVersion {
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	current.State, current.Produced = ProviderRenewalSucceeded, &produced
	encoded, err := json.Marshal(current)
	if err != nil || len(encoded) > 4096 {
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	secret.Data[providerCleanupReceiptKey] = encoded
	updated, err := store.client.CoreV1().Secrets(store.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return ProviderRenewalReceipt{}, ErrProviderRenewalConflict
	}
	defer clearSecretData(updated)
	return store.parseProviderRenewalReceipt(updated, receipt.Identity)
}

// READ recovery проверяет фактически опубликованный immutable результат по
// exact task/account/name. Ни OAuth, ни повтор Create здесь не исполняются.
func (store *Store) RecoverProviderRenewalCredential(ctx context.Context, identity ProviderRenewalIdentity) (ProviderCredentialDescriptor, bool, error) {
	if !validProviderRenewalIdentity(identity) {
		return ProviderCredentialDescriptor{}, false, ErrProviderCredentialInputInvalid
	}
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, providerCredentialName(identity.AccountRef, identity.TaskRef), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ProviderCredentialDescriptor{}, false, nil
	}
	if err != nil {
		return ProviderCredentialDescriptor{}, false, errors.New("read produced provider renewal credential")
	}
	defer clearSecretData(secret)
	result, err := providerCredentialDescriptor(secret, identity.TaskRef, identity.AccountRef, secret.Annotations[providerContentSHAAnnotation])
	return result, err == nil, err
}
