package platform

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// runtimeProjectionMethod Names the exact runtime-controller materialization RPC used in
	// credential authority binding; a similar method name or payload field is not interchangeable
	// authority.
	// runtimeProjectionMethod Задаёт точный RPC материализации runtime-controller для привязки
	// credential authority. Похожее имя метода или поле payload не являются взаимозаменяемыми
	// полномочиями.
	runtimeProjectionMethod = "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeRuntimeCredentials"
	// assistantProjectionMethod Names the distinct system-assistant materialization RPC that permits
	// organization-scoped secret projection only under its verified owner execution.
	// assistantProjectionMethod Задаёт отдельный RPC материализации системного ассистента, допускающий
	// организационную проекцию секретов только в проверенном выполнении владельца.
	assistantProjectionMethod = "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeSystemAssistantCredentials"
	// sttProjectionMethod Names the exact transcription credential projection RPC for the STT workload
	// and pinned system configuration.
	// sttProjectionMethod Задаёт точный RPC проекции credential транскрипции для STT workload и
	// закреплённой системной конфигурации.
	sttProjectionMethod = "/stt.v1.TranscriptionCredentialProjectionService/ProjectTranscriptionCredential"
	// maximumProjectionItems Bounds decoded runtime secret projection metadata at 64 items before
	// resolving owner-scoped exact secret descriptors. It does not limit or read secret value bytes.
	// maximumProjectionItems Ограничивает декодированные метаданные runtime-проекции 64 элементами до
	// разрешения точных дескрипторов владельца. Значения секретов не читает и их размер не определяет.
	maximumProjectionItems = 64
)

// ResolveRuntimeCredentialProjection Requires the exact secret-broker resolve permission, valid
// runtime projection authority and a nonempty fence before owner-state resolution. Returns only
// credential/secret bindings; materialization and reading secret bytes belong to the broker.
// ResolveRuntimeCredentialProjection Требует точное resolve-разрешение secret-broker, допустимую
// runtime authority и непустой fence до разрешения состояния владельца. Возвращает лишь привязки
// credential и секретов; материализация и чтение значений принадлежат broker.
func (repository *Repository) ResolveRuntimeCredentialProjection(ctx context.Context, principal value.Principal, input platformrepo.RuntimeCredentialProjectionInput) (platformrepo.RuntimeCredentialProjection, error) {
	if !validCredentialProjectionOwner(principal, "platform.credential-projections.runtime.resolve") ||
		!repository.validRuntimeProjectionAuthority(input.Authority, false) || input.Fence == "" {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrForbidden
	}
	return repository.resolveRuntimeCredentialProjection(ctx, principal, input)
}

// ValidateRuntimeCredentialProjection Requires exact broker validate permission, read-validation
// authority and an empty fence, then re-resolves current owner state and compares
// provider/runtime-secret descriptors. Not-found/forbidden/conflict become false without error;
// other failures propagate. Validation performs no materialization or resend.
// ValidateRuntimeCredentialProjection Требует точное validate-разрешение broker, authority
// проверки чтения и пустой fence, затем заново разрешает состояние владельца и сравнивает
// дескрипторы. NotFound/Forbidden/Conflict дают false без ошибки, остальные сбои возвращаются.
// Материализации или повторной отправки нет.
func (repository *Repository) ValidateRuntimeCredentialProjection(ctx context.Context, principal value.Principal, input platformrepo.RuntimeCredentialProjectionInput) (bool, error) {
	if !validCredentialProjectionOwner(principal, "platform.credential-projections.runtime.validate") ||
		!repository.validRuntimeProjectionAuthority(input.Authority, true) || input.Fence != "" {
		return false, errs.ErrForbidden
	}
	resolved, err := repository.resolveRuntimeCredentialProjection(ctx, principal, input)
	if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return sameProviderBinding(resolved.ProviderCredential, input.ProviderCredential) &&
		sameRuntimeSecretDescriptors(resolved.RuntimeSecrets, input.RuntimeSecrets), nil
}

// resolveRuntimeCredentialProjection Validates bounded execution pins in read-only RepeatableRead,
// enforces tenant binding or reconstructs trusted authority from owner state, resolves the exact
// eligible lease/revision/provider and at most 64 exact scoped secret descriptors, caps expiry by
// authority and commits. Failures discard output; values are never read and rollback errors with
// the request context remain ignored.
// resolveRuntimeCredentialProjection Проверяет ограниченные привязки выполнения в read-only
// RepeatableRead, требует совпадение организации либо восстанавливает доверенную authority из
// состояния владельца. Разрешает точные пригодные lease, revision, provider и до 64 дескрипторов
// секретов нужной области, ограничивает срок authority и делает commit. Ошибка отбрасывает
// результат; значения не читаются, ошибка rollback с контекстом запроса игнорируется.
func (repository *Repository) resolveRuntimeCredentialProjection(ctx context.Context, principal value.Principal, input platformrepo.RuntimeCredentialProjectionInput) (platformrepo.RuntimeCredentialProjection, error) {
	if !validRuntimeProjectionInput(input) {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.RuntimeCredentialProjection{}, err
	}
	trusted := input.Authority.RPCProfile == transportprofile.TrustedCluster
	if !trusted && input.Authority.TenantID != current.organizationID {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrForbidden
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result platformrepo.RuntimeCredentialProjection
	if trusted {
		input.Authority, err = repository.resolveTrustedProjectionAuthority(ctx, tx, current.organizationID, input)
		if err != nil {
			return result, err
		}
		result.Authority = input.Authority
	}
	var rawSecrets []byte
	err = scanProviderCredentialProjection(tx.QueryRow(ctx, queryCredentialProjectionResolveRuntime, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": input.Authority.ActorID,
		"project_id": nullUUID(input.Authority.ProjectID), "lease_ref": input.LeaseRef,
		"system_assistant":  input.Authority.CallerFullMethod == assistantProjectionMethod,
		"workload_instance": input.WorkloadInstance, "generation": input.Generation, "fence": input.Fence,
		"runtime_revision_ref": input.RuntimeRevisionRef, "runtime_revision_digest": input.RuntimeRevisionDigest,
		"attempt": input.Attempt, "input_digest": input.InputDigest, "session_ref": input.SessionRef, "turn_ref": input.TurnRef,
	}), &result.ProviderCredential, &rawSecrets, &result.ExpiresAt)
	if err != nil {
		return platformrepo.RuntimeCredentialProjection{}, err
	}
	var stored []runtimecontract.RuntimeSecretProjection
	if err := jsonUnmarshal(rawSecrets, &stored); err != nil || len(stored) > maximumProjectionItems {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrConflict
	}
	// Область выбирается по проверенному exact RPC и owner-состоянию выполнения,
	// а не по project-контексту открытого экрана или descriptor из payload.
	secretScope := "PROJECT"
	if input.Authority.CallerFullMethod == assistantProjectionMethod && input.Authority.ProjectID == "" {
		secretScope = "ORGANIZATION"
	}
	for _, candidate := range stored {
		var descriptor entity.RuntimeSecretRevisionDescriptor
		var secretRef string
		err := tx.QueryRow(ctx, queryCredentialProjectionResolveRuntimeSecret, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "project_id": nullUUID(input.Authority.ProjectID), "scope_kind": secretScope,
			"secret_name": candidate.SecretName, "secret_key": candidate.SecretKey, "secret_uid": candidate.SecretUID,
			"secret_resource_version": candidate.SecretResourceVersion, "content_sha256": candidate.ContentSHA256,
		}).Scan(&secretRef, &descriptor.Revision, &descriptor.Namespace, &descriptor.SecretName, &descriptor.SecretKey,
			&descriptor.SecretUID, &descriptor.SecretResourceVersion, &descriptor.ContentSHA256)
		if errors.Is(err, pgx.ErrNoRows) {
			return platformrepo.RuntimeCredentialProjection{}, errs.ErrNotFound
		}
		if err != nil || candidate.Name == "" || secretRef == "" {
			return platformrepo.RuntimeCredentialProjection{}, errs.ErrUnavailable
		}
		result.RuntimeSecrets = append(result.RuntimeSecrets, platformrepo.RuntimeSecretProjectionBinding{
			Name: candidate.Name, SecretRef: secretRef, Descriptor: descriptor,
		})
	}
	if input.Authority.ExpiresAt.Before(result.ExpiresAt) {
		result.ExpiresAt = input.Authority.ExpiresAt
	}
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrConflict
	}
	return result, nil
}

// ResolveTranscriptionCredentialProjection Requires exact broker/STT authority and current
// subject, validates pinned ready system STT configuration/account/generation, resolves credential
// metadata and supported model/language, and caps a one-minute expiry by authority before commit.
// Provider capabilities are parsed but not otherwise evaluated here; no secret value is returned.
// ResolveTranscriptionCredentialProjection Требует точные broker/STT authority и текущего
// субъекта, проверяет закреплённые готовые конфигурацию STT, аккаунт и поколение, разрешает
// метаданные credential и поддерживаемые модель с языком. До commit ограничивает минутный срок
// authority. Возможности провайдера разбираются без дальнейшей оценки здесь; значения секретов не
// возвращаются.
func (repository *Repository) ResolveTranscriptionCredentialProjection(ctx context.Context, principal value.Principal, input platformrepo.TranscriptionCredentialProjectionInput) (platformrepo.TranscriptionCredentialProjection, error) {
	if !validCredentialProjectionOwner(principal, "platform.credential-projections.stt.resolve") ||
		!validRuntimeSecretSHA256(input.ConfigDigestSHA256) || input.ConfigRevision == 0 || input.ProviderCredentialGeneration == 0 || input.ProviderAccountRef == "" {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.TranscriptionCredentialProjection{}, err
	}
	identity := principal
	identity.ActorID, identity.AuthorityTenant = current.actorID, current.organizationID
	if !repository.validSTTProjectionAuthority(identity, input.Authority) || input.Authority.TenantID != current.organizationID {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrForbidden
	}
	var result platformrepo.TranscriptionCredentialProjection
	user, err := repository.ResolvePrincipal(ctx, value.Principal{ActorID: input.Authority.ActorID, AuthorityTenant: input.Authority.TenantID})
	if err != nil {
		return result, err
	}
	userScope, err := repository.resolveScope(ctx, user)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if input.Authority.RPCProfile == transportprofile.TrustedCluster {
		if err := repository.requireAccess(ctx, tx, userScope, "platform.stt.use", organizationTarget(userScope.organizationRef)); err != nil {
			return result, errs.ErrForbidden
		}
	}
	configuration, err := repository.getSystemSTTConfigurationTx(ctx, tx, userScope)
	if err != nil {
		return result, err
	}
	if !configuration.Ready || uint64(configuration.Revision) != input.ConfigRevision || configuration.Digest != input.ConfigDigestSHA256 ||
		configuration.ProviderAccountRef != input.ProviderAccountRef || uint64(configuration.ProviderCredentialGeneration) != input.ProviderCredentialGeneration {
		return result, errs.ErrNotFound
	}
	var model, language string
	var rawProviderCapabilities []byte
	err = scanProviderCredentialProjection(tx.QueryRow(ctx, queryCredentialProjectionResolveSTT, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "config_revision": input.ConfigRevision,
		"config_digest": input.ConfigDigestSHA256, "account_ref": input.ProviderAccountRef,
		"credential_generation": input.ProviderCredentialGeneration,
	}), &result.ProviderCredential, &model, &language, &rawProviderCapabilities)
	if err != nil {
		return platformrepo.TranscriptionCredentialProjection{}, err
	}
	var providerCapabilities map[string]any
	if jsonUnmarshal(rawProviderCapabilities, &providerCapabilities) != nil {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrUnavailable
	}
	if !systemSTTModelSupported(model, language) {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrNotFound
	}
	result.ExpiresAt = time.Now().UTC().Add(time.Minute)
	if input.Authority.ExpiresAt.Before(result.ExpiresAt) {
		result.ExpiresAt = input.Authority.ExpiresAt
	}
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrConflict
	}
	return result, nil
}

// validSTTProjectionAuthority Uses the legacy proof-bound validator outside trusted cluster;
// trusted mode requires exact broker operation, no project/JTI, matching current
// actor/tenant/credential revisions, fixed STT workload/method and expiry strictly within 30
// seconds. This predicate checks verified-context shape, not provider readiness.
// validSTTProjectionAuthority Вне trusted cluster использует прежний валидатор с proof; в
// доверенном режиме требует точную операцию broker, отсутствие проекта и JTI, текущие actor,
// tenant и ревизии credential, фиксированные STT workload и метод, срок строго в пределах 30
// секунд. Проверяет форму проверенного контекста, а не готовность провайдера.
func (repository *Repository) validSTTProjectionAuthority(principal value.Principal, authority platformrepo.CredentialProjectionAuthority) bool {
	if !repository.trustedCluster {
		return validProjectionAuthority(authority, "stt-tts-service", sttProjectionMethod)
	}
	now := time.Now().UTC()
	return authority.RPCProfile == transportprofile.TrustedCluster && validCredentialProjectionOwner(principal, platformrepo.TrustedSTTCredentialOperation) &&
		principal.ProjectRef == "" && authority.ProjectID == "" && authority.ProofJTI == "" &&
		authority.ActorID == principal.ActorID && authority.TenantID == principal.AuthorityTenant &&
		uuid.Validate(authority.ActorID) == nil && uuid.Validate(authority.TenantID) == nil &&
		authority.SourceRevision == principal.CredentialRevision && authority.SourceRevision > 0 &&
		authority.CallerCredentialRevision == principal.CredentialRevision && validRuntimeSecretSHA256(authority.SourceDigestSHA256) &&
		authority.CallerWorkloadID == "stt-tts-service" && authority.CallerFullMethod == sttProjectionMethod &&
		authority.ExpiresAt.After(now) && !authority.ExpiresAt.After(now.Add(30*time.Second))
}

// validCredentialProjectionOwner Accepts only the secret-broker caller workload with the exact
// requested permission. This narrow predicate relies on the transport to verify principal
// provenance; payload identity alone cannot satisfy that boundary.
// validCredentialProjectionOwner Допускает лишь workload secret-broker с точным требуемым
// разрешением. Узкая проверка полагается на проверенное transport-происхождение principal; одной
// личности из payload недостаточно.
func validCredentialProjectionOwner(principal value.Principal, permission string) bool {
	return principal.CallerWorkload == "secret-broker" && principal.Permission == permission
}

// validProjectionAuthority Validates the legacy empty-profile authority tuple: UUID
// actor/tenant/proof and project or permitted projectless assistant/STT, positive revisions,
// digest, exact workload/method and future expiry within five minutes. It validates already
// verified metadata rather than checking a signature itself.
// validProjectionAuthority Проверяет прежнюю authority с пустым RPCProfile: UUID субъекта,
// организации и proof, проект либо допустимое отсутствие для assistant/STT, положительные ревизии,
// хеш, точные workload и метод, будущий срок до пяти минут. Проверяет уже подтверждённые
// метаданные, сама подпись не проверяется.
func validProjectionAuthority(authority platformrepo.CredentialProjectionAuthority, workload, method string) bool {
	now := time.Now().UTC()
	return authority.RPCProfile == "" && uuid.Validate(authority.ActorID) == nil && uuid.Validate(authority.TenantID) == nil &&
		(uuid.Validate(authority.ProjectID) == nil || (method == assistantProjectionMethod || method == sttProjectionMethod) && authority.ProjectID == "") && uuid.Validate(authority.ProofJTI) == nil &&
		authority.SourceRevision > 0 && authority.CallerCredentialRevision > 0 && validRuntimeSecretSHA256(authority.SourceDigestSHA256) &&
		authority.CallerWorkloadID == workload && authority.CallerFullMethod == method &&
		authority.ExpiresAt.After(now) && !authority.ExpiresAt.After(now.Add(5*time.Minute))
}

// validRuntimeProjectionAuthority Selects the legacy runtime RPC for project-scoped authority or
// system-assistant RPC when project ID is empty, then applies the fixed runtime-controller
// authority predicate. It does not infer authority from an arbitrary screen context.
// validRuntimeProjectionAuthority Выбирает прежний runtime RPC для проектной authority либо RPC
// системного ассистента при пустом project ID, затем применяет проверку фиксированного
// runtime-controller. Полномочия из произвольного контекста экрана не выводит.
func validRuntimeProjectionAuthority(authority platformrepo.CredentialProjectionAuthority) bool {
	method := runtimeProjectionMethod
	if authority.ProjectID == "" {
		method = assistantProjectionMethod
	}
	return validProjectionAuthority(authority, "runtime-controller", method)
}

// validRuntimeProjectionInput Checks nonempty execution/session/turn/lease/revision refs, bounded
// workload instance, positive generation/attempt and lexical revision/input digests. It does not
// verify owner lineage, current lease state or fence; SQL and authority validation own those
// checks.
// validRuntimeProjectionInput Проверяет непустые ссылки выполнения, сессии, turn, lease и
// revision, ограниченный workload instance, положительные generation/attempt и форму хешей.
// Владение, состояние аренды и fence не подтверждает; их проверяют SQL и authority.
func validRuntimeProjectionInput(input platformrepo.RuntimeCredentialProjectionInput) bool {
	return input.WorkloadInstance != "" && len(input.WorkloadInstance) <= 128 && input.LeaseRef != "" &&
		input.Generation > 0 && input.Attempt > 0 && input.RuntimeRevisionRef != "" && input.SessionRef != "" &&
		input.TurnRef != "" && validRuntimeSecretSHA256(input.RuntimeRevisionDigest) && validRuntimeSecretSHA256(input.InputDigest)
}

// sameProviderBinding Compares every field of the comparable provider credential binding exactly,
// including object identity/revision/digest. Equality does not resolve current owner eligibility
// or read a secret value.
// sameProviderBinding Точно сравнивает все поля сравнимой привязки credential провайдера, включая
// идентичность объекта, ревизию и хеш. Равенство не разрешает текущую пригодность владельца и не
// читает значение секрета.
func sameProviderBinding(left, right platformrepo.ProviderCredentialBinding) bool {
	return left == right
}

// sameRuntimeSecretDescriptors Requires equal lengths, sorts copies by the descriptor key and
// compares complete binding values without changing caller slices. Nil/empty are equivalent; this
// helper does not reject duplicate keys, and revision equality is checked by value although
// revision is absent from the sort key.
// sameRuntimeSecretDescriptors Требует равные длины, сортирует копии по ключу дескриптора и
// сравнивает полные привязки, не меняя срезы вызывающего. Nil и пустой список равны; повторные
// ключи здесь не отклоняются, ревизия сравнивается в значении, хотя отсутствует в ключе
// сортировки.
func sameRuntimeSecretDescriptors(left, right []platformrepo.RuntimeSecretProjectionBinding) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]platformrepo.RuntimeSecretProjectionBinding(nil), left...)
	rightCopy := append([]platformrepo.RuntimeSecretProjectionBinding(nil), right...)
	sort.Slice(leftCopy, func(i, j int) bool {
		return projectionDescriptorKey(leftCopy[i]) < projectionDescriptorKey(leftCopy[j])
	})
	sort.Slice(rightCopy, func(i, j int) bool {
		return projectionDescriptorKey(rightCopy[i]) < projectionDescriptorKey(rightCopy[j])
	})
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

// projectionDescriptorKey Joins runtime variable/secret ref and
// namespace/object/key/UID/resource-version/content-digest fields with NUL separators for
// deterministic descriptor sorting. Revision is not part of this key; complete equality is checked
// separately and no authority is issued.
// projectionDescriptorKey Соединяет имя runtime-переменной, ссылку секрета, namespace, объект,
// ключ, UID, версию ресурса и хеш через NUL для сортировки. Ревизия в ключ не входит; полное
// равенство проверяется отдельно, полномочия не выдаются.
func projectionDescriptorKey(value platformrepo.RuntimeSecretProjectionBinding) string {
	descriptor := value.Descriptor
	return strings.Join([]string{value.Name, value.SecretRef, descriptor.Namespace, descriptor.SecretName, descriptor.SecretKey, descriptor.SecretUID, descriptor.SecretResourceVersion, descriptor.ContentSHA256}, "\x00")
}

// scanProviderCredentialProjection scans the seven shared provider credential binding fields
// followed by caller-owned typed destinations in exact SQL order. Wrapped missing rows become
// not-found and other failures unavailable. It may fill partial internal destinations on error;
// callers discard their complete result then. SQL eligibility, authority, expiry and transaction
// handling remain explicit at runtime/STT callers; no secret value is read by this helper.
// scanProviderCredentialProjection читает семь общих полей привязки credential провайдера,
// затем типизированные поля вызывающего в точном порядке SQL. Обёрнутое отсутствие строки даёт
// NotFound, другой сбой — Unavailable. При ошибке внутренние поля могут заполниться частично;
// вызывающие отбрасывают весь результат. SQL-пригодность, полномочия, сроки и транзакция остаются
// явными в runtime/STT путях; значения секретов функция не читает.
func scanProviderCredentialProjection(row pgx.Row, binding *platformrepo.ProviderCredentialBinding, tail ...any) error {
	destinations := []any{&binding.AccountRef, &binding.CredentialRevisionRef, &binding.CredentialRevision,
		&binding.SecretName, &binding.SecretUID, &binding.SecretResourceVersion, &binding.ContentSHA256}
	err := row.Scan(append(destinations, tail...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
