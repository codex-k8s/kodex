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
	// providerRenewalReceiptLabel distinguishes mutable OAuth renewal reservations from immutable credential
	// results and terminal cleanup receipts.
	// providerRenewalReceiptLabel отличает изменяемые резервирования OAuth renewal от immutable credential
	// результатов и terminal cleanup квитанций.
	providerRenewalReceiptLabel = "provider-credentials.kodex.dev/renewal-receipt"
	// providerRenewalReceiptSchema identifies the one supported durable renewal record format checked before
	// any state interpretation.
	// providerRenewalReceiptSchema обозначает единственный поддерживаемый формат durable renewal записи,
	// проверяемый до интерпретации состояния.
	providerRenewalReceiptSchema = "kodex.provider-renewal-receipt.v1"
	// ProviderRenewalRunning marks the reserved, not-yet-completed renewal effect; it does not prove the
	// external OAuth process ran or that retry is safe.
	// ProviderRenewalRunning обозначает зарезервированный, ещё не завершённый renewal эффект; не доказывает
	// запуск внешнего OAuth процесса или безопасность повтора.
	ProviderRenewalRunning = "RUNNING"
	// ProviderRenewalSucceeded marks a receipt completed with an exact produced credential descriptor after
	// authoritative result readback.
	// ProviderRenewalSucceeded обозначает квитанцию, завершённую с точным produced credential descriptor
	// после авторитетного чтения результата.
	ProviderRenewalSucceeded = "SUCCEEDED"
)

// ErrProviderRenewalConflict rejects identity, canonical record, source-result or CAS mismatches;
// receipt read failures are intentionally collapsed to this same error.
// ErrProviderRenewalConflict отклоняет несовпадения identity, канонической записи, исходного результата
// или CAS; ошибки чтения квитанции намеренно сводятся к той же ошибке.
var ErrProviderRenewalConflict = errors.New("provider credential renewal receipt conflicts with stored state")

// ProviderRenewalIdentity carries the control-plane's exact task/account versions, claimant and fence,
// request digest, expiry and previous credential pins for one authorized renewal. Browser input cannot
// establish this authority.
// ProviderRenewalIdentity несёт точные версии task/account от control-plane, claimant и fence, request
// digest, срок и привязки предыдущего credential для одного разрешённого renewal. Ввод браузера эти
// полномочия не устанавливает.
type ProviderRenewalIdentity struct {
	TaskRef, AccountRef, CredentialRef, RequestDigest, ClaimantID, Fence string
	AccountVersion, CredentialRevision, Generation                       int64
	ExpiresAt                                                            time.Time
	Previous                                                             ProviderCredentialDescriptor
}

// ProviderRenewalReceipt stores RUNNING or SUCCEEDED together with the immutable input identity and
// optional produced descriptor. API UID/resourceVersion are local CAS pins excluded from JSON, and no
// OAuth tokens are stored in the receipt.
// ProviderRenewalReceipt хранит RUNNING или SUCCEEDED вместе с immutable входной identity и
// необязательным produced descriptor. API UID/resourceVersion являются локальными CAS-привязками и
// исключены из JSON; OAuth-токены в квитанции не хранятся.
type ProviderRenewalReceipt struct {
	Schema          string
	Identity        ProviderRenewalIdentity
	State           string
	Produced        *ProviderCredentialDescriptor
	UID             string `json:"-"`
	ResourceVersion string `json:"-"`
}

// validProviderRenewalIdentity checks typed references, visible bounded claimant, positive JSON-safe
// versions, nonzero expiry, exact previous-descriptor shape and lowercase request digest. It does not
// check current expiry or prove previous credential eligibility.
// validProviderRenewalIdentity проверяет типизированные ссылки, ограниченный видимый claimant,
// положительные безопасные для JSON версии, ненулевой срок, точную форму предыдущего descriptor и
// lowercase request digest. Текущий срок и допустимость предыдущего credential не проверяются.
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

// renewalVisibleASCII rejects spaces, controls and non-ASCII runes in a claimant; empty input passes
// here and is rejected by the identity validator's separate length check.
// renewalVisibleASCII отклоняет пробелы, управляющие и не-ASCII руны в claimant; пустой ввод здесь
// допускается и отдельно отклоняется проверкой длины в identity validator.
func renewalVisibleASCII(value string) bool {
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

// providerRenewalReceiptName uses 16 hash bytes of account and task separated by NUL, reserving one name
// across retries and claim generations; full identity equality is enforced when parsing that name's
// record.
// providerRenewalReceiptName использует 16 байтов хеша разделённых NUL account и task, резервируя одно
// имя для повторов и поколений claim; полное равенство identity проверяется при разборе записи по этому
// имени.
func providerRenewalReceiptName(identity ProviderRenewalIdentity) string {
	digest := sha256.Sum256([]byte(identity.AccountRef + "\x00" + identity.TaskRef))
	return "provider-renewal-" + hex.EncodeToString(digest[:16])
}

// parseProviderRenewalReceipt requires a mutable managed Secret with exact task/account metadata and one
// bounded canonical JSON record matching the entire identity. State and produced descriptor must agree;
// the produced name is task-derived, and live API pins are attached only after validation.
// parseProviderRenewalReceipt требует изменяемый управляемый Secret с точными task/account metadata и
// одной ограниченной канонической JSON-записью, совпадающей со всей identity. State согласуется с
// produced descriptor; produced name выводится из task, API-привязки добавляются только после проверки.
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

// BeginProviderCredentialRenewal creates the durable RUNNING reservation only for a fresh valid
// identity. created=true requires an acknowledged Create plus validated readback; AlreadyExists or an
// unknown write outcome never authorizes another OAuth effect.
// BeginProviderCredentialRenewal создаёт durable RUNNING резервирование только для действующей
// корректной identity. created=true требует подтверждённый Create и проверенный ответ; AlreadyExists или
// неопределённый исход записи не разрешает ещё один OAuth эффект.
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

// ReadProviderCredentialRenewal rereads and validates the exact stored identity without requiring its
// original expiry to remain current, allowing outcome observation without replaying OAuth.
// ReadProviderCredentialRenewal перечитывает и проверяет точную сохранённую identity без требования
// актуальности её исходного срока, позволяя наблюдать исход без повтора OAuth.
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

// CompleteProviderCredentialRenewal verifies the actually published immutable credential before a
// UID/resourceVersion-guarded receipt update. An identical SUCCEEDED replay returns its existing result
// without another write; stale or divergent state conflicts.
// CompleteProviderCredentialRenewal проверяет фактически опубликованный immutable credential до
// обновления квитанции с контролем UID/resourceVersion. Идентичный повтор SUCCEEDED возвращает
// существующий результат без новой записи; устаревшее или другое состояние даёт конфликт.
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

// RecoverProviderRenewalCredential reads only the deterministic account/task result name and verifies
// broker-owned immutable content before returning metadata. NotFound is found=false; no Create, OAuth
// call or retry of the external effect occurs.
// RecoverProviderRenewalCredential читает только детерминированное имя результата account/task и
// проверяет immutable содержимое владельца broker до возврата metadata. NotFound означает found=false;
// Create, вызов OAuth и повтор внешнего эффекта не выполняются.
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
