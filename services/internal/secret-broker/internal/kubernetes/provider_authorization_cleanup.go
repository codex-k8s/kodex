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
	// ProviderCleanupAuthorization selects an observed mutable authorization attempt as the exact cleanup
	// target, requiring its UID and resourceVersion.
	// ProviderCleanupAuthorization выбирает наблюдавшуюся изменяемую authorization attempt как точную
	// cleanup цель с обязательными UID и resourceVersion.
	ProviderCleanupAuthorization = "AUTHORIZATION_ATTEMPT"
	// ProviderCleanupAbsence selects a confirmed absence snapshot that may reserve the deterministic name by
	// Create; it carries no prior object pins.
	// ProviderCleanupAbsence выбирает снимок подтверждённого отсутствия, позволяющий занять
	// детерминированное имя через Create; привязки прежнего объекта отсутствуют.
	ProviderCleanupAbsence = "AUTHORIZATION_ABSENCE"
	// ProviderAuthorizationPresent reports a validated pending-attempt object whose current
	// UID/resourceVersion are returned for a later fenced mutation.
	// ProviderAuthorizationPresent обозначает проверенный pending-attempt объект, чьи текущие
	// UID/resourceVersion возвращаются для последующего изменения с предусловиями.
	ProviderAuthorizationPresent = "PRESENT"
	// ProviderAuthorizationAbsent reports an absent authorization or credential name whose full two-name
	// fencing has not yet been proved.
	// ProviderAuthorizationAbsent обозначает отсутствующее authorization или credential имя, для которого
	// полная блокировка обоих имён ещё не доказана.
	ProviderAuthorizationAbsent = "ABSENT_UNFENCED"
	// ProviderAuthorizationFenced reports a validated authorization tombstone and, when no produced
	// credential exists, a validated credential-name fence; one pending tombstone alone is insufficient.
	// ProviderAuthorizationFenced обозначает проверенный authorization tombstone и, при отсутствии
	// произведённого credential, проверенный credential-name fence; одного pending tombstone недостаточно.
	ProviderAuthorizationFenced = "CONFIRMED_ABSENT"
	// providerCleanupFenceLabel distinguishes immutable name-reserving tombstones from authorization
	// attempts and credential payloads.
	// providerCleanupFenceLabel отличает immutable tombstones, резервирующие имена, от authorization
	// attempts и credential payloads.
	providerCleanupFenceLabel = "provider-credentials.kodex.dev/cleanup-fence"
	// providerCleanupFenceKey stores the single metadata-only JSON fence record after authorization material
	// is removed.
	// providerCleanupFenceKey хранит единственную JSON-запись fence только с metadata после удаления
	// authorization material.
	providerCleanupFenceKey = "fence.json"
	// providerCleanupFenceSchema pins the accepted tombstone format; parsing rejects an unknown schema
	// rather than upgrading it.
	// providerCleanupFenceSchema закрепляет допустимый формат tombstone; разбор отклоняет неизвестную схему,
	// а не преобразует её.
	providerCleanupFenceSchema = "kodex.provider-cleanup-fence.v1"
	// providerCleanupFenceMaximum sets the eight-KiB accepted fence/cleanup-receipt JSON size; parsers
	// reject larger records and receipt persistence checks the same bound.
	// providerCleanupFenceMaximum задаёт допустимый размер JSON fence и cleanup-квитанции восемь КиБ; parser
	// отклоняет большую запись, а сохранение квитанции проверяет ту же границу.
	providerCleanupFenceMaximum = 8 << 10
)

// ProviderAuthorizationCleanupTarget binds task, account, authorization and deterministic materializer
// attempt to a generation and either exact object pins or explicit absence kind; it is assigned and
// authorized above storage.
// ProviderAuthorizationCleanupTarget связывает task, account, authorization и детерминированную
// materializer attempt с generation и либо точными привязками объекта, либо явным absence kind;
// назначение и авторизация выполняются выше хранилища.
type ProviderAuthorizationCleanupTarget struct {
	TaskRef, AccountRef, AuthorizationAttemptRef, MaterializerAttemptRef string
	Kind, UID, ResourceVersion                                           string
	Generation                                                           int64
}

// ProviderAuthorizationCleanupObservation returns metadata for the observed cleanup state, server-read
// target pins and any exact produced credential; it contains no authorization or token payload.
// ProviderAuthorizationCleanupObservation возвращает metadata наблюдённого cleanup состояния,
// прочитанные сервером привязки цели и точный произведённый credential при наличии; authorization или
// token payload отсутствует.
type ProviderAuthorizationCleanupObservation struct {
	State              string
	Target             ProviderAuthorizationCleanupTarget
	ProducedCredential *ProviderCredentialDescriptor
}

// ProviderCredentialCleanupResult records a terminal cleanup digest and an optional replacement
// credential descriptor for the owner to handle as another exact target, without returning credential
// bytes.
// ProviderCredentialCleanupResult фиксирует terminal cleanup digest и необязательный descriptor
// заменившего credential, который владелец обрабатывает как другую точную цель, без возврата credential
// байтов.
type ProviderCredentialCleanupResult struct {
	TerminalReceipt    string
	ProducedCredential *ProviderCredentialDescriptor
}

// providerCleanupFence retains only provenance and optional original UID/resourceVersion after material
// removal. Its immutable Secret occupies the original deterministic name to prevent delayed writers from
// recreating authorization data.
// providerCleanupFence сохраняет только происхождение и необязательные исходные UID/resourceVersion
// после удаления material. Его immutable Secret занимает исходное детерминированное имя, запрещая
// запоздавшим writer повторно создавать authorization данные.
type providerCleanupFence struct {
	Schema, Kind, AccountRef, AuthorizationAttemptRef, MaterializerAttemptRef string
	SecretName                                                                string
	OriginalUID, OriginalResourceVersion                                      string
}

// validAuthorizationCleanupIdentity validates typed cleanup references and a positive generation, then
// requires the materializer reference derived from authorization attempt and account; it checks binding
// shape, not owner authority.
// validAuthorizationCleanupIdentity проверяет типизированные cleanup ссылки и положительное generation,
// затем требует materializer reference, выведенную из authorization attempt и account; проверяется форма
// привязки, не полномочия владельца.
func validAuthorizationCleanupIdentity(target ProviderAuthorizationCleanupTarget) bool {
	digest := sha256.Sum256([]byte(target.AuthorizationAttemptRef + "\x00" + target.AccountRef))
	return validProviderCleanupReference(target.TaskRef, "pcct_") &&
		validProviderCleanupReference(target.AccountRef, "pacc_") &&
		validProviderCleanupReference(target.AuthorizationAttemptRef, "pauth_") && target.Generation > 0 &&
		target.MaterializerAttemptRef == "pmat_"+hex.EncodeToString(digest[:16])
}

// validAuthorizationCleanupTarget requires the validated lineage plus exact UID/resourceVersion for an
// authorization object or empty pins for absence; unknown and mixed target kinds are rejected.
// validAuthorizationCleanupTarget требует проверенное происхождение и точные UID/resourceVersion для
// authorization объекта либо пустые привязки для absence; неизвестные и смешанные target kinds
// отклоняются.
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

// ObserveProviderAuthorizationCleanup accepts an identity-only request, reads pending and produced-name
// metadata and distinguishes present, unfenced absence and validated fences. It returns exact pins for
// later mutation, clears fetched DTO data and performs no provider polling or writes.
// ObserveProviderAuthorizationCleanup принимает запрос только с identity, читает metadata pending и
// produced имени и различает наличие, отсутствие без fence и проверенные fences. Возвращает точные
// привязки для изменения, обнуляет Data полученных DTO и не выполняет provider polling или запись.
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

// FenceProviderAuthorization replaces only the exact observed mutable attempt using its API version, or
// Creates an immutable tombstone for a confirmed absence. Matching fences are idempotent; definitive CAS
// rejection is SnapshotChanged, while an unknown write outcome remains a general error.
// FenceProviderAuthorization заменяет только точную наблюдавшуюся mutable attempt с её API-версией либо
// создаёт immutable tombstone при подтверждённом отсутствии. Совпадающий fence идемпотентен; доказанный
// CAS-отказ даёт SnapshotChanged, неопределённый исход записи остаётся общей ошибкой.
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

// FenceAuthorizationCredentialName first requires the matching authorization fence, then attempts Create
// at the deterministic credential name. An existing valid credential is returned as a separate exact
// target; neither that credential nor a foreign object is overwritten.
// FenceAuthorizationCredentialName сначала требует соответствующий authorization fence, затем пробует
// Create по детерминированному credential имени. Существующий корректный credential возвращается
// отдельной точной целью; он и посторонний объект не перезаписываются.
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

// readAuthorizationProducedCredential accepts a matching metadata-only credential fence or validates the
// actual immutable broker credential against account, attempt and content. allowAbsent controls only
// NotFound tolerance, never bypasses existing-object validation.
// readAuthorizationProducedCredential допускает соответствующий credential fence только с metadata либо
// проверяет фактический immutable broker credential по account, attempt и содержимому. allowAbsent
// управляет только допустимостью NotFound и не обходит проверку существующего объекта.
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

// providerCleanupFenceSecret constructs an immutable managed Secret with a metadata-only fence and exact
// object name; the caller performs CAS/Create and owns clearing the returned data buffer.
// providerCleanupFenceSecret строит immutable управляемый Secret с fence только из metadata и точным
// именем объекта; вызывающий выполняет CAS/Create и обнуляет возвращённый буфер Data.
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

// parseProviderCleanupFence validates managed immutable identity, namespace, one bounded canonical JSON
// record, expected kind/account/name and optional original API pins. Authorization lineage equality is
// checked separately by fenceMatchesAuthorization.
// parseProviderCleanupFence проверяет управляемую immutable identity, namespace, одну ограниченную
// каноническую JSON-запись, ожидаемые kind/account/name и необязательные исходные API-привязки.
// Равенство authorization lineage отдельно проверяет fenceMatchesAuthorization.
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

// fenceMatchesAuthorization compares account and authorization/materializer lineage only; kind, object
// identity and original UID/resourceVersion must be checked by the surrounding parser and caller.
// fenceMatchesAuthorization сравнивает только account и authorization/materializer lineage; kind,
// identity объекта и исходные UID/resourceVersion проверяют окружающие parser и вызывающий.
func fenceMatchesAuthorization(fence providerCleanupFence, target ProviderAuthorizationCleanupTarget) bool {
	return fence.AccountRef == target.AccountRef && fence.AuthorizationAttemptRef == target.AuthorizationAttemptRef &&
		fence.MaterializerAttemptRef == target.MaterializerAttemptRef
}
