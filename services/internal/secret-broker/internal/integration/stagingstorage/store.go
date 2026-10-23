package stagingstorage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	// managedLabel distinguishes staged draft ciphertext objects; sharing a name or UID alone does not make
	// another Secret eligible.
	// managedLabel отличает staged draft ciphertext objects; совпадение name или UID само по себе не делает
	// иной Secret допустимым.
	managedLabel = "secret-drafts.kodex.dev/managed"
	// bindingAnnotation stores the strict scoped draft binding compared with authoritative work, never
	// plaintext or key material.
	// bindingAnnotation хранит strict scoped draft binding, сверяемый с authoritative work, не plaintext и не
	// key material.
	bindingAnnotation = "secret-drafts.kodex.dev/binding"
	// keyIDAnnotation pins the ciphertext's encryption-key identity; it does not contain the key bytes.
	// keyIDAnnotation закрепляет encryption-key identity ciphertext; байтов ключа не содержит.
	keyIDAnnotation = "secret-drafts.kodex.dev/encryption-key-id"
	// keyGenerationAnnotation carries the positive decimal encryption-key generation, checked again when
	// describing stored objects.
	// keyGenerationAnnotation содержит положительный decimal encryption-key generation, повторно проверяемый
	// при описании stored objects.
	keyGenerationAnnotation = "secret-drafts.kodex.dev/encryption-key-generation"
)

// Store adapts one namespaced Kubernetes Secret client for immutable staged ciphertext and exact cleanup;
// grants and lifecycle remain outside this adapter.
// Store адаптирует один namespaced Kubernetes Secret client для immutable staged ciphertext и exact
// cleanup; grants и lifecycle остаются вне adapter.
type Store struct {
	client    typedcorev1.SecretInterface
	namespace string
}

// New requires a nonnil Secret interface and DNS-label namespace without probing the client or proving its
// configured namespace matches; trusted composition must align them.
// New требует nonnil Secret interface и DNS-label namespace без проверки клиента и совпадения настроенного
// namespace; их соответствие обеспечивает trusted composition.
func New(client typedcorev1.SecretInterface, namespace string) (*Store, error) {
	if client == nil || len(validation.IsDNS1123Label(namespace)) != 0 {
		return nil, secretdrafts.ErrInvalid
	}
	return &Store{client: client, namespace: namespace}, nil
}

// Check reads a fixed readiness probe, clears any returned data and accepts NotFound; it proves that Get
// path, not create/delete capability or owner authority.
// Check читает фиксированный readiness probe, очищает возвращённые data и допускает NotFound; проверяет
// этот Get path, не create/delete capability и не owner authority.
func (store *Store) Check(ctx context.Context) error {
	probe, err := store.client.Get(ctx, "runtime-secret-draft-readiness-probe", metav1.GetOptions{})
	clearData(probe)
	if err != nil && !apierrors.IsNotFound(err) {
		return secretdrafts.ErrUnavailable
	}
	return nil
}

// Create validates work/ciphertext bounds, creates immutable ciphertext or observes an existing name,
// verifies exact metadata/digest/key identity and performs an additional exact Read. It clears adapter
// copies and does not retry uncertain writes or overwrite mismatches.
// Create проверяет work/ciphertext bounds, создаёт immutable ciphertext либо читает существующее имя,
// сверяет exact metadata/digest/key identity и делает дополнительный exact Read. Очищает adapter copies
// без повторения неопределённых writes и перезаписи mismatches.
func (store *Store) Create(ctx context.Context, work value.DraftWork, encrypted value.EncryptedSecretDraft) (value.DraftEncryptedDescriptor, error) {
	if store.validate(work) != nil || len(encrypted.Ciphertext) < 29 || len(encrypted.Ciphertext) > 1<<20 ||
		len(encrypted.Key.ID) != 64 || encrypted.Key.Generation < 1 {
		return value.DraftEncryptedDescriptor{}, secretdrafts.ErrInvalid
	}
	binding, err := json.Marshal(work.Binding)
	if err != nil {
		return value.DraftEncryptedDescriptor{}, secretdrafts.ErrInvalid
	}
	immutable := true
	wanted := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: work.StagedName, Namespace: store.namespace,
		Labels: map[string]string{managedLabel: "true"}, Annotations: map[string]string{
			bindingAnnotation: string(binding), keyIDAnnotation: encrypted.Key.ID,
			keyGenerationAnnotation: strconv.FormatInt(encrypted.Key.Generation, 10),
		}}, Type: corev1.SecretTypeOpaque, Immutable: &immutable,
		Data: map[string][]byte{work.StagedKey: append([]byte(nil), encrypted.Ciphertext...)}}
	defer clearData(wanted)
	actual, err := store.client.Create(ctx, wanted, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		actual, err = store.client.Get(ctx, work.StagedName, metav1.GetOptions{})
	}
	defer clearData(actual)
	if err != nil {
		return value.DraftEncryptedDescriptor{}, secretdrafts.ErrUnavailable
	}
	descriptor, err := store.describe(work, actual)
	wantedDigest := sha256.Sum256(encrypted.Ciphertext)
	if err != nil || descriptor.EncryptionKey != encrypted.Key || descriptor.CiphertextSHA256 != hex.EncodeToString(wantedDigest[:]) {
		return value.DraftEncryptedDescriptor{}, secretdrafts.ErrConflict
	}
	// Ответ Create дополнительно подтверждается авторитетным GET exact UID/RV.
	verified, err := store.Read(ctx, work, descriptor)
	clear(verified.Ciphertext)
	if err != nil {
		return value.DraftEncryptedDescriptor{}, err
	}
	return descriptor, nil
}

// Read gets the work-bound object, requires full equality with the pinned descriptor and returns a
// ciphertext copy while clearing fetched data; it does not decrypt.
// Read получает связанный с work объект, требует полного равенства pinned descriptor и возвращает
// ciphertext copy, очищая fetched data; расшифровку не выполняет.
func (store *Store) Read(ctx context.Context, work value.DraftWork, expected value.DraftEncryptedDescriptor) (value.EncryptedSecretDraft, error) {
	actual, err := store.get(ctx, work)
	defer clearData(actual)
	if err != nil {
		return value.EncryptedSecretDraft{}, err
	}
	descriptor, err := store.describe(work, actual)
	if err != nil || descriptor != expected {
		return value.EncryptedSecretDraft{}, secretdrafts.ErrConflict
	}
	return value.EncryptedSecretDraft{Key: descriptor.EncryptionKey, Ciphertext: append([]byte(nil), actual.Data[work.StagedKey]...)}, nil
}

// Lookup observes the work-bound object's validated descriptor and clears fetched ciphertext without
// returning its bytes.
// Lookup наблюдает проверенный descriptor связанного с work объекта и очищает fetched ciphertext без
// возврата его байтов.
func (store *Store) Lookup(ctx context.Context, work value.DraftWork) (value.DraftEncryptedDescriptor, error) {
	actual, err := store.get(ctx, work)
	defer clearData(actual)
	if err != nil {
		return value.DraftEncryptedDescriptor{}, err
	}
	return store.describe(work, actual)
}

// Delete verifies the descriptor by Read, deletes with exact UID/resourceVersion preconditions and
// requires NotFound readback. A replacement or lingering object returns Conflict; uncertain failures never
// trigger a blind second deletion.
// Delete сверяет descriptor через Read, удаляет с exact UID/resourceVersion preconditions и требует
// NotFound readback. Replacement либо оставшийся объект дают Conflict; неопределённый отказ не вызывает
// слепое повторное удаление.
func (store *Store) Delete(ctx context.Context, work value.DraftWork, expected value.DraftEncryptedDescriptor) error {
	if expected.UID == "" || expected.ResourceVersion == "" || expected.Namespace != store.namespace ||
		expected.Name != work.StagedName || expected.DataKey != work.StagedKey {
		return secretdrafts.ErrConflict
	}
	read, err := store.Read(ctx, work, expected)
	clear(read.Ciphertext)
	if err != nil {
		if err == secretdrafts.ErrNotFound {
			return nil
		}
		return err
	}
	uid := types.UID(expected.UID)
	rv := expected.ResourceVersion
	err = store.client.Delete(ctx, expected.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}})
	if err != nil && !apierrors.IsNotFound(err) {
		if apierrors.IsConflict(err) {
			return secretdrafts.ErrConflict
		}
		return secretdrafts.ErrUnavailable
	}
	// Только NotFound является доказательством завершённой очистки. Новый UID
	// под тем же именем не удаляется и не подменяет прежний receipt.
	actual, err := store.client.Get(ctx, expected.Name, metav1.GetOptions{})
	clearData(actual)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return secretdrafts.ErrUnavailable
	}
	return secretdrafts.ErrConflict
}

// get validates binding/name/key/namespace before Get, maps NotFound distinctly and hides other Kubernetes
// diagnostics as Unavailable, clearing any error response.
// get проверяет binding/name/key/namespace до Get, отдельно отображает NotFound и скрывает прочие
// Kubernetes diagnostics как Unavailable, очищая error response.
func (store *Store) get(ctx context.Context, work value.DraftWork) (*corev1.Secret, error) {
	if err := store.validate(work); err != nil {
		return nil, err
	}
	actual, err := store.client.Get(ctx, work.StagedName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, secretdrafts.ErrNotFound
	}
	if err != nil {
		clearData(actual)
		return nil, secretdrafts.ErrUnavailable
	}
	return actual, nil
}

// validate checks binding syntax, the configured staging namespace and Kubernetes name/key syntax; it does
// not validate grants, lease freshness or owner operation state.
// validate проверяет binding syntax, настроенный staging namespace и Kubernetes name/key syntax; grants,
// актуальность lease и owner operation state не проверяет.
func (store *Store) validate(work value.DraftWork) error {
	if work.Binding.Validate() != nil || work.StagedNamespace != store.namespace ||
		len(validation.IsDNS1123Subdomain(work.StagedName)) != 0 || len(validation.IsConfigMapKey(work.StagedKey)) != 0 {
		return secretdrafts.ErrConflict
	}
	return nil
}

// describe requires exact name/namespace, UID/RV, immutable live Opaque type, managed label and one
// bounded data key; it strictly matches binding/key metadata and hashes ciphertext without authenticating
// or decrypting it.
// describe требует exact name/namespace, UID/RV, immutable live Opaque type, managed label и один bounded
// data key; строго сверяет binding/key metadata и хеширует ciphertext без authentication и расшифровки.
func (store *Store) describe(work value.DraftWork, actual *corev1.Secret) (value.DraftEncryptedDescriptor, error) {
	if actual == nil || actual.Name != work.StagedName || actual.Namespace != store.namespace || actual.UID == "" || actual.ResourceVersion == "" ||
		actual.DeletionTimestamp != nil || actual.Immutable == nil || !*actual.Immutable || actual.Type != corev1.SecretTypeOpaque ||
		actual.Labels[managedLabel] != "true" || len(actual.Data) != 1 || len(actual.StringData) != 0 ||
		len(actual.Data[work.StagedKey]) < 29 || len(actual.Data[work.StagedKey]) > 1<<20 {
		return value.DraftEncryptedDescriptor{}, secretdrafts.ErrConflict
	}
	var binding value.SecretDraftBinding
	if internalrpcauth.DecodeStrictJSON([]byte(actual.Annotations[bindingAnnotation]), &binding) != nil || binding.Validate() != nil || binding != work.Binding {
		return value.DraftEncryptedDescriptor{}, secretdrafts.ErrConflict
	}
	keyGeneration, err := strconv.ParseInt(actual.Annotations[keyGenerationAnnotation], 10, 64)
	keyID := actual.Annotations[keyIDAnnotation]
	decodedID, decodeErr := hex.DecodeString(keyID)
	if err != nil || keyGeneration < 1 || decodeErr != nil || len(decodedID) != 32 || hex.EncodeToString(decodedID) != keyID {
		return value.DraftEncryptedDescriptor{}, secretdrafts.ErrConflict
	}
	digest := sha256.Sum256(actual.Data[work.StagedKey])
	return value.DraftEncryptedDescriptor{Namespace: store.namespace, Name: actual.Name, DataKey: work.StagedKey,
		UID: string(actual.UID), ResourceVersion: actual.ResourceVersion, CiphertextSHA256: hex.EncodeToString(digest[:]),
		EncryptionKey: value.DraftEncryptionKey{ID: keyID, Generation: keyGeneration}}, nil
}

// clearData overwrites byte slices in Secret.Data and drops Data/StringData references, handling nil;
// dropping immutable string references is not string-memory zeroization.
// clearData обнуляет byte slices в Secret.Data и удаляет Data/StringData references, допуская nil;
// удаление ссылок на immutable strings не стирает их память.
func clearData(secret *corev1.Secret) {
	if secret == nil {
		return
	}
	for _, data := range secret.Data {
		clear(data)
	}
	secret.Data, secret.StringData = nil, nil
}
