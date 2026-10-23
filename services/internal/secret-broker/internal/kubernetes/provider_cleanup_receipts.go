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
	// providerCleanupReceiptLabel identifies immutable terminal cleanup receipts, separate from
	// name-blocking fences and credential material.
	// providerCleanupReceiptLabel выделяет immutable terminal квитанции cleanup отдельно от блокирующих имя
	// fences и credential material.
	providerCleanupReceiptLabel = "provider-credentials.kodex.dev/cleanup-receipt"
	// providerCleanupReceiptKey is the single JSON data key used for cleanup receipts and mutable renewal
	// receipts; their schemas and validation remain distinct.
	// providerCleanupReceiptKey задаёт единственный JSON-ключ Data для cleanup и изменяемых renewal
	// квитанций; их схемы и проверки различаются.
	providerCleanupReceiptKey = "receipt.json"
	// providerCleanupReceiptSchema pins the supported terminal cleanup record format; unknown schema values
	// fail closed during readback.
	// providerCleanupReceiptSchema закрепляет поддерживаемый формат terminal cleanup записи; неизвестная
	// схема даёт закрытый отказ при чтении.
	providerCleanupReceiptSchema = "kodex.provider-cleanup-receipt.v1"
)

// ProviderCleanupReceiptIdentity binds task, account, cleanup kind and generation to the digest of one
// exact target, preventing a terminal receipt from being reused for another input.
// ProviderCleanupReceiptIdentity связывает task, account, cleanup kind и generation с digest одной
// точной цели, исключая использование terminal квитанции для другого входа.
type ProviderCleanupReceiptIdentity struct {
	TaskRef, AccountRef, Kind, TargetDigest string
	Generation                              int64
}

// ProviderCleanupRecoveryIdentity carries the control-plane-assigned original cleanup target identity
// independently of the current claim, including a bounded historical generation window where explicitly
// supplied.
// ProviderCleanupRecoveryIdentity несёт назначенную control-plane исходную identity cleanup цели
// отдельно от текущего claim, включая ограниченное окно исторических поколений, если оно явно задано.
type ProviderCleanupRecoveryIdentity struct {
	TaskRef                          string
	Generation, LegacyLastGeneration int64
}

// Valid checks task shape, positive origin generation and a historical window of zero to 32; a nonzero
// window requires origin generation one. When origin and current task match, neither bound may exceed
// the current generation; ownership is checked elsewhere.
// Valid проверяет форму task, положительное исходное поколение и историческое окно от нуля до 32;
// ненулевое окно требует исходного поколения один. При совпадении исходного и текущего task границы не
// превышают текущее поколение; владение проверяется отдельно.
func (identity ProviderCleanupRecoveryIdentity) Valid(currentTask string, currentGeneration int64) bool {
	return validProviderCleanupReference(identity.TaskRef, "pcct_") && identity.Generation > 0 &&
		identity.LegacyLastGeneration >= 0 && identity.LegacyLastGeneration <= 32 &&
		(identity.LegacyLastGeneration == 0 || identity.Generation == 1) &&
		(identity.TaskRef != currentTask || identity.Generation <= currentGeneration && identity.LegacyLastGeneration <= currentGeneration)
}

// CredentialCleanupReceiptIdentity exposes the validated credential-target receipt identity constructor
// without performing reads, cleanup or authority checks.
// CredentialCleanupReceiptIdentity предоставляет конструктор проверенной identity квитанции для
// credential-цели без чтения, cleanup и проверки полномочий.
func CredentialCleanupReceiptIdentity(taskRef, accountRef string, generation int64, descriptor ProviderCredentialDescriptor) (ProviderCleanupReceiptIdentity, error) {
	return credentialCleanupReceiptIdentity(taskRef, accountRef, generation, descriptor)
}

// providerCleanupReceiptRecord serializes the versioned cleanup identity and metadata-only terminal
// result for immutable storage and byte-exact readback.
// providerCleanupReceiptRecord сериализует версионированную cleanup identity и terminal результат только
// с metadata для immutable хранения и чтения с точным сравнением байтов.
type providerCleanupReceiptRecord struct {
	Schema   string
	Identity ProviderCleanupReceiptIdentity
	Result   ProviderCredentialCleanupResult
}

// AuthorizationCleanupReceiptIdentity validates the fully specified authorization or absence target and
// hashes its exact fields into a receipt identity without changing external state.
// AuthorizationCleanupReceiptIdentity проверяет полностью заданную authorization или absence цель и
// хеширует её точные поля в identity квитанции без изменения внешнего состояния.
func AuthorizationCleanupReceiptIdentity(target ProviderAuthorizationCleanupTarget) (ProviderCleanupReceiptIdentity, error) {
	if !validAuthorizationCleanupTarget(target) {
		return ProviderCleanupReceiptIdentity{}, ErrProviderCredentialCleanupInvalid
	}
	return ProviderCleanupReceiptIdentity{TaskRef: target.TaskRef, AccountRef: target.AccountRef,
		Kind: target.Kind, Generation: target.Generation, TargetDigest: providerCleanupValueDigest(target)}, nil
}

// credentialCleanupReceiptIdentity requires typed task/account references, positive generation and
// strict credential descriptor pins before hashing the descriptor as a CREDENTIAL target.
// credentialCleanupReceiptIdentity требует типизированные ссылки task/account, положительное generation
// и строгие привязки credential descriptor до хеширования descriptor как цели CREDENTIAL.
func credentialCleanupReceiptIdentity(taskRef, accountRef string, generation int64, descriptor ProviderCredentialDescriptor) (ProviderCleanupReceiptIdentity, error) {
	if !validProviderCleanupReference(taskRef, "pcct_") || !validProviderCleanupReference(accountRef, "pacc_") ||
		generation < 1 || !validProviderCredentialCleanupDescriptor(descriptor) {
		return ProviderCleanupReceiptIdentity{}, ErrProviderCredentialCleanupInvalid
	}
	return ProviderCleanupReceiptIdentity{TaskRef: taskRef, AccountRef: accountRef,
		Kind: "CREDENTIAL", Generation: generation, TargetDigest: providerCleanupValueDigest(descriptor)}, nil
}

// ReadProviderCleanupReceipt validates the identity and reads its deterministic receipt name. Only
// NotFound returns found=false without error; existing records must pass exact canonical validation, and
// temporary Data is cleared.
// ReadProviderCleanupReceipt проверяет identity и читает детерминированное имя квитанции. Только
// NotFound возвращает found=false без ошибки; существующая запись проходит точную проверку канонического
// представления, временная Data обнуляется.
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

// CompleteProviderCleanupReceipt persists an immutable metadata result and reads an AlreadyExists record
// instead of overwriting it; exact result equality makes retries idempotent. It validates produced
// descriptor shape, not the existence of the underlying credential or completion of cleanup itself.
// CompleteProviderCleanupReceipt сохраняет immutable metadata результата и при AlreadyExists читает
// запись вместо перезаписи; точное равенство результата делает повтор идемпотентным. Проверяется форма
// produced descriptor, не существование credential и не само завершение cleanup.
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

// providerCleanupReceiptFromSecret checks exact name/namespace, immutable managed metadata, one bounded
// data entry, schema, identity and result digest. Unknown fields, trailing JSON and any noncanonical
// byte representation conflict; no secret value is returned.
// providerCleanupReceiptFromSecret проверяет точные имя/namespace, immutable управляемые metadata, один
// ограниченный элемент Data, схему, identity и digest результата. Неизвестные поля, хвост JSON и любое
// неканоническое представление байтов дают конфликт; значение секрета не возвращается.
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

// validProviderCleanupReceiptIdentity validates task/account reference syntax, positive generation,
// digest shape and the closed set of credential/authorization/absence kinds; it does not authenticate
// the referenced task.
// validProviderCleanupReceiptIdentity проверяет синтаксис ссылок task/account, положительное generation,
// форму digest и закрытый набор credential/authorization/absence kinds; подлинность task здесь не
// подтверждается.
func validProviderCleanupReceiptIdentity(identity ProviderCleanupReceiptIdentity) bool {
	return validProviderCleanupReference(identity.TaskRef, "pcct_") && validProviderCleanupReference(identity.AccountRef, "pacc_") &&
		identity.Generation > 0 && validDigest(identity.TargetDigest) &&
		(identity.Kind == "CREDENTIAL" || identity.Kind == ProviderCleanupAuthorization || identity.Kind == ProviderCleanupAbsence)
}

// providerCleanupReceiptName derives a bounded Secret name from the first 20 SHA-256 bytes of the
// complete cleanup identity, not from caller-chosen filenames.
// providerCleanupReceiptName выводит ограниченное имя Secret из первых 20 байтов SHA-256 полной cleanup
// identity, а не из выбранного вызывающим имени файла.
func providerCleanupReceiptName(identity ProviderCleanupReceiptIdentity) string {
	return "provider-cleanup-" + providerCleanupValueDigest(identity)[:40]
}

// providerCleanupResultReceipt hashes the exact cleanup identity together with an optional replacement
// descriptor into safe metadata; the unkeyed digest is not a signature or independent proof of effect.
// providerCleanupResultReceipt хеширует точную cleanup identity с необязательным replacement descriptor
// в безопасные metadata; digest без ключа не является подписью и самостоятельным доказательством
// эффекта.
func providerCleanupResultReceipt(identity ProviderCleanupReceiptIdentity, produced *ProviderCredentialDescriptor) string {
	return "provider-cleanup:sha256:" + providerCleanupValueDigest(struct {
		Identity ProviderCleanupReceiptIdentity
		Produced *ProviderCredentialDescriptor
	}{identity, produced})
}

// providerCleanupValueDigest hashes Go JSON serialization of the supplied typed value; marshal errors
// are discarded by this helper, so it is not a general-purpose validated canonicalization API.
// providerCleanupValueDigest хеширует Go JSON-сериализацию переданного типизированного значения; helper
// отбрасывает ошибку marshal, поэтому не является универсальным API проверенной канонизации.
func providerCleanupValueDigest(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
