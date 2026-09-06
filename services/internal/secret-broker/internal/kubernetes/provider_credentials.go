package kubernetes

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	providerAttemptLabel          = "provider-credentials.kodex.dev/authorization-attempt"
	providerCredentialLabel       = "provider-credentials.kodex.dev/credential"
	providerAttemptRefAnnotation  = "provider-credentials.kodex.dev/attempt-ref"
	providerAccountRefAnnotation  = "provider-credentials.kodex.dev/account-ref"
	providerContentSHAAnnotation  = "provider-credentials.kodex.dev/content-sha256"
	providerManagedByLabel        = "app.kubernetes.io/managed-by"
	providerPartOfLabel           = "app.kubernetes.io/part-of"
	providerRuntimeManagedLabel   = "runtime.kodex.dev/managed"
	providerRuntimeAccountRef     = "runtime.kodex.dev/provider-account-ref"
	providerRuntimeContentSHA     = "runtime.kodex.dev/provider-credential-digest"
	providerSecretBrokerManager   = "secret-broker"
	providerRuntimeManager        = "runtime-controller"
	providerAuthJSONKey           = "auth.json"
	providerAuthSHA256Key         = "auth.sha256"
	providerCredentialMaximumSize = 1 << 20
	providerReferenceMaximumSize  = 96
	providerResourceVersionMax    = 128
)

var (
	providerReferencePattern             = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{7,95}$`)
	providerCleanupReferencePattern      = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	providerCredentialNamePattern        = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)
	providerCredentialUIDPattern         = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	ErrProviderAttemptNotFound           = errors.New("provider authorization attempt is not found")
	ErrProviderAttemptConflict           = errors.New("provider authorization attempt conflicts with stored state")
	ErrProviderCredentialConflict        = errors.New("provider credential conflicts with stored materialization")
	ErrProviderCredentialInputInvalid    = errors.New("provider credential materialization input is invalid")
	ErrProviderCredentialCleanupConflict = errors.New("provider credential cleanup conflicts with stored materialization")
	ErrProviderCredentialCleanupInvalid  = errors.New("provider credential cleanup input is invalid")
	// SnapshotChanged доказывает отсутствие записи fence в текущем вызове.
	// Общий Conflict такого доказательства не несёт.
	ErrProviderAuthorizationCleanupSnapshotChanged = errors.Join(ErrProviderCredentialCleanupConflict,
		errors.New("provider authorization cleanup snapshot changed before fencing"))
)

// ProviderCredentialDescriptor содержит только exact metadata Kubernetes
// Secret. Ни один credential byte не покидает secret-broker.
type ProviderCredentialDescriptor struct {
	SecretName, SecretUID, SecretResourceVersion, ContentSHA256 string
}

// ProviderAuthorizationAttempt является безопасным межрепличным состоянием
// device authorization. Descriptor присутствует только после terminal success.
type ProviderAuthorizationAttempt struct {
	AttemptRef, AccountRef, MaterializerAttemptRef string
	SecretUID                                      string
	State, VerificationURI, UserCode               string
	ExternalAccountMasked, SafeFailureCode         string
	ExpiresAt                                      time.Time
	Credential                                     *ProviderCredentialDescriptor
	ResourceVersion                                string
}

func (store *Store) CreateProviderAuthorizationAttempt(
	ctx context.Context,
	attempt ProviderAuthorizationAttempt,
) (ProviderAuthorizationAttempt, bool, error) {
	name, err := validateProviderAttempt(attempt, true)
	if err != nil {
		return ProviderAuthorizationAttempt{}, false, err
	}
	wanted := providerAttemptSecret(store.namespace, name, attempt)
	defer clearSecretData(wanted)
	created, err := store.client.CoreV1().Secrets(store.namespace).Create(ctx, wanted, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = store.client.CoreV1().Secrets(store.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return ProviderAuthorizationAttempt{}, false, errors.New("read provider authorization attempt")
		}
		defer clearSecretData(created)
		stored, parseErr := providerAttemptFromSecret(created)
		if parseErr != nil || !samePendingProviderAttempt(stored, attempt) {
			return ProviderAuthorizationAttempt{}, false, ErrProviderAttemptConflict
		}
		return stored, false, nil
	}
	if err != nil {
		return ProviderAuthorizationAttempt{}, false, errors.New("create provider authorization attempt")
	}
	defer clearSecretData(created)
	stored, err := providerAttemptFromSecret(created)
	if err != nil || !samePendingProviderAttempt(stored, attempt) {
		return ProviderAuthorizationAttempt{}, false, ErrProviderAttemptConflict
	}
	return stored, true, nil
}

func (store *Store) GetProviderAuthorizationAttempt(
	ctx context.Context,
	materializerAttemptRef string,
) (ProviderAuthorizationAttempt, error) {
	name, err := providerAttemptName(materializerAttemptRef)
	if err != nil {
		return ProviderAuthorizationAttempt{}, err
	}
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ProviderAuthorizationAttempt{}, ErrProviderAttemptNotFound
	}
	if err != nil {
		return ProviderAuthorizationAttempt{}, errors.New("read provider authorization attempt")
	}
	defer clearSecretData(secret)
	return providerAttemptFromSecret(secret)
}

func (store *Store) CompleteProviderAuthorizationAttempt(
	ctx context.Context,
	wanted ProviderAuthorizationAttempt,
) (ProviderAuthorizationAttempt, error) {
	name, err := validateProviderAttempt(wanted, false)
	if err != nil || wanted.State == "PENDING" {
		return ProviderAuthorizationAttempt{}, ErrProviderAttemptConflict
	}
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ProviderAuthorizationAttempt{}, ErrProviderAttemptNotFound
	}
	if err != nil {
		return ProviderAuthorizationAttempt{}, errors.New("read provider authorization attempt for completion")
	}
	defer clearSecretData(secret)
	current, err := providerAttemptFromSecret(secret)
	if err != nil || current.AttemptRef != wanted.AttemptRef || current.AccountRef != wanted.AccountRef ||
		current.MaterializerAttemptRef != wanted.MaterializerAttemptRef {
		return ProviderAuthorizationAttempt{}, ErrProviderAttemptConflict
	}
	if current.State != "PENDING" {
		if sameTerminalProviderAttempt(current, wanted) {
			return current, nil
		}
		return ProviderAuthorizationAttempt{}, ErrProviderAttemptConflict
	}
	terminal := providerAttemptSecret(store.namespace, name, wanted)
	terminal.UID = secret.UID
	terminal.ResourceVersion = secret.ResourceVersion
	defer clearSecretData(terminal)
	updated, err := store.client.CoreV1().Secrets(store.namespace).Update(ctx, terminal, metav1.UpdateOptions{})
	if apierrors.IsConflict(err) {
		return ProviderAuthorizationAttempt{}, ErrProviderAttemptConflict
	}
	if err != nil {
		return ProviderAuthorizationAttempt{}, errors.New("complete provider authorization attempt")
	}
	defer clearSecretData(updated)
	result, err := providerAttemptFromSecret(updated)
	if err != nil || !sameTerminalProviderAttempt(result, wanted) {
		return ProviderAuthorizationAttempt{}, ErrProviderAttemptConflict
	}
	return result, nil
}

func (store *Store) CreateProviderCredential(
	ctx context.Context,
	attemptRef, accountRef string,
	authJSON []byte,
) (ProviderCredentialDescriptor, error) {
	if !providerReferencePattern.MatchString(attemptRef) || !providerReferencePattern.MatchString(accountRef) ||
		len(authJSON) == 0 || len(authJSON) > providerCredentialMaximumSize || !json.Valid(authJSON) {
		return ProviderCredentialDescriptor{}, ErrProviderCredentialInputInvalid
	}
	digest := sha256.Sum256(authJSON)
	digestText := hex.EncodeToString(digest[:])
	name := providerCredentialName(accountRef, attemptRef)
	immutable := true
	wanted := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: store.namespace,
			Labels: map[string]string{
				providerCredentialLabel: "true",
				providerManagedByLabel:  providerSecretBrokerManager,
				providerPartOfLabel:     "kodex",
			},
			Annotations: map[string]string{
				providerAttemptRefAnnotation: attemptRef,
				providerAccountRefAnnotation: accountRef,
				providerContentSHAAnnotation: digestText,
			},
		},
		Immutable: &immutable,
		Type:      corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			providerAuthJSONKey:   append([]byte(nil), authJSON...),
			providerAuthSHA256Key: []byte(digestText),
		},
	}
	defer clearSecretData(wanted)
	created, err := store.client.CoreV1().Secrets(store.namespace).Create(ctx, wanted, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = store.client.CoreV1().Secrets(store.namespace).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return ProviderCredentialDescriptor{}, errors.New("create immutable provider credential")
	}
	defer clearSecretData(created)
	return providerCredentialDescriptor(created, attemptRef, accountRef, digestText)
}

// ReadProviderCredentialExact возвращает копию auth.json только после exact
// проверки server-owned account binding, UID, resourceVersion и digest.
func (store *Store) ReadProviderCredentialExact(ctx context.Context, accountRef string, descriptor ProviderCredentialDescriptor) ([]byte, error) {
	if !providerReferencePattern.MatchString(accountRef) || !validProviderCredentialDescriptor(descriptor) {
		return nil, ErrProviderCredentialInputInvalid
	}
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, descriptor.SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, ErrProviderCredentialConflict
	}
	if err != nil {
		return nil, errors.New("read exact provider credential")
	}
	defer clearSecretData(secret)
	if string(secret.UID) != descriptor.SecretUID || secret.ResourceVersion != descriptor.SecretResourceVersion ||
		!providerCredentialCleanupSecretMatches(secret, accountRef, descriptor) {
		return nil, ErrProviderCredentialConflict
	}
	return append([]byte(nil), secret.Data[providerAuthJSONKey]...), nil
}

// DiscardProviderAuthorizationAttempt сохраняет тот же durable fence,
// что и фоновая cleanup task. Ошибка компенсации не завершает owner cleanup.
func (store *Store) DiscardProviderAuthorizationAttempt(
	ctx context.Context,
	attemptRef, accountRef, materializerAttemptRef, secretUID, initialResourceVersion string,
) error {
	target := ProviderAuthorizationCleanupTarget{AccountRef: accountRef, AuthorizationAttemptRef: attemptRef,
		MaterializerAttemptRef: materializerAttemptRef, UID: secretUID, ResourceVersion: initialResourceVersion,
		Kind: ProviderCleanupAuthorization, Generation: 1}
	target.TaskRef = "pcct_" + providerCleanupValueDigest(target)[:40]
	if err := store.FenceProviderAuthorization(ctx, target); err != nil {
		return providerDiscardError(err, ErrProviderAttemptConflict)
	}
	produced, err := store.FenceAuthorizationCredentialName(ctx, target)
	if err != nil {
		return providerDiscardError(err, ErrProviderAttemptConflict)
	}
	if produced != nil {
		return store.DiscardProviderCredential(ctx, attemptRef, accountRef, *produced)
	}
	return nil
}

// DiscardProviderCredential применяет delete только после полного exact
// readback immutable Secret и Kubernetes UID/RV preconditions.
func (store *Store) DiscardProviderCredential(
	ctx context.Context,
	attemptRef, accountRef string,
	descriptor ProviderCredentialDescriptor,
) error {
	if !providerReferencePattern.MatchString(attemptRef) || !providerReferencePattern.MatchString(accountRef) ||
		!validProviderCredentialDescriptor(descriptor) || descriptor.SecretName != providerCredentialName(accountRef, attemptRef) {
		return ErrProviderCredentialConflict
	}
	taskRef := "pcct_" + providerCleanupValueDigest(struct {
		Attempt, Account string
		Descriptor       ProviderCredentialDescriptor
	}{attemptRef, accountRef, descriptor})[:40]
	result, err := store.CleanupProviderCredential(ctx, taskRef, accountRef, 1, descriptor)
	if err != nil {
		return providerDiscardError(err, ErrProviderCredentialConflict)
	}
	if result.ProducedCredential != nil {
		return ErrProviderCredentialConflict
	}
	return nil
}

func providerDiscardError(err, conflict error) error {
	if errors.Is(err, ErrProviderCredentialCleanupInvalid) || errors.Is(err, ErrProviderCredentialCleanupConflict) {
		return errors.Join(conflict, err)
	}
	return err
}

// CleanupProviderCredential удаляет только exact объект и занимает его имя
// immutable fence. Появившаяся замена возвращается владельцу отдельным target.
func (store *Store) CleanupProviderCredential(
	ctx context.Context,
	taskRef, accountRef string,
	leaseGeneration int64,
	descriptor ProviderCredentialDescriptor,
) (ProviderCredentialCleanupResult, error) {
	identity, err := credentialCleanupReceiptIdentity(taskRef, accountRef, leaseGeneration, descriptor)
	if err != nil {
		return ProviderCredentialCleanupResult{}, err
	}
	if result, found, err := store.ReadProviderCleanupReceipt(ctx, identity); err != nil || found {
		return result, err
	}
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, descriptor.SecretName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return ProviderCredentialCleanupResult{}, errors.New("read provider credential for cleanup")
	}
	if apierrors.IsNotFound(err) {
		secret = nil
	}
	defer clearSecretData(secret)
	if secret != nil && secret.Labels[providerCleanupFenceLabel] == "" {
		if string(secret.UID) != descriptor.SecretUID {
			produced, err := store.cleanupReplacementDescriptor(secret, accountRef, descriptor.SecretName)
			if err != nil {
				return ProviderCredentialCleanupResult{}, err
			}
			return store.CompleteProviderCleanupReceipt(ctx, identity, produced)
		}
		if secret.Namespace != store.namespace || !providerCredentialCleanupSecretMatches(secret, accountRef, descriptor) {
			return ProviderCredentialCleanupResult{}, ErrProviderCredentialCleanupConflict
		}
		uid, version := types.UID(descriptor.SecretUID), descriptor.SecretResourceVersion
		err = store.client.CoreV1().Secrets(store.namespace).Delete(ctx, descriptor.SecretName, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version},
		})
		if apierrors.IsConflict(err) || apierrors.IsInvalid(err) {
			return ProviderCredentialCleanupResult{}, ErrProviderCredentialCleanupConflict
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return ProviderCredentialCleanupResult{}, errors.New("delete provider credential for cleanup")
		}
	}
	wanted := store.providerCleanupFenceSecret(descriptor.SecretName, providerCleanupFence{
		Schema: providerCleanupFenceSchema, Kind: "CREDENTIAL", AccountRef: accountRef,
		OriginalUID: descriptor.SecretUID, OriginalResourceVersion: descriptor.SecretResourceVersion,
	})
	defer clearSecretData(wanted)
	written, err := store.client.CoreV1().Secrets(store.namespace).Create(ctx, wanted, metav1.CreateOptions{})
	clearSecretData(written)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return ProviderCredentialCleanupResult{}, errors.New("persist provider credential cleanup fence")
	}
	readback, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, descriptor.SecretName, metav1.GetOptions{})
	if err != nil {
		return ProviderCredentialCleanupResult{}, errors.New("read back provider credential cleanup fence")
	}
	defer clearSecretData(readback)
	if readback.Labels[providerCleanupFenceLabel] != "" {
		if _, err := store.parseProviderCleanupFence(readback, "CREDENTIAL", accountRef); err != nil || readback.Name != descriptor.SecretName {
			return ProviderCredentialCleanupResult{}, ErrProviderCredentialCleanupConflict
		}
		return store.CompleteProviderCleanupReceipt(ctx, identity, nil)
	}
	if string(readback.UID) == descriptor.SecretUID {
		return ProviderCredentialCleanupResult{}, ErrProviderCredentialCleanupConflict
	}
	produced, err := store.cleanupReplacementDescriptor(readback, accountRef, descriptor.SecretName)
	if err != nil {
		return ProviderCredentialCleanupResult{}, err
	}
	return store.CompleteProviderCleanupReceipt(ctx, identity, produced)
}

func (store *Store) cleanupReplacementDescriptor(secret *corev1.Secret, accountRef, name string) (*ProviderCredentialDescriptor, error) {
	if secret == nil || secret.Namespace != store.namespace || secret.Name != name {
		return nil, ErrProviderCredentialCleanupConflict
	}
	descriptor := ProviderCredentialDescriptor{SecretName: secret.Name, SecretUID: string(secret.UID),
		SecretResourceVersion: secret.ResourceVersion, ContentSHA256: string(secret.Data[providerAuthSHA256Key])}
	if !validProviderCredentialCleanupDescriptor(descriptor) || !providerCredentialCleanupSecretMatches(secret, accountRef, descriptor) {
		return nil, ErrProviderCredentialCleanupConflict
	}
	return &descriptor, nil
}

func providerAttemptSecret(namespace, name string, attempt ProviderAuthorizationAttempt) *corev1.Secret {
	descriptor := []byte(nil)
	if attempt.Credential != nil {
		descriptor, _ = json.Marshal(attempt.Credential)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace,
			Labels: map[string]string{providerAttemptLabel: "true"},
			Annotations: map[string]string{
				providerAttemptRefAnnotation: attempt.AttemptRef,
				providerAccountRefAnnotation: attempt.AccountRef,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"materializer-attempt-ref": []byte(attempt.MaterializerAttemptRef),
			"state":                    []byte(attempt.State),
			"verification-uri":         []byte(attempt.VerificationURI),
			"user-code":                []byte(attempt.UserCode),
			"expires-at":               []byte(attempt.ExpiresAt.UTC().Format(time.RFC3339Nano)),
			"external-account-masked":  []byte(attempt.ExternalAccountMasked),
			"safe-failure-code":        []byte(attempt.SafeFailureCode),
			"credential-descriptor":    descriptor,
		},
	}
}

func providerAttemptFromSecret(secret *corev1.Secret) (ProviderAuthorizationAttempt, error) {
	if secret == nil || secret.Namespace == "" || secret.Name == "" || secret.UID == "" || secret.ResourceVersion == "" ||
		secret.Labels[providerAttemptLabel] != "true" || secret.Type != corev1.SecretTypeOpaque || secret.Immutable != nil {
		return ProviderAuthorizationAttempt{}, ErrProviderAttemptConflict
	}
	result := ProviderAuthorizationAttempt{
		AttemptRef:             secret.Annotations[providerAttemptRefAnnotation],
		AccountRef:             secret.Annotations[providerAccountRefAnnotation],
		MaterializerAttemptRef: string(secret.Data["materializer-attempt-ref"]),
		State:                  string(secret.Data["state"]),
		VerificationURI:        string(secret.Data["verification-uri"]),
		UserCode:               string(secret.Data["user-code"]),
		ExternalAccountMasked:  string(secret.Data["external-account-masked"]),
		SafeFailureCode:        string(secret.Data["safe-failure-code"]),
		SecretUID:              string(secret.UID),
		ResourceVersion:        secret.ResourceVersion,
	}
	result.ExpiresAt, _ = time.Parse(time.RFC3339Nano, string(secret.Data["expires-at"]))
	if raw := secret.Data["credential-descriptor"]; len(raw) > 0 {
		result.Credential = &ProviderCredentialDescriptor{}
		if json.Unmarshal(raw, result.Credential) != nil {
			return ProviderAuthorizationAttempt{}, ErrProviderAttemptConflict
		}
	}
	if _, err := validateProviderAttempt(result, result.State == "PENDING"); err != nil {
		return ProviderAuthorizationAttempt{}, err
	}
	return result, nil
}

func validateProviderAttempt(attempt ProviderAuthorizationAttempt, pending bool) (string, error) {
	name, err := providerAttemptName(attempt.MaterializerAttemptRef)
	if err != nil || !providerReferencePattern.MatchString(attempt.AttemptRef) ||
		!providerReferencePattern.MatchString(attempt.AccountRef) || attempt.ExpiresAt.IsZero() ||
		len(attempt.VerificationURI) > 2000 || len(attempt.UserCode) > 128 ||
		len(attempt.ExternalAccountMasked) > 320 || len(attempt.SafeFailureCode) > 96 {
		return "", ErrProviderAttemptConflict
	}
	if pending {
		if attempt.State != "PENDING" || attempt.VerificationURI == "" || attempt.UserCode == "" ||
			attempt.Credential != nil || attempt.SafeFailureCode != "" {
			return "", ErrProviderAttemptConflict
		}
		return name, nil
	}
	switch attempt.State {
	case "AUTHORIZED":
		if attempt.Credential == nil || !validProviderCredentialDescriptor(*attempt.Credential) ||
			attempt.ExternalAccountMasked == "" || attempt.SafeFailureCode != "" {
			return "", ErrProviderAttemptConflict
		}
	case "EXPIRED", "FAILED":
		if attempt.Credential != nil || attempt.SafeFailureCode == "" {
			return "", ErrProviderAttemptConflict
		}
	default:
		return "", ErrProviderAttemptConflict
	}
	return name, nil
}

func providerAttemptName(materializerAttemptRef string) (string, error) {
	if !providerReferencePattern.MatchString(materializerAttemptRef) {
		return "", ErrProviderAttemptConflict
	}
	digest := sha256.Sum256([]byte(materializerAttemptRef))
	return "provider-auth-" + hex.EncodeToString(digest[:16]), nil
}

func providerCredentialName(accountRef, attemptRef string) string {
	digest := sha256.Sum256([]byte(accountRef + "\x00" + attemptRef))
	return "provider-credential-" + hex.EncodeToString(digest[:16])
}

func providerCredentialDescriptor(secret *corev1.Secret, attemptRef, accountRef, digest string) (ProviderCredentialDescriptor, error) {
	if secret == nil || secret.UID == "" || secret.ResourceVersion == "" || secret.Type != corev1.SecretTypeOpaque ||
		secret.Immutable == nil || !*secret.Immutable || !providerCredentialSecretBrokerOwned(secret, accountRef, digest) ||
		secret.Annotations[providerAttemptRefAnnotation] != attemptRef ||
		string(secret.Data[providerAuthSHA256Key]) != digest {
		return ProviderCredentialDescriptor{}, ErrProviderCredentialConflict
	}
	actual := sha256.Sum256(secret.Data[providerAuthJSONKey])
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(actual[:])), []byte(digest)) != 1 ||
		!json.Valid(secret.Data[providerAuthJSONKey]) {
		return ProviderCredentialDescriptor{}, ErrProviderCredentialConflict
	}
	result := ProviderCredentialDescriptor{
		SecretName: secret.Name, SecretUID: string(secret.UID),
		SecretResourceVersion: secret.ResourceVersion, ContentSHA256: digest,
	}
	if !validProviderCredentialDescriptor(result) {
		return ProviderCredentialDescriptor{}, ErrProviderCredentialConflict
	}
	return result, nil
}

func validProviderCredentialDescriptor(value ProviderCredentialDescriptor) bool {
	if value.SecretName == "" || len(value.SecretName) > 63 || value.SecretUID == "" ||
		value.SecretResourceVersion == "" || len(value.SecretResourceVersion) > 128 ||
		len(value.ContentSHA256) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value.ContentSHA256)
	return err == nil && len(decoded) == sha256.Size &&
		value.ContentSHA256 == strings.ToLower(value.ContentSHA256)
}

func validProviderCleanupReference(value, prefix string) bool {
	if len(value) < len(prefix)+8 || len(value) > providerReferenceMaximumSize || !strings.HasPrefix(value, prefix) {
		return false
	}
	return providerCleanupReferencePattern.MatchString(value[len(prefix):])
}

func validProviderCredentialCleanupDescriptor(value ProviderCredentialDescriptor) bool {
	return providerCredentialNamePattern.MatchString(value.SecretName) &&
		providerCredentialUIDPattern.MatchString(value.SecretUID) &&
		validProviderResourceVersion(value.SecretResourceVersion) &&
		validProviderCredentialDescriptor(value)
}

func validProviderResourceVersion(value string) bool {
	if value == "" || len(value) > providerResourceVersionMax {
		return false
	}
	for index := range len(value) {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}

func providerCredentialCleanupSecretMatches(
	secret *corev1.Secret,
	accountRef string,
	descriptor ProviderCredentialDescriptor,
) bool {
	if secret == nil {
		return false
	}
	serverOwned := providerCredentialSecretBrokerOwned(secret, accountRef, descriptor.ContentSHA256) ||
		providerCredentialRuntimeOwned(secret, accountRef, descriptor.ContentSHA256)
	if secret.Name != descriptor.SecretName || string(secret.UID) != descriptor.SecretUID ||
		secret.ResourceVersion != descriptor.SecretResourceVersion || secret.Immutable == nil || !*secret.Immutable ||
		secret.Type != corev1.SecretTypeOpaque || len(secret.Data) != 2 ||
		string(secret.Data[providerAuthSHA256Key]) != descriptor.ContentSHA256 ||
		!json.Valid(secret.Data[providerAuthJSONKey]) || !serverOwned {
		return false
	}
	actual := sha256.Sum256(secret.Data[providerAuthJSONKey])
	return subtle.ConstantTimeCompare(
		[]byte(hex.EncodeToString(actual[:])),
		[]byte(descriptor.ContentSHA256),
	) == 1
}

func providerCredentialSecretBrokerOwned(secret *corev1.Secret, accountRef, digest string) bool {
	return secret.Labels[providerCredentialLabel] == "true" &&
		secret.Labels[providerManagedByLabel] == providerSecretBrokerManager &&
		secret.Labels[providerPartOfLabel] == "kodex" &&
		secret.Annotations[providerAccountRefAnnotation] == accountRef &&
		secret.Annotations[providerContentSHAAnnotation] == digest
}

func providerCredentialRuntimeOwned(secret *corev1.Secret, accountRef, digest string) bool {
	return secret.Labels[providerRuntimeManagedLabel] == "true" &&
		secret.Labels[providerManagedByLabel] == providerRuntimeManager &&
		secret.Labels[providerPartOfLabel] == "kodex" &&
		secret.Annotations[providerRuntimeAccountRef] == accountRef &&
		secret.Annotations[providerRuntimeContentSHA] == digest
}

func samePendingProviderAttempt(left, right ProviderAuthorizationAttempt) bool {
	return left.AttemptRef == right.AttemptRef && left.AccountRef == right.AccountRef &&
		left.MaterializerAttemptRef == right.MaterializerAttemptRef && left.State == right.State &&
		left.VerificationURI == right.VerificationURI && left.UserCode == right.UserCode &&
		left.ExpiresAt.Equal(right.ExpiresAt)
}

func sameTerminalProviderAttempt(left, right ProviderAuthorizationAttempt) bool {
	if left.AttemptRef != right.AttemptRef || left.AccountRef != right.AccountRef ||
		left.MaterializerAttemptRef != right.MaterializerAttemptRef || left.State != right.State ||
		left.ExternalAccountMasked != right.ExternalAccountMasked || left.SafeFailureCode != right.SafeFailureCode {
		return false
	}
	if left.Credential == nil || right.Credential == nil {
		return left.Credential == nil && right.Credential == nil
	}
	return *left.Credential == *right.Credential
}
