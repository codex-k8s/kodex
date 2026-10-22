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
	// providerAttemptLabel identifies mutable device-authorization state, not immutable credential output or
	// cleanup tombstones.
	// providerAttemptLabel выделяет изменяемое состояние device-authorization, а не immutable credential
	// результат или cleanup tombstones.
	providerAttemptLabel = "provider-credentials.kodex.dev/authorization-attempt"
	// providerCredentialLabel identifies immutable credential objects published by secret-broker; runtime
	// and bootstrap publishers have separate provenance predicates.
	// providerCredentialLabel выделяет immutable credential объекты издателя secret-broker; для runtime и
	// bootstrap существуют отдельные предикаты происхождения.
	providerCredentialLabel = "provider-credentials.kodex.dev/credential"
	// providerAttemptRefAnnotation binds an authorization or credential object to its originating attempt or
	// renewal task reference.
	// providerAttemptRefAnnotation связывает authorization или credential объект со ссылкой исходной attempt
	// либо renewal task.
	providerAttemptRefAnnotation = "provider-credentials.kodex.dev/attempt-ref"
	// providerAccountRefAnnotation stores the broker/bootstrap account binding checked before exact
	// credential use or cleanup.
	// providerAccountRefAnnotation хранит account-привязку broker/bootstrap, проверяемую перед точным
	// использованием credential или cleanup.
	providerAccountRefAnnotation = "provider-credentials.kodex.dev/account-ref"
	// providerContentSHAAnnotation records broker/bootstrap content provenance; actual auth.json bytes and
	// the companion digest entry are checked independently.
	// providerContentSHAAnnotation фиксирует происхождение содержимого broker/bootstrap; фактические байты
	// auth.json и соседний элемент digest проверяются отдельно.
	providerContentSHAAnnotation = "provider-credentials.kodex.dev/content-sha256"
	// providerManagedByLabel records the exact component publisher rather than treating any platform-looking
	// Secret as trusted material.
	// providerManagedByLabel указывает точного издателя-компонент, не признавая любой похожий на
	// платформенный Secret доверенным material.
	providerManagedByLabel = "app.kubernetes.io/managed-by"
	// providerPartOfLabel requires Kodex membership alongside the exact publisher and account/digest
	// binding; this label alone grants no access.
	// providerPartOfLabel требует принадлежность Kodex вместе с точным издателем и account/digest привязкой;
	// сама метка доступа не даёт.
	providerPartOfLabel = "app.kubernetes.io/part-of"
	// providerRuntimeManagedLabel identifies credentials published by runtime-controller for its separate
	// exact ownership predicate.
	// providerRuntimeManagedLabel выделяет credentials издателя runtime-controller для отдельного предиката
	// точного владения.
	providerRuntimeManagedLabel = "runtime.kodex.dev/managed"
	// providerRuntimeAccountRef is the runtime-controller account annotation, distinct from the
	// broker/bootstrap annotation key.
	// providerRuntimeAccountRef задаёт account annotation runtime-controller, отличную от ключа
	// broker/bootstrap.
	providerRuntimeAccountRef = "runtime.kodex.dev/provider-account-ref"
	// providerRuntimeContentSHA is the runtime-controller digest annotation paired with exact content
	// validation.
	// providerRuntimeContentSHA задаёт digest annotation runtime-controller, используемую вместе с точной
	// проверкой содержимого.
	providerRuntimeContentSHA = "runtime.kodex.dev/provider-credential-digest"
	// providerSecretBrokerManager is the exact managed-by value required for broker credentials, cleanup
	// fences, receipts and credential projections.
	// providerSecretBrokerManager задаёт точное managed-by значение для broker credentials, cleanup fences,
	// квитанций и credential projections.
	providerSecretBrokerManager = "secret-broker"
	// providerRuntimeManager is the explicitly supported runtime-controller publisher identity for immutable
	// provider credential reads and cleanup.
	// providerRuntimeManager задаёт явно поддерживаемую identity издателя runtime-controller для чтения и
	// cleanup immutable provider credentials.
	providerRuntimeManager = "runtime-controller"
	// providerBootstrapManager identifies the dedicated canonical bootstrap publisher rather than historical
	// installer or local-development objects.
	// providerBootstrapManager обозначает выделенного канонического bootstrap издателя, а не исторические
	// объекты installer или локальной разработки.
	providerBootstrapManager = "kodex-provider-bootstrap"
	// providerBootstrapLabel pins the supported bootstrap provenance version in addition to publisher,
	// account and digest checks.
	// providerBootstrapLabel закрепляет поддерживаемую версию bootstrap происхождения помимо проверок
	// издателя, account и digest.
	providerBootstrapLabel = "provider-credentials.kodex.dev/bootstrap"
	// providerAuthJSONKey contains credential JSON bytes; materialization uses independent copies and
	// returned exact-read copies must be cleared by their callers.
	// providerAuthJSONKey содержит байты credential JSON; materialization использует независимые копии, а
	// возвращённые exact-read копии обнуляет вызывающий.
	providerAuthJSONKey = "auth.json"
	// providerAuthSHA256Key contains the exact lowercase hexadecimal digest text without a newline, separate
	// from auth.json payload bytes.
	// providerAuthSHA256Key содержит точный lowercase шестнадцатеричный текст digest без перевода строки
	// отдельно от payload auth.json.
	providerAuthSHA256Key = "auth.sha256"
	// providerCredentialMaximumSize limits newly materialized credential JSON to one MiB; descriptor parsing
	// and existing-object validation are separate paths.
	// providerCredentialMaximumSize ограничивает вновь материализуемый credential JSON одним МиБ; разбор
	// descriptor и проверка существующего объекта выполняются отдельно.
	providerCredentialMaximumSize = 1 << 20
	// providerReferenceMaximumSize bounds typed cleanup references to 96 bytes before their suffix character
	// validation.
	// providerReferenceMaximumSize ограничивает типизированные cleanup ссылки 96 байтами до проверки
	// символов suffix.
	providerReferenceMaximumSize = 96
	// providerResourceVersionMax bounds strict cleanup resourceVersion metadata to 128 bytes without
	// assuming a numeric API version format.
	// providerResourceVersionMax ограничивает строгие cleanup metadata resourceVersion 128 байтами без
	// предположения о числовом формате API-версии.
	providerResourceVersionMax = 128
)

var (
	// providerReferencePattern admits eight to 96 ASCII reference characters with an alphabetic first
	// character; typed prefixes are enforced by specialized cleanup validators.
	// providerReferencePattern допускает ASCII-ссылки длиной восемь–96 символов с буквой в начале;
	// типизированные префиксы проверяют специализированные cleanup validator.
	providerReferencePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{7,95}$`)
	// providerCleanupReferencePattern limits a typed cleanup reference suffix to ASCII alphanumerics,
	// underscore and hyphen; prefix and total length are checked separately.
	// providerCleanupReferencePattern ограничивает suffix типизированной cleanup ссылки ASCII-буквами,
	// цифрами, underscore и hyphen; prefix и общая длина проверяются отдельно.
	providerCleanupReferencePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	// providerCredentialNamePattern accepts bounded lowercase DNS-label-shaped credential names, rejecting
	// uppercase, separators and leading/trailing hyphens.
	// providerCredentialNamePattern допускает ограниченные lowercase имена credential формы DNS label,
	// отклоняя верхний регистр, разделители и hyphen по краям.
	providerCredentialNamePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)
	// providerCredentialUIDPattern accepts lowercase UUID-shaped Kubernetes UIDs for strict cleanup pins
	// without querying object existence or UUID version semantics.
	// providerCredentialUIDPattern допускает lowercase UUID-форму Kubernetes UID для строгих
	// cleanup-привязок без поиска объекта и проверки семантики версии UUID.
	providerCredentialUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	// ErrProviderAttemptNotFound represents authoritative API absence of an authorization attempt, distinct
	// from malformed state or an upstream API error.
	// ErrProviderAttemptNotFound обозначает подтверждённое API отсутствие authorization attempt отдельно от
	// повреждённого состояния и ошибки upstream API.
	ErrProviderAttemptNotFound = errors.New("provider authorization attempt is not found")
	// ErrProviderAttemptConflict rejects invalid authorization state, mismatched attempt lineage, divergent
	// terminal replay or failed CAS.
	// ErrProviderAttemptConflict отклоняет неверное authorization состояние, несовпадающее происхождение
	// attempt, другой terminal повтор или неуспешный CAS.
	ErrProviderAttemptConflict = errors.New("provider authorization attempt conflicts with stored state")
	// ErrProviderCredentialConflict rejects a credential whose exact pins, content or publisher binding
	// disagree with the requested materialization.
	// ErrProviderCredentialConflict отклоняет credential, чьи точные привязки, содержимое или издатель не
	// соответствуют запрошенной materialization.
	ErrProviderCredentialConflict = errors.New("provider credential conflicts with stored materialization")
	// ErrProviderCredentialInputInvalid rejects invalid credential materialization/read inputs and malformed
	// or expired new renewal reservations before the associated effect.
	// ErrProviderCredentialInputInvalid отклоняет неверный вход materialization/чтения credential и
	// повреждённые или просроченные новые renewal резервирования до соответствующего эффекта.
	ErrProviderCredentialInputInvalid = errors.New("provider credential materialization input is invalid")
	// ErrProviderCredentialCleanupConflict reports a cleanup target, stored state or API precondition
	// mismatch; it does not by itself prove that no write occurred.
	// ErrProviderCredentialCleanupConflict сообщает о несовпадении cleanup цели, сохранённого состояния или
	// API предусловия; сама ошибка не доказывает отсутствие записи.
	ErrProviderCredentialCleanupConflict = errors.New("provider credential cleanup conflicts with stored materialization")
	// ErrProviderCredentialCleanupInvalid rejects malformed cleanup identities or exact descriptors before
	// the corresponding storage action.
	// ErrProviderCredentialCleanupInvalid отклоняет повреждённые cleanup identity или точные descriptors до
	// соответствующего действия хранилища.
	ErrProviderCredentialCleanupInvalid = errors.New("provider credential cleanup input is invalid")
	// ErrProviderAuthorizationCleanupSnapshotChanged joins cleanup conflict with a precise no-fence-write
	// result for this call: the observed snapshot changed before persistence or the API definitively
	// rejected CAS. A lost write response is not this sentinel.
	// ErrProviderAuthorizationCleanupSnapshotChanged объединяет cleanup conflict с точным результатом
	// отсутствия записи fence в этом вызове: снимок изменился до сохранения либо API доказанно отклонил CAS.
	// Потерянный ответ записи этой ошибкой не считается.
	ErrProviderAuthorizationCleanupSnapshotChanged = errors.Join(ErrProviderCredentialCleanupConflict,
		errors.New("provider authorization cleanup snapshot changed before fencing"))
)

// ProviderCredentialDescriptor contains only exact Secret name, UID, resourceVersion and content digest;
// the descriptor itself never carries credential bytes or establishes authority.
// ProviderCredentialDescriptor содержит только точные имя Secret, UID, resourceVersion и digest
// содержимого; сам descriptor не несёт credential байтов и не устанавливает полномочия.
type ProviderCredentialDescriptor struct {
	SecretName, SecretUID, SecretResourceVersion, ContentSHA256 string
}

// ProviderAuthorizationAttempt carries shared mutable device-authorization state with user-facing
// verification metadata and an optional descriptor only on authorized success. ResourceVersion pins
// updates; raw provider token JSON is not embedded here.
// ProviderAuthorizationAttempt несёт общее изменяемое device-authorization состояние с пользовательскими
// verification metadata и необязательным descriptor только при authorized success. ResourceVersion
// привязывает обновления; сырой provider token JSON сюда не включается.
type ProviderAuthorizationAttempt struct {
	AttemptRef, AccountRef, MaterializerAttemptRef string
	SecretUID                                      string
	State, VerificationURI, UserCode               string
	ExternalAccountMasked, SafeFailureCode         string
	ExpiresAt                                      time.Time
	Credential                                     *ProviderCredentialDescriptor
	ResourceVersion                                string
}

// CreateProviderAuthorizationAttempt validates a pending attempt, creates its deterministic mutable
// Secret and returns created=true only after a valid create response. AlreadyExists reads and compares
// the same pending fields without overwriting; temporary DTO bytes are cleared.
// CreateProviderAuthorizationAttempt проверяет pending attempt, создаёт детерминированный изменяемый
// Secret и возвращает created=true только после корректного ответа создания. AlreadyExists читает и
// сравнивает те же pending поля без перезаписи; временные байты DTO обнуляются.
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

// GetProviderAuthorizationAttempt validates the materializer reference, reads its deterministic Secret
// and parses known attempt state. NotFound remains distinct; parsing copies string metadata before the
// fetched byte slices are cleared.
// GetProviderAuthorizationAttempt проверяет materializer reference, читает детерминированный Secret и
// разбирает известное attempt состояние. NotFound отличается от других ошибок; parser копирует string
// metadata до обнуления полученных байтовых срезов.
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

// CompleteProviderAuthorizationAttempt accepts a valid terminal result, verifies current
// attempt/account/materializer lineage and updates a pending object with its fetched
// UID/resourceVersion. Matching terminal replay is read-only; divergent terminal state and API conflict
// are rejected.
// CompleteProviderAuthorizationAttempt принимает корректный terminal результат, сверяет текущие
// attempt/account/materializer lineage и обновляет pending объект с прочитанными UID/resourceVersion.
// Совпадающий terminal повтор выполняет только чтение; другой terminal state и API conflict отклоняются.
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

// CreateProviderCredential bounds and syntax-checks JSON, creates an immutable account/attempt-derived
// Secret with copied payload and digest, and validates the returned or preexisting object. AlreadyExists
// never overwrites different material; caller input bytes remain caller-owned.
// CreateProviderCredential ограничивает JSON и проверяет синтаксис, создаёт immutable Secret с именем из
// account/attempt, копией payload и digest и проверяет возвращённый либо существующий объект.
// AlreadyExists не перезаписывает другое material; входные байты остаются у вызывающего.
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

// ReadProviderCredentialExact requires exact account binding, UID, resourceVersion, supported publisher
// and recomputed content digest before returning a plaintext copy. Fetched DTO bytes are cleared; the
// caller clears the independent returned bytes.
// ReadProviderCredentialExact требует точные account-привязку, UID, resourceVersion, поддерживаемого
// издателя и пересчитанный digest содержимого до возврата копии plaintext. Байты полученного DTO
// обнуляются; отдельные возвращённые байты обнуляет вызывающий.
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

// DiscardProviderAuthorizationAttempt derives a deterministic compensation task, fences the exact
// authorization snapshot and then its credential name; a discovered credential is cleaned as an exact
// target. Failure does not complete the owner's durable cleanup task.
// DiscardProviderAuthorizationAttempt выводит детерминированную compensation task, блокирует точный
// authorization снимок и затем credential имя; обнаруженный credential очищается как точная цель. Ошибка
// не завершает durable cleanup task владельца.
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

// DiscardProviderCredential requires account/attempt-derived name and exact descriptor shape, then
// delegates to durable cleanup with a deterministic compensation identity. A discovered replacement
// remains a conflict rather than being silently discarded.
// DiscardProviderCredential требует имя из account/attempt и точную форму descriptor, затем передаёт
// действие durable cleanup с детерминированной compensation identity. Обнаруженная замена остаётся
// конфликтом, а не молча удаляется.
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

// providerDiscardError preserves cleanup invalid/conflict provenance while joining the caller-facing
// attempt or credential conflict; unrelated transport errors pass through unchanged.
// providerDiscardError сохраняет происхождение cleanup invalid/conflict, присоединяя видимый вызывающему
// attempt или credential conflict; посторонние транспортные ошибки проходят без изменения.
func providerDiscardError(err, conflict error) error {
	if errors.Is(err, ErrProviderCredentialCleanupInvalid) || errors.Is(err, ErrProviderCredentialCleanupConflict) {
		return errors.Join(conflict, err)
	}
	return err
}

// CleanupProviderCredential first reuses an exact terminal receipt, otherwise validates and deletes only
// the pinned credential with API preconditions and reserves its name with an immutable fence. A valid
// replacement is reported separately without deletion; completion requires verified fence or replacement
// readback.
// CleanupProviderCredential сначала использует точную terminal квитанцию, иначе проверяет и удаляет
// только закреплённый credential с API предусловиями и резервирует имя immutable fence. Корректная
// замена сообщается отдельно без удаления; завершение требует проверенного чтения fence либо замены.
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

// cleanupReplacementDescriptor validates namespace/name and derives exact metadata from a supported
// immutable account-bound replacement, including actual content verification; it never deletes the
// replacement.
// cleanupReplacementDescriptor проверяет namespace/name и выводит точные metadata из поддерживаемой
// immutable замены, привязанной к account, со сверкой фактического содержимого; замена не удаляется.
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

// providerAttemptSecret encodes mutable device-authorization state and an optional credential descriptor
// into a Secret without assigning API identity or validating authority; callers validate input and clear
// data buffers.
// providerAttemptSecret кодирует изменяемое device-authorization состояние и необязательный credential
// descriptor в Secret без назначения API identity и проверки полномочий; вызывающие проверяют вход и
// обнуляют буферы Data.
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

// providerAttemptFromSecret requires a labeled mutable Opaque object with API pins, decodes known state
// fields and validates pending or terminal shape. It does not enforce unknown Data keys, canonical JSON
// or URI trust policy.
// providerAttemptFromSecret требует помеченный mutable Opaque объект с API-привязками, разбирает
// известные state поля и проверяет pending либо terminal форму. Неизвестные ключи Data, канонический
// JSON и политика доверия URI здесь не проверяются.
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

// validateProviderAttempt validates references, nonzero expiry and bounded metadata, then requires
// consistent pending, authorized or failed/expired fields. It neither checks expiry freshness nor parses
// the verification URI as a trusted URL.
// validateProviderAttempt проверяет ссылки, ненулевой срок и ограниченные metadata, затем требует
// согласованные pending, authorized либо failed/expired поля. Актуальность срока и доверенность
// verification URI как URL не проверяются.
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

// providerAttemptName validates a materializer reference and hashes its bytes into a bounded
// authorization Secret name; the name does not confer account or caller authority.
// providerAttemptName проверяет materializer reference и хеширует байты в ограниченное имя authorization
// Secret; имя не предоставляет полномочий account или вызывающего.
func providerAttemptName(materializerAttemptRef string) (string, error) {
	if !providerReferencePattern.MatchString(materializerAttemptRef) {
		return "", ErrProviderAttemptConflict
	}
	digest := sha256.Sum256([]byte(materializerAttemptRef))
	return "provider-auth-" + hex.EncodeToString(digest[:16]), nil
}

// providerCredentialName derives a bounded credential name from NUL-separated account and attempt
// references using 16 SHA-256 bytes; reference validation and ownership remain the caller's
// responsibility.
// providerCredentialName выводит ограниченное имя credential из разделённых NUL account и attempt,
// используя 16 байтов SHA-256; проверку ссылок и владения выполняет вызывающий.
func providerCredentialName(accountRef, attemptRef string) string {
	digest := sha256.Sum256([]byte(accountRef + "\x00" + attemptRef))
	return "provider-credential-" + hex.EncodeToString(digest[:16])
}

// providerCredentialDescriptor validates an immutable broker-owned credential against account, attempt
// and expected digest, recomputes the actual JSON hash and returns metadata only; cleanup supports
// additional publishers through separate predicates.
// providerCredentialDescriptor проверяет immutable credential владельца broker по account, attempt и
// ожидаемому digest, пересчитывает фактический хеш JSON и возвращает только metadata; cleanup
// поддерживает других издателей отдельными предикатами.
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

// validProviderCredentialDescriptor checks nonempty bounded object pins and lowercase SHA-256
// representation; strict Kubernetes name, UID and visible resourceVersion syntax are added by the
// cleanup validator.
// validProviderCredentialDescriptor проверяет непустые ограниченные привязки объекта и lowercase
// представление SHA-256; строгий синтаксис имени Kubernetes, UID и видимого resourceVersion добавляет
// cleanup validator.
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

// validProviderCleanupReference requires the exact typed prefix, at least eight suffix characters, the
// total size bound and a closed ASCII suffix alphabet; it does not resolve the referenced resource.
// validProviderCleanupReference требует точный типизированный prefix, минимум восемь символов suffix,
// ограниченную общую длину и закрытый ASCII-алфавит suffix; ресурс по ссылке не разрешается.
func validProviderCleanupReference(value, prefix string) bool {
	if len(value) < len(prefix)+8 || len(value) > providerReferenceMaximumSize || !strings.HasPrefix(value, prefix) {
		return false
	}
	return providerCleanupReferencePattern.MatchString(value[len(prefix):])
}

// validProviderCredentialCleanupDescriptor strengthens the general descriptor with DNS-label-shaped
// name, lowercase UUID-shaped UID and bounded visible ASCII resourceVersion before cleanup can reach
// storage.
// validProviderCredentialCleanupDescriptor усиливает общий descriptor именем формы DNS label, lowercase
// UUID-формой UID и ограниченным видимым ASCII resourceVersion до обращения cleanup к хранилищу.
func validProviderCredentialCleanupDescriptor(value ProviderCredentialDescriptor) bool {
	return providerCredentialNamePattern.MatchString(value.SecretName) &&
		providerCredentialUIDPattern.MatchString(value.SecretUID) &&
		validProviderResourceVersion(value.SecretResourceVersion) &&
		validProviderCredentialDescriptor(value)
}

// validProviderResourceVersion accepts one to 128 visible ASCII bytes without spaces or controls;
// Kubernetes versions are treated as opaque rather than parsed as integers.
// validProviderResourceVersion допускает один–128 видимых ASCII-байтов без пробелов и управляющих
// символов; Kubernetes версии считаются непрозрачными, а не разбираются как числа.
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

// providerCredentialCleanupSecretMatches requires exact API pins, immutable Opaque metadata, exactly two
// payload keys, a supported account/digest publisher and JSON whose actual SHA-256 matches the
// descriptor. Namespace is checked by the surrounding store operation.
// providerCredentialCleanupSecretMatches требует точные API-привязки, immutable Opaque metadata, ровно
// два payload ключа, поддерживаемого account/digest издателя и JSON с фактическим SHA-256 descriptor.
// Namespace проверяет окружающая операция store.
func providerCredentialCleanupSecretMatches(
	secret *corev1.Secret,
	accountRef string,
	descriptor ProviderCredentialDescriptor,
) bool {
	if secret == nil {
		return false
	}
	serverOwned := providerCredentialSecretBrokerOwned(secret, accountRef, descriptor.ContentSHA256) ||
		providerCredentialRuntimeOwned(secret, accountRef, descriptor.ContentSHA256) ||
		providerCredentialBootstrapOwned(secret, accountRef, descriptor.ContentSHA256)
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

// providerCredentialSecretBrokerOwned checks only broker publisher labels and account/digest annotations
// on a nonnil object; immutable metadata, API pins and actual bytes are verified by its caller.
// providerCredentialSecretBrokerOwned проверяет только метки издателя broker и account/digest
// annotations ненулевого объекта; immutable metadata, API-привязки и фактические байты проверяет
// вызывающий.
func providerCredentialSecretBrokerOwned(secret *corev1.Secret, accountRef, digest string) bool {
	return secret.Labels[providerCredentialLabel] == "true" &&
		secret.Labels[providerManagedByLabel] == providerSecretBrokerManager &&
		secret.Labels[providerPartOfLabel] == "kodex" &&
		secret.Annotations[providerAccountRefAnnotation] == accountRef &&
		secret.Annotations[providerContentSHAAnnotation] == digest
}

// providerCredentialRuntimeOwned recognizes only the runtime-controller managed label set and its
// account/digest annotation keys; it does not verify bytes or establish domain authorization.
// providerCredentialRuntimeOwned распознаёт только managed-метки runtime-controller и его account/digest
// annotation ключи; байты и доменная авторизация не проверяются.
func providerCredentialRuntimeOwned(secret *corev1.Secret, accountRef, digest string) bool {
	return secret.Labels[providerRuntimeManagedLabel] == "true" &&
		secret.Labels[providerManagedByLabel] == providerRuntimeManager &&
		secret.Labels[providerPartOfLabel] == "kodex" &&
		secret.Annotations[providerRuntimeAccountRef] == accountRef &&
		secret.Annotations[providerRuntimeContentSHA] == digest
}

// providerCredentialBootstrapOwned requires the dedicated bootstrap publisher, v1 provenance label and
// exact account/digest annotations; historical installer/local-development publishers are not accepted
// by this predicate.
// providerCredentialBootstrapOwned требует выделенного bootstrap издателя, метку происхождения v1 и
// точные account/digest annotations; исторические издатели installer/локальной разработки этим
// предикатом не допускаются.
func providerCredentialBootstrapOwned(secret *corev1.Secret, accountRef, digest string) bool {
	return secret.Labels[providerBootstrapLabel] == "v1" &&
		secret.Labels[providerManagedByLabel] == providerBootstrapManager &&
		secret.Labels[providerPartOfLabel] == "kodex" &&
		secret.Annotations[providerAccountRefAnnotation] == accountRef &&
		secret.Annotations[providerContentSHAAnnotation] == digest
}

// samePendingProviderAttempt compares lineage, pending state, verification fields and expiry for create
// replay; API identity, terminal account metadata and failure/credential fields are not compared here.
// samePendingProviderAttempt сравнивает lineage, pending state, verification поля и срок для повтора
// создания; API identity, terminal account metadata и failure/credential поля здесь не сравниваются.
func samePendingProviderAttempt(left, right ProviderAuthorizationAttempt) bool {
	return left.AttemptRef == right.AttemptRef && left.AccountRef == right.AccountRef &&
		left.MaterializerAttemptRef == right.MaterializerAttemptRef && left.State == right.State &&
		left.VerificationURI == right.VerificationURI && left.UserCode == right.UserCode &&
		left.ExpiresAt.Equal(right.ExpiresAt)
}

// sameTerminalProviderAttempt compares lineage, terminal state, masked account, failure code and exact
// optional descriptor for replay; verification URI, user code, expiry and API resourceVersion do not
// define terminal equality.
// sameTerminalProviderAttempt сравнивает lineage, terminal state, маскированный account, failure code и
// точный необязательный descriptor для повтора; verification URI, user code, срок и API resourceVersion
// не определяют terminal равенство.
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
