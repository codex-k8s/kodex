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
	summary := "i18n:PROVIDER_ACCOUNT_UPDATED"
	emitEvent := true
	switch input.Kind {
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
	case command.RevokeProviderAccount:
		if state == "REVOKED" {
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

// ProviderMaterializationReferenced проверяет owner-side exact reference после
// неоднозначного результата command transaction. Ошибка чтения никогда не
// интерпретируется как разрешение удалить Kubernetes Secret.
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

func (repository *Repository) insertProviderAuthorization(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	accountID string,
	payload command.ProviderAccountInput,
) error {
	_, err := tx.Exec(ctx, queryProviderAccountsInsertAuthorization, pgx.StrictNamedArgs{
		"authorization_ref": payload.AuthorizationRef, "organization_id": current.organizationID,
		"account_id": accountID, "method": payload.AuthorizationMethod, "state": payload.AuthorizationState,
		"materializer_attempt_ref": payload.MaterializerAttemptRef, "verification_uri": payload.VerificationURI,
		"user_code": payload.UserCode, "expires_at": payload.AuthorizationExpiresAt,
		"safe_failure_code": payload.SafeFailureCode, "created_by": current.actorID,
	})
	if err != nil {
		return mapWriteError(err)
	}
	return nil
}

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

func (repository *Repository) providerAccountByRef(ctx context.Context, tx pgx.Tx, current scope, ref string) (entity.ProviderAccount, error) {
	item, err := scanProviderAccount(tx.QueryRow(ctx, queryMVPGetProviderAccount, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "account_ref": ref,
	}))
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	item.NextActions = providerAccountActions(item, true, true, true)
	return item, nil
}

func validPendingProviderAuthorization(payload command.ProviderAccountInput) bool {
	return strings.HasPrefix(payload.AuthorizationRef, "pauth_") && payload.AuthorizationMethod == "DEVICE_CODE" &&
		payload.AuthorizationState == "PENDING" && payload.MaterializerAttemptRef != "" &&
		payload.VerificationURI != "" && payload.UserCode != "" && payload.AuthorizationExpiresAt != nil
}

func providerAccountCanAuthorize(state string) bool {
	return state == "PENDING_AUTHORIZATION" || state == "REAUTHORIZATION_REQUIRED"
}

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

func validTerminalProviderAuthorization(payload command.ProviderAccountInput, credentialRequired bool) bool {
	return strings.HasPrefix(payload.AuthorizationRef, "pauth_") &&
		(payload.AuthorizationMethod == "API_KEY" || payload.AuthorizationMethod == "DEVICE_CODE") &&
		payload.AuthorizationState == "AUTHORIZED" && (!credentialRequired || payload.Credential != nil)
}

func validProviderCredential(value entity.ProviderCredentialDescriptor) bool {
	_, uuidErr := uuid.Parse(value.SecretUID)
	return validDNSLabel(value.SecretName) && uuidErr == nil && value.SecretResourceVersion != "" &&
		len(value.SecretResourceVersion) <= 128 && validProviderSHA256(value.ContentSHA256)
}

func validProviderSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
