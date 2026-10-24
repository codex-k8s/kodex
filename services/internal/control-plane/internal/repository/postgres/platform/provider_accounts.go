package platform

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// changeProviderAccount dispatches specialized account transitions after exact
// organization/account lock and expected version, with creation and queued-work cancellation on
// their own paths. It preserves concurrency role/bounds, pending/terminal authorization
// validation, credential activation, active-runtime/warm-consumer revoke guards and durable
// cleanup intent. PENDING device polling emits no account event; successful changes return
// hydrated metadata and event intent, while the outer command owns authority,
// audit/idempotency/outbox and commit. No secret bytes or provider network operation is performed
// here.
// changeProviderAccount направляет специализированные переходы после точной блокировки account
// организации и проверки версии; создание и отмена queued work имеют отдельные пути. Сохраняет
// role и предел concurrency, pending и terminal authorization validation, активацию credential,
// guards активных runtime и warm consumers перед revoke, durable cleanup intent. PENDING poll не
// создаёт account event; изменения возвращают metadata и намерение события, внешняя команда
// владеет authority, audit, idempotency, outbox и commit. Значения секретов и provider network
// operations здесь отсутствуют.
func (repository *Repository) changeProviderAccount(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	input command.Command,
) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ProviderAccountInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if input.Kind == command.CreateProviderAccount {
		return repository.createProviderAccount(ctx, tx, current, payload)
	}
	if payload.AccountRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var accountID, state string
	var version int64
	var enabled bool
	var credentialID *string
	if err := tx.QueryRow(ctx, queryProviderAccountsLock, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "account_ref": payload.AccountRef,
	}).Scan(&accountID, &version, &state, &enabled, &credentialID); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if input.Kind == command.CancelProviderAccountQueuedWork {
		return repository.cancelProviderAccountQueuedWork(ctx, tx, current, input, payload, accountID, version)
	}
	summary := "i18n:PROVIDER_ACCOUNT_UPDATED"
	emitEvent := true
	switch input.Kind {
	case command.SetProviderAccountConcurrency:
		if payload.MaximumConcurrentExecutions < 1 || payload.MaximumConcurrentExecutions > 256 {
			return commandOutcome{}, errs.ErrInvalid
		}
		if state == "DELETING" || state == "DELETED" {
			return commandOutcome{}, errs.ErrConflict
		}
		if current.role != "OWNER" && current.role != "ADMINISTRATOR" {
			return commandOutcome{}, errs.ErrForbidden
		}
		tag, err := tx.Exec(ctx, queryProviderAccountsSetConcurrency, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "account_id": accountID,
			"expected_version": version, "maximum_concurrent_executions": payload.MaximumConcurrentExecutions,
		})
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
	case command.VerifyProviderAuthorization:
		if state != "AUTHORIZED" || !enabled || credentialID == nil {
			return commandOutcome{}, errs.ErrConflict
		}
		if err := repository.startProviderVerification(ctx, tx, current, accountID); err != nil {
			return commandOutcome{}, err
		}
		summary = "i18n:PROVIDER_ACCOUNT_VERIFICATION_REQUESTED"
		emitEvent = true
	case command.StartProviderDeviceAuth:
		if !providerAccountCanAuthorize(state) || !validPendingProviderAuthorization(payload) {
			return commandOutcome{}, errs.ErrConflict
		}
		if err := repository.insertProviderAuthorization(ctx, tx, current, accountID, payload); err != nil {
			return commandOutcome{}, err
		}
		if _, err := tx.Exec(ctx, queryProviderAccountsUpdateLifecycle, pgx.StrictNamedArgs{
			"account_id": accountID, "state": "PENDING_AUTHORIZATION", "enabled": false, "clear_credential": false,
		}); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		summary = "i18n:PROVIDER_DEVICE_AUTHORIZATION_STARTED"
	case command.AuthorizeProviderAPIKey:
		if !providerAccountCanAuthorize(state) {
			return commandOutcome{}, errs.ErrConflict
		}
		if !validTerminalProviderAuthorization(payload, true) {
			return commandOutcome{}, errs.ErrInvalid
		}
		if _, err := tx.Exec(ctx, queryProviderAccountsFailPendingAuthorizations, pgx.StrictNamedArgs{
			"account_id": accountID, "safe_failure_code": "SUPERSEDED",
		}); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err := repository.insertProviderAuthorization(ctx, tx, current, accountID, payload); err != nil {
			return commandOutcome{}, err
		}
		if err := repository.activateProviderCredential(ctx, tx, current, accountID, credentialID, payload); err != nil {
			return commandOutcome{}, err
		}
		summary = "i18n:PROVIDER_ACCOUNT_AUTHORIZED"
	case command.RefreshProviderAuthorization:
		if state != "PENDING_AUTHORIZATION" || payload.AuthorizationMethod != "DEVICE_CODE" ||
			payload.AuthorizationRef == "" || payload.MaterializerAttemptRef == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		if payload.AuthorizationState == "PENDING" {
			emitEvent = false
			break
		}
		if payload.AuthorizationState != "AUTHORIZED" && payload.AuthorizationState != "EXPIRED" && payload.AuthorizationState != "FAILED" {
			return commandOutcome{}, errs.ErrInvalid
		}
		tag, err := tx.Exec(ctx, queryProviderAccountsCompleteAuthorization, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "account_id": accountID,
			"authorization_ref": payload.AuthorizationRef, "materializer_attempt_ref": payload.MaterializerAttemptRef,
			"state": payload.AuthorizationState, "safe_failure_code": payload.SafeFailureCode,
		})
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
		if payload.AuthorizationState == "AUTHORIZED" {
			if !validTerminalProviderAuthorization(payload, true) {
				return commandOutcome{}, errs.ErrInvalid
			}
			if err := repository.activateProviderCredential(ctx, tx, current, accountID, credentialID, payload); err != nil {
				return commandOutcome{}, err
			}
			summary = "i18n:PROVIDER_ACCOUNT_AUTHORIZED"
		} else {
			if payload.Credential != nil || payload.SafeFailureCode == "" {
				return commandOutcome{}, errs.ErrInvalid
			}
			if _, err := tx.Exec(ctx, queryProviderAccountsUpdateLifecycle, pgx.StrictNamedArgs{
				"account_id": accountID, "state": "REAUTHORIZATION_REQUIRED", "enabled": false, "clear_credential": false,
			}); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			summary = "i18n:PROVIDER_AUTHORIZATION_FAILED"
		}
	case command.DeleteProviderAccount:
		if err := repository.startProviderAccountDeletion(ctx, tx, current, accountID, state); err != nil {
			return commandOutcome{}, err
		}
		summary = "i18n:PROVIDER_ACCOUNT_DELETION_REQUESTED"
	case command.RevokeProviderAccount:
		if state == "REVOKED" || state == "DELETING" || state == "DELETED" {
			return commandOutcome{}, errs.ErrConflict
		}
		var activeRuntimeLease, activeWarmConsumer bool
		if err := tx.QueryRow(ctx, queryProviderAccountsCleanupGuard, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "account_id": accountID,
		}).Scan(&activeRuntimeLease, &activeWarmConsumer); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if activeRuntimeLease || activeWarmConsumer {
			return commandOutcome{}, errs.ErrConflict
		}
		if _, err := tx.Exec(ctx, queryProviderAccountsFailPendingAuthorizations, pgx.StrictNamedArgs{
			"account_id": accountID, "safe_failure_code": "REVOKED",
		}); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryProviderAccountsUpdateLifecycle, pgx.StrictNamedArgs{
			"account_id": accountID, "state": "REVOKED", "enabled": false, "clear_credential": true,
		}); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryProviderCredentialCleanupScheduleAccount, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "account_id": accountID,
			"eligible_at": time.Now().UTC(), "maximum_attempts": providerCredentialCleanupMaxAttempts,
		}); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		summary = "i18n:PROVIDER_ACCOUNT_REVOKED"
	case command.SetProviderAccountEnabled:
		nextState, ok := providerAccountEnabledTransition(state, enabled, credentialID != nil, payload.Enabled)
		if !ok {
			return commandOutcome{}, errs.ErrConflict
		}
		if _, err := tx.Exec(ctx, queryProviderAccountsUpdateLifecycle, pgx.StrictNamedArgs{
			"account_id": accountID, "state": nextState, "enabled": payload.Enabled, "clear_credential": false,
		}); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
	item, err := repository.providerAccountByRef(ctx, tx, current, payload.AccountRef)
	if err != nil {
		return commandOutcome{}, err
	}
	outcome := commandOutcome{
		result: command.Result{ProviderAccount: &item}, resourceKind: "PROVIDER_ACCOUNT",
		resourceRef: item.Ref, summary: summary, platformAggregateVersion: item.Version, platformState: item.State,
	}
	if emitEvent {
		outcome.platformEvent = "PROVIDER_ACCOUNT_CHANGED"
	}
	return outcome, nil
}

// ProviderMaterializationReferenced checks owner-side materialization interest after an ambiguous
// command using exactly one attempt locator or validated credential descriptor, exact account
// access and a ReadCommitted account lock. Missing account returns false/nil, but access is hidden
// as NotFound and read failures remain errors, never cleanup permission. The query also keeps any
// exact RESERVED authorization alive; this method returns metadata only and defers ignored
// request-context rollback without commit.
// ProviderMaterializationReferenced проверяет owner-side ссылку после неоднозначной команды по
// одному attempt locator либо проверенному credential descriptor, точному account доступу и
// ReadCommitted блокировке. Отсутствующий account даёт false/nil, отказ доступа скрывается как
// NotFound, ошибки чтения не становятся разрешением очистки. SQL также сохраняет интерес RESERVED
// authorization; метод возвращает только метаданные и использует отложенный ignored rollback с
// request context без commit.
func (repository *Repository) ProviderMaterializationReferenced(
	ctx context.Context,
	principal value.Principal,
	accountRef, authorizationRef, materializerAttemptRef string,
	credential *entity.ProviderCredentialDescriptor,
) (bool, error) {
	if strings.TrimSpace(accountRef) == "" || !strings.HasPrefix(authorizationRef, "pauth_") ||
		(materializerAttemptRef == "") == (credential == nil) {
		return false, errs.ErrInvalid
	}
	secretName, secretUID, secretResourceVersion, contentSHA256 := "", "", "", ""
	if credential != nil {
		if !validProviderCredential(*credential) {
			return false, errs.ErrInvalid
		}
		secretName = credential.SecretName
		secretUID = credential.SecretUID
		secretResourceVersion = credential.SecretResourceVersion
		contentSHA256 = credential.ContentSHA256
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return false, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROVIDER_ACCOUNT", ResourceRef: accountRef}
	if err := repository.requireAccess(ctx, tx, current, "provider.account.view", target); err != nil {
		return false, errs.ErrNotFound
	}
	var accountID string
	if err := tx.QueryRow(ctx, queryProviderAccountsMaterializationGuard, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "account_ref": accountRef,
	}).Scan(&accountID); errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, errs.ErrUnavailable
	}
	var referenced bool
	err = tx.QueryRow(ctx, queryProviderAccountsMaterializationReferenced, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"account_ref":     accountRef, "authorization_ref": authorizationRef,
		"materializer_attempt_ref": materializerAttemptRef,
		"secret_name":              secretName, "secret_uid": secretUID,
		"secret_resource_version": secretResourceVersion, "content_sha256": contentSHA256,
	}).Scan(&referenced)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	return referenced, nil
}

// createProviderAccount validates trimmed name and provider key, generates a server account ref
// and inserts only against an enabled definition in the caller transaction. No definition is
// NotFound; write failures use the domain mapper. Hydrated metadata and creation event intent are
// returned without committing or authorizing the outer command.
// createProviderAccount проверяет нормализованное имя и provider key, создаёт серверную ссылку
// account и вставляет только при включённом definition в переданной транзакции. Отсутствие
// definition — NotFound, ошибки записи отображаются в доменные. Возвращает metadata и намерение
// события без commit и авторизации внешней команды.
func (repository *Repository) createProviderAccount(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	payload command.ProviderAccountInput,
) (commandOutcome, error) {
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" || len(payload.Name) > 160 || !validStableKey(payload.DefinitionKey) {
		return commandOutcome{}, errs.ErrInvalid
	}
	ref, err := newRef("pacc")
	if err != nil {
		return commandOutcome{}, err
	}
	var created string
	if err := tx.QueryRow(ctx, queryProviderAccountsCreate, pgx.StrictNamedArgs{
		"account_ref": ref, "organization_id": current.organizationID, "definition_key": payload.DefinitionKey,
		"name": payload.Name, "created_by": current.actorID,
	}).Scan(&created); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	item, err := repository.providerAccountByRef(ctx, tx, current, created)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{
		result: command.Result{ProviderAccount: &item}, resourceKind: "PROVIDER_ACCOUNT", resourceRef: item.Ref,
		summary: "i18n:PROVIDER_ACCOUNT_CREATED", platformEvent: "PROVIDER_ACCOUNT_CHANGED",
		platformAggregateVersion: item.Version, platformState: item.State,
	}, nil
}

// insertProviderAuthorization inserts authorization/materializer metadata or applies a RESERVED
// record only when its exact organization/account/actor/method/version/deadline predicates match.
// Write errors use mapWriteError and any affected count other than one is Conflict; authorization
// input validation and account locking remain with the caller.
// insertProviderAuthorization вставляет authorization и materializer metadata либо применяет
// RESERVED запись только при совпадении организации, account, субъекта, метода, версии и срока.
// Ошибки записи проходят mapWriteError, число строк не один даёт Conflict; валидация входа и
// блокировка account остаются у вызывающего.
func (repository *Repository) insertProviderAuthorization(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	accountID string,
	payload command.ProviderAccountInput,
) error {
	tag, err := tx.Exec(ctx, queryProviderAccountsInsertAuthorization, pgx.StrictNamedArgs{
		"authorization_ref": payload.AuthorizationRef, "organization_id": current.organizationID,
		"account_id": accountID, "method": payload.AuthorizationMethod, "state": payload.AuthorizationState,
		"materializer_attempt_ref": payload.MaterializerAttemptRef, "verification_uri": payload.VerificationURI,
		"materializer_attempt_uid":              payload.MaterializerAttemptUID,
		"materializer_attempt_resource_version": payload.MaterializerAttemptResourceVersion,
		"user_code":                             payload.UserCode, "expires_at": payload.AuthorizationExpiresAt,
		"safe_failure_code": payload.SafeFailureCode, "created_by": current.actorID,
	})
	if err != nil {
		return mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

// activateProviderCredential requires a valid exact Kubernetes descriptor and bounded nonempty
// masked account identity, inserts the next credential revision and activates it under the caller
// account lock. A replaced credential gets durable cleanup after the retention grace; failures
// require rollback of all preceding changes. It stores descriptor metadata, never secret bytes,
// and performs no external apply/delete or commit.
// activateProviderCredential требует допустимый Kubernetes descriptor и ограниченную непустую
// masked identity, вставляет следующую credential revision и активирует её под блокировкой
// account. Прежняя credential получает durable cleanup после retention grace; ошибка требует
// rollback всех изменений. Хранит только дескриптор, не значения; внешних apply, delete и commit
// нет.
func (repository *Repository) activateProviderCredential(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	accountID string,
	previousCredentialID *string,
	payload command.ProviderAccountInput,
) error {
	if payload.Credential == nil || !validProviderCredential(*payload.Credential) ||
		strings.TrimSpace(payload.ExternalAccountMasked) == "" || len(payload.ExternalAccountMasked) > 320 {
		return errs.ErrInvalid
	}
	credentialRef, err := newRef("pcred")
	if err != nil {
		return err
	}
	var credentialID string
	if err := tx.QueryRow(ctx, queryProviderAccountsInsertCredentialRevision, pgx.StrictNamedArgs{
		"credential_ref": credentialRef, "organization_id": current.organizationID, "account_id": accountID,
		"secret_name": payload.Credential.SecretName, "secret_uid": payload.Credential.SecretUID,
		"secret_resource_version": payload.Credential.SecretResourceVersion,
		"content_sha256":          payload.Credential.ContentSHA256,
	}).Scan(&credentialID); err != nil {
		return mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryProviderAccountsActivateCredential, pgx.StrictNamedArgs{
		"credential_id": credentialID, "account_id": accountID, "external_account_masked": payload.ExternalAccountMasked,
	}); err != nil {
		return errs.ErrUnavailable
	}
	if previousCredentialID != nil && *previousCredentialID != credentialID {
		return repository.scheduleProviderCredentialCleanup(ctx, tx, current.organizationID, accountID,
			*previousCredentialID, time.Now().UTC().Add(providerCredentialCleanupRetention))
	}
	return nil
}

// providerAccountByRef reads exact organization/account metadata, computes existing management
// hints from role and hydrates lifecycle details in the same transaction. Errors return an empty
// account; unconditional action inputs here rely on the surrounding authorized path and never
// replace command permission checks.
// providerAccountByRef читает metadata точного account организации, вычисляет прежние подсказки
// действий по роли и дополняет lifecycle в той же транзакции. При ошибке account пуст; безусловные
// action inputs опираются на авторизованный внешний путь и не заменяют проверки команды.
func (repository *Repository) providerAccountByRef(ctx context.Context, tx pgx.Tx, current scope, ref string) (entity.ProviderAccount, error) {
	item, err := scanProviderAccount(tx.QueryRow(ctx, queryMVPGetProviderAccount, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "account_ref": ref,
	}))
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	item.NextActions = providerAccountActions(item, true, true, true, current.role == "OWNER" || current.role == "ADMINISTRATOR")
	items := []entity.ProviderAccount{item}
	if err := repository.hydrateProviderAccountLifecycle(ctx, tx, current, items); err != nil {
		return entity.ProviderAccount{}, err
	}
	return items[0], nil
}

// validPendingProviderAuthorization checks the existing pauth_ prefix, DEVICE_CODE/PENDING values
// and presence of materializer ref, verification URI, user code and expiry pointer. It does not
// validate URI trust, future expiry or materializer identity/readback; those are separate boundary
// checks.
// validPendingProviderAuthorization проверяет прежний pauth_ префикс, DEVICE_CODE и PENDING,
// наличие materializer ref, verification URI, user code и expiry pointer. Доверие URI, будущий
// срок и materializer identity/readback здесь не проверяются.
func validPendingProviderAuthorization(payload command.ProviderAccountInput) bool {
	return strings.HasPrefix(payload.AuthorizationRef, "pauth_") && payload.AuthorizationMethod == "DEVICE_CODE" &&
		payload.AuthorizationState == "PENDING" && payload.MaterializerAttemptRef != "" &&
		payload.VerificationURI != "" && payload.UserCode != "" && payload.AuthorizationExpiresAt != nil
}

// providerAccountCanAuthorize allows authorization only from PENDING_AUTHORIZATION or
// REAUTHORIZATION_REQUIRED. It is a state predicate; actor permission, expected version and
// credential/materializer validation remain external.
// providerAccountCanAuthorize разрешает начало авторизации только из PENDING_AUTHORIZATION и
// REAUTHORIZATION_REQUIRED. Это условие состояния; полномочия субъекта, ожидаемая версия и
// credential/materializer validation проверяются отдельно.
func providerAccountCanAuthorize(state string) bool {
	return state == "PENDING_AUTHORIZATION" || state == "REAUTHORIZATION_REQUIRED"
}

// providerAccountEnabledTransition permits only DISABLED/false to AUTHORIZED/true or the inverse
// with a current credential. Missing credential, redundant requests and other states return an
// empty state/false without mutation or authorization.
// providerAccountEnabledTransition допускает только DISABLED/false в AUTHORIZED/true либо обратный
// переход при текущей credential. Без credential, при повторном запросе и ином состоянии
// возвращает пустое состояние и false без изменения и авторизации.
func providerAccountEnabledTransition(state string, enabled, hasCredential, requested bool) (string, bool) {
	if !hasCredential {
		return "", false
	}
	if requested && state == "DISABLED" && !enabled {
		return "AUTHORIZED", true
	}
	if !requested && state == "AUTHORIZED" && enabled {
		return "DISABLED", true
	}
	return "", false
}

// validTerminalProviderAuthorization checks the existing authorization ref prefix,
// API_KEY/DEVICE_CODE method and AUTHORIZED state, requiring a credential pointer only when
// requested. It does not inspect that descriptor, expiry, failure code or caller authority; the
// transition validates these separately.
// validTerminalProviderAuthorization проверяет прежний префикс ссылки, метод API_KEY либо
// DEVICE_CODE и AUTHORIZED, требуя credential pointer только по флагу. Дескриптор, срок, failure
// code и полномочия не проверяет; это делает переход отдельно.
func validTerminalProviderAuthorization(payload command.ProviderAccountInput, credentialRequired bool) bool {
	return strings.HasPrefix(payload.AuthorizationRef, "pauth_") &&
		(payload.AuthorizationMethod == "API_KEY" || payload.AuthorizationMethod == "DEVICE_CODE") &&
		payload.AuthorizationState == "AUTHORIZED" && (!credentialRequired || payload.Credential != nil)
}

// validProviderCredential checks a DNS-label Secret name, UUID-parsable UID, nonempty resource
// version at most 128 bytes and exact lowercase SHA-256 text. It does not contact Kubernetes,
// compare content or establish namespace, actor or provenance authority.
// validProviderCredential проверяет DNS-label имя Secret, UID, разбираемый как UUID, непустую
// resource version до 128 байт и точный строчный SHA-256. Kubernetes не вызывает, содержимое не
// сравнивает, полномочия namespace, субъекта и происхождения не устанавливает.
func validProviderCredential(value entity.ProviderCredentialDescriptor) bool {
	_, uuidErr := uuid.Parse(value.SecretUID)
	return validDNSLabel(value.SecretName) && uuidErr == nil && value.SecretResourceVersion != "" &&
		len(value.SecretResourceVersion) <= 128 && validProviderSHA256(value.ContentSHA256)
}

// validProviderSHA256 delegates the exact unprefixed 64-byte lowercase hexadecimal grammar to the
// existing shared SHA-256 validator, preserving zeros and rejecting other lengths, case, controls
// and Unicode. It validates representation only without hashing, I/O or content/provenance
// verification.
// validProviderSHA256 передаёт существующему общему валидатору точную грамматику 64 строчных
// hex-байт без префикса, сохраняя нулевой хеш и отклоняя длину, регистр, control и Unicode.
// Проверяет представление без хеширования, I/O и проверки содержимого и происхождения.
func validProviderSHA256(value string) bool {
	return exactSHA256(value)
}
