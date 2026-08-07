package gateway

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	managementrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/management"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/management/*.sql
var managementQueries embed.FS

var _ managementrepo.Repository = (*Repository)(nil)

type managementReceipt struct {
	Kind, ID, Digest, RequestDigest string
	Version                         uint64
}

func managementSQL(name string) string {
	raw, err := managementQueries.ReadFile("sql/management/" + name + ".sql")
	if err != nil {
		panic("missing embedded management query: " + name)
	}
	return string(raw)
}

func (repository *Repository) managementTransact(
	ctx context.Context,
	scope domainrepo.Scope,
	callback func(*transaction) error,
) error {
	return repository.Transact(ctx, scope, func(value domainrepo.Transaction) error {
		current, ok := value.(*transaction)
		if !ok {
			return errors.New("management transaction type is invalid")
		}
		return callback(current)
	})
}

func lockManagementReceipt(ctx context.Context, tx *transaction, operation, keyHash, requestHash string) (managementReceipt, bool, error) {
	if _, err := tx.tx.Exec(ctx, managementSQL("receipt__lock"), pgx.StrictNamedArgs{
		"operation": operation, "key_sha256": keyHash,
	}); err != nil {
		return managementReceipt{}, false, err
	}
	var value managementReceipt
	err := tx.tx.QueryRow(ctx, managementSQL("receipt__get"), pgx.StrictNamedArgs{
		"tenant_id": tx.tenantID, "project_id": tx.projectID,
		"operation": operation, "key_sha256": keyHash,
	}).Scan(&value.Kind, &value.ID, &value.Version, &value.Digest, &value.RequestDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return managementReceipt{}, false, nil
	}
	if err != nil {
		return managementReceipt{}, false, err
	}
	if value.RequestDigest != requestHash {
		return managementReceipt{}, false, errs.ErrConflict
	}
	return value, true, nil
}

func insertManagementReceipt(ctx context.Context, tx *transaction, operation, keyHash, requestHash, kind, id string, version uint64, digest string, at time.Time) error {
	_, err := tx.tx.Exec(ctx, managementSQL("receipt__insert"), pgx.StrictNamedArgs{
		"tenant_id": tx.tenantID, "project_id": tx.projectID,
		"operation": operation, "key_sha256": keyHash, "request_sha256": requestHash,
		"resource_kind": kind, "resource_id": id, "result_version": version,
		"result_sha256": digest, "created_at": at,
	})
	return err
}

func managementPayload(value any) ([]byte, string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", errors.New("marshal management resource")
	}
	digest := sha256.Sum256(raw)
	return raw, hex.EncodeToString(digest[:]), nil
}

func managementEffectPayload(resourceID string, version, generation uint64) []byte {
	raw, _ := json.Marshal(struct {
		ResourceID string `json:"resource_id"`
		Version    uint64 `json:"version"`
		Generation uint64 `json:"generation,omitempty"`
	}{resourceID, version, generation})
	return raw
}

func insertManagementEffect(ctx context.Context, tx *transaction, kind, resourceKind, resourceID string, version, generation uint64, intentDigest string, at time.Time) error {
	_, err := tx.tx.Exec(ctx, managementSQL("effect__insert"), pgx.StrictNamedArgs{
		"effect_id": uuid.NewString(), "tenant_id": tx.tenantID, "project_id": tx.projectID,
		"actor_id":    tx.actorID,
		"effect_kind": kind, "resource_kind": resourceKind, "resource_id": resourceID,
		"resource_version": version, "resource_generation": generation,
		"intent_sha256": intentDigest, "available_at": at,
		"payload":    managementEffectPayload(resourceID, version, generation),
		"created_at": at, "updated_at": at,
	})
	return err
}

func (repository *Repository) StartAuthorization(ctx context.Context, command managementrepo.StartAuthorizationCommand) (entity.ProviderAuthorization, bool, error) {
	var result entity.ProviderAuthorization
	var replay bool
	err := repository.managementTransact(ctx, command.Scope, func(tx *transaction) error {
		stored, found, err := lockManagementReceipt(ctx, tx, "provider_authorization.start", command.IdempotencyHash, command.RequestHash)
		if err != nil {
			return err
		}
		if found {
			result, err = getAuthorizationTx(ctx, tx, stored.ID)
			replay = true
			return err
		}
		connectionRaw, _, err := managementPayload(command.Connection)
		if err != nil {
			return err
		}
		if _, err = tx.tx.Exec(ctx, managementSQL("connection__insert"), pgx.StrictNamedArgs{
			"connection_id": command.Connection.ID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
			"stable_key": command.Connection.StableKey, "provider_id": command.Connection.ProviderID,
			"display_name": command.Connection.DisplayName, "version": command.Connection.Version,
			"generation": command.Connection.Generation, "revoke_generation": command.Connection.RevokeGeneration,
			"status": command.Connection.Status, "active_credential_generation": command.Connection.ActiveCredential,
			"masked_label": command.Connection.MaskedLabel, "masked_account": command.Connection.MaskedAccount,
			"capability_sha256":  command.Connection.CapabilityDigest,
			"observation_sha256": command.Connection.ObservationDigest, "observed_at": command.Connection.ObservedAt,
			"control_plane_resource_id": command.Connection.ControlPlaneID,
			"control_plane_version":     command.Connection.ControlPlaneVersion,
			"control_plane_sha256":      command.Connection.ControlPlaneDigest,
			"payload":                   connectionRaw, "created_at": command.Connection.CreatedAt, "updated_at": command.Connection.UpdatedAt,
		}); err != nil {
			return err
		}
		authorizationRaw, authorizationDigest, err := managementPayload(command.Authorization)
		if err != nil {
			return err
		}
		if _, err = tx.tx.Exec(ctx, managementSQL("authorization__insert"), pgx.StrictNamedArgs{
			"authorization_id": command.Authorization.ID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
			"connection_id": command.Authorization.ConnectionID, "provider_id": command.Authorization.ProviderID,
			"attempt": command.Authorization.Attempt, "version": command.Authorization.Version,
			"generation": command.Authorization.Generation, "state": command.Authorization.State,
			"intent_sha256": command.Authorization.IntentDigest, "expires_at": command.Authorization.ExpiresAt,
			"failure_category": command.Authorization.FailureCategory, "payload": authorizationRaw,
			"created_at": command.Authorization.CreatedAt, "updated_at": command.Authorization.UpdatedAt,
		}); err != nil {
			return err
		}
		if err = insertManagementEffect(ctx, tx, "PROVIDER_AUTHORIZE", "provider_authorization", command.Authorization.ID,
			command.Authorization.Version, command.Authorization.Generation, command.Authorization.IntentDigest, command.Authorization.CreatedAt); err != nil {
			return err
		}
		if err = insertManagementReceipt(ctx, tx, "provider_authorization.start", command.IdempotencyHash, command.RequestHash,
			"provider_authorization", command.Authorization.ID, command.Authorization.Version, authorizationDigest, command.Authorization.CreatedAt); err != nil {
			return err
		}
		if err = tx.appendAudit(ctx, command.Audit); err != nil {
			return err
		}
		result = command.Authorization
		return nil
	})
	return result, replay, err
}

func getAuthorizationTx(ctx context.Context, tx *transaction, id string) (entity.ProviderAuthorization, error) {
	var raw, encrypted []byte
	if err := tx.tx.QueryRow(ctx, managementSQL("authorization__get"), pgx.StrictNamedArgs{
		"authorization_id": id, "tenant_id": tx.tenantID, "project_id": tx.projectID,
	}).Scan(&raw, &encrypted); err != nil {
		return entity.ProviderAuthorization{}, err
	}
	var result entity.ProviderAuthorization
	if json.Unmarshal(raw, &result) != nil {
		return entity.ProviderAuthorization{}, errors.New("stored provider authorization is invalid")
	}
	if len(encrypted) > 0 {
		// Encrypted device output is deliberately returned only to the domain
		// service through a separate decrypting repository method in a later read.
		result.EncryptedDeviceResult = encrypted
	}
	return result, nil
}

func (repository *Repository) GetAuthorization(ctx context.Context, scope domainrepo.Scope, id string) (entity.ProviderAuthorization, error) {
	var result entity.ProviderAuthorization
	err := repository.read(ctx, scope, func(rawTx pgx.Tx) error {
		value, err := getAuthorizationTx(ctx, &transaction{tx: rawTx, tenantID: scope.TenantID, projectID: scope.ProjectID}, id)
		result = value
		return err
	})
	return result, err
}

func (repository *Repository) GetLatestAuthorization(ctx context.Context, scope domainrepo.Scope, connectionID string) (entity.ProviderAuthorization, error) {
	var result entity.ProviderAuthorization
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		var raw, encrypted []byte
		if err := tx.QueryRow(ctx, managementSQL("authorization__latest"), pgx.StrictNamedArgs{
			"connection_id": connectionID, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
		}).Scan(&raw, &encrypted); err != nil {
			return err
		}
		if json.Unmarshal(raw, &result) != nil {
			return errors.New("stored provider authorization is invalid")
		}
		result.EncryptedDeviceResult = encrypted
		return nil
	})
	return result, err
}

func getManagedConnectionTx(ctx context.Context, tx pgx.Tx, scope domainrepo.Scope, id string) (entity.ManagedProviderConnection, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, managementSQL("connection__get"), pgx.StrictNamedArgs{
		"connection_id": id, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
	}).Scan(&raw); err != nil {
		return entity.ManagedProviderConnection{}, err
	}
	var value entity.ManagedProviderConnection
	if json.Unmarshal(raw, &value) != nil {
		return entity.ManagedProviderConnection{}, errors.New("stored provider connection is invalid")
	}
	return value, nil
}

func (repository *Repository) GetManagedConnection(ctx context.Context, scope domainrepo.Scope, id string) (entity.ManagedProviderConnection, error) {
	var value entity.ManagedProviderConnection
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		var err error
		value, err = getManagedConnectionTx(ctx, tx, scope, id)
		return err
	})
	return value, err
}

func pageLimit(value int) int {
	if value < 1 || value > 100 {
		return 51
	}
	return value + 1
}

func nextPage[T any](values []T, requested int, id func(T) string) ([]T, string) {
	if requested < 1 || len(values) <= requested {
		return values, ""
	}
	next := id(values[requested-1])
	return values[:requested], next
}

func (repository *Repository) ListConnections(ctx context.Context, scope domainrepo.Scope, states []string, limit int, after string) ([]entity.ManagedProviderConnection, string, error) {
	values := make([]entity.ManagedProviderConnection, 0)
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, managementSQL("connection__list"), pgx.StrictNamedArgs{
			"tenant_id": scope.TenantID, "project_id": scope.ProjectID, "states": states,
			"after_id": after, "page_limit": pageLimit(limit),
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			var value entity.ManagedProviderConnection
			if rows.Scan(&raw) != nil || json.Unmarshal(raw, &value) != nil {
				return errors.New("stored provider connection list is invalid")
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	values, next := nextPage(values, limit, func(value entity.ManagedProviderConnection) string { return value.ID })
	return values, next, err
}

func (repository *Repository) RestartAuthorization(ctx context.Context, command managementrepo.RestartAuthorizationCommand) (entity.ProviderAuthorization, bool, error) {
	var result entity.ProviderAuthorization
	var replay bool
	err := repository.managementTransact(ctx, command.Scope, func(tx *transaction) error {
		stored, found, err := lockManagementReceipt(ctx, tx, "provider_authorization.restart", command.IdempotencyHash, command.RequestHash)
		if err != nil {
			return err
		}
		if found {
			result, err = getAuthorizationTx(ctx, tx, stored.ID)
			replay = true
			return err
		}
		var previousRaw, connectionRaw []byte
		var databaseNow time.Time
		if err = tx.tx.QueryRow(ctx, managementSQL("authorization__restart_lock"), pgx.StrictNamedArgs{
			"authorization_id": command.PreviousID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
		}).Scan(&previousRaw, &connectionRaw, &databaseNow); err != nil {
			return err
		}
		var previous entity.ProviderAuthorization
		var connection entity.ManagedProviderConnection
		if json.Unmarshal(previousRaw, &previous) != nil || json.Unmarshal(connectionRaw, &connection) != nil {
			return errors.New("stored provider reauthorization state is invalid")
		}
		if previous.Version != command.ExpectedVersion || previous.State == "CANCELLED" || connection.Status == "REVOKED" {
			return errs.ErrConflict
		}
		previous.State, previous.Version, previous.UpdatedAt = "CANCELLED", previous.Version+1, databaseNow
		nextStatus := "PENDING"
		if connection.ActiveCredential > 0 {
			nextStatus = "VALID"
		}
		connection.Version, connection.Generation, connection.Status, connection.UpdatedAt = connection.Version+1, connection.Generation+1, nextStatus, databaseNow
		command.Authorization.ConnectionID, command.Authorization.ProviderID = connection.ID, connection.ProviderID
		command.Authorization.Attempt, command.Authorization.Generation = previous.Attempt+1, connection.Generation
		command.Authorization.CreatedAt, command.Authorization.UpdatedAt = databaseNow, databaseNow
		previousPayload, _, _ := managementPayload(previous)
		connectionPayload, _, _ := managementPayload(connection)
		authorizationPayload, authorizationDigest, _ := managementPayload(command.Authorization)
		var inserted string
		if err = tx.tx.QueryRow(ctx, managementSQL("authorization__replace"), pgx.StrictNamedArgs{
			"previous_id": previous.ID, "previous_version": command.ExpectedVersion, "previous_payload": previousPayload,
			"connection_version": connection.Version, "connection_generation": connection.Generation, "connection_status": nextStatus, "connection_payload": connectionPayload,
			"authorization_id": command.Authorization.ID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
			"provider_id": command.Authorization.ProviderID, "attempt": command.Authorization.Attempt,
			"version": command.Authorization.Version, "generation": command.Authorization.Generation,
			"state": command.Authorization.State, "intent_sha256": command.Authorization.IntentDigest,
			"expires_at": command.Authorization.ExpiresAt, "failure_category": command.Authorization.FailureCategory,
			"payload": authorizationPayload, "created_at": databaseNow, "updated_at": databaseNow,
		}).Scan(&inserted); err != nil {
			return err
		}
		if err = insertManagementEffect(ctx, tx, "PROVIDER_AUTHORIZE", "provider_authorization", inserted,
			command.Authorization.Version, command.Authorization.Generation, command.Authorization.IntentDigest, databaseNow); err != nil {
			return err
		}
		if err = insertManagementReceipt(ctx, tx, "provider_authorization.restart", command.IdempotencyHash, command.RequestHash,
			"provider_authorization", inserted, command.Authorization.Version, authorizationDigest, databaseNow); err != nil {
			return err
		}
		command.Audit.ResourceID, command.Audit.OccurredAt = inserted, databaseNow
		if err = tx.appendAudit(ctx, command.Audit); err != nil {
			return err
		}
		result = command.Authorization
		return nil
	})
	return result, replay, err
}

func (repository *Repository) CancelAuthorization(ctx context.Context, command managementrepo.CancelAuthorizationCommand) (entity.ProviderAuthorization, bool, error) {
	var result entity.ProviderAuthorization
	var replay bool
	err := repository.managementTransact(ctx, command.Scope, func(tx *transaction) error {
		stored, found, err := lockManagementReceipt(ctx, tx, "provider_authorization.cancel", command.IdempotencyHash, command.RequestHash)
		if err != nil {
			return err
		}
		if found {
			result, err = getAuthorizationTx(ctx, tx, stored.ID)
			replay = true
			return err
		}
		var authRaw, connectionRaw []byte
		var databaseNow time.Time
		if err = tx.tx.QueryRow(ctx, managementSQL("authorization__cancel_lock"), pgx.StrictNamedArgs{
			"authorization_id": command.AuthorizationID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
		}).Scan(&authRaw, &connectionRaw, &databaseNow); err != nil {
			return err
		}
		var authorization entity.ProviderAuthorization
		var connection entity.ManagedProviderConnection
		if json.Unmarshal(authRaw, &authorization) != nil || json.Unmarshal(connectionRaw, &connection) != nil {
			return errors.New("stored provider cancellation state is invalid")
		}
		if authorization.Version != command.ExpectedVersion || authorization.State != "PENDING" && authorization.State != "CODE_ISSUED" {
			return errs.ErrConflict
		}
		authorization.State, authorization.Version, authorization.UpdatedAt = "CANCELLED", authorization.Version+1, databaseNow
		connection.Version, connection.UpdatedAt = connection.Version+1, databaseNow
		if connection.ActiveCredential == 0 {
			connection.Status = "INVALID"
		}
		authorizationPayload, digest, _ := managementPayload(authorization)
		connectionPayload, _, _ := managementPayload(connection)
		_, err = tx.tx.Exec(ctx, managementSQL("authorization__cancel"), pgx.StrictNamedArgs{
			"authorization_id": authorization.ID, "expected_version": command.ExpectedVersion,
			"authorization_payload": authorizationPayload, "connection_version": connection.Version,
			"connection_status": connection.Status, "connection_payload": connectionPayload, "updated_at": databaseNow,
		})
		if err != nil {
			return err
		}
		if err = insertManagementReceipt(ctx, tx, "provider_authorization.cancel", command.IdempotencyHash, command.RequestHash,
			"provider_authorization", authorization.ID, authorization.Version, digest, databaseNow); err != nil {
			return err
		}
		command.Audit.OccurredAt = databaseNow
		if err = tx.appendAudit(ctx, command.Audit); err != nil {
			return err
		}
		result = authorization
		return nil
	})
	return result, replay, err
}

func (repository *Repository) RevokeConnection(ctx context.Context, command managementrepo.RevokeConnectionCommand) (entity.ManagedProviderConnection, bool, error) {
	var result entity.ManagedProviderConnection
	var replay bool
	err := repository.managementTransact(ctx, command.Scope, func(tx *transaction) error {
		stored, found, err := lockManagementReceipt(ctx, tx, "provider_connection.revoke", command.IdempotencyHash, command.RequestHash)
		if err != nil {
			return err
		}
		if found {
			result, err = getManagedConnectionTx(ctx, tx.tx, command.Scope, stored.ID)
			replay = true
			return err
		}
		var raw []byte
		var databaseNow time.Time
		if err = tx.tx.QueryRow(ctx, managementSQL("connection__revoke_lock"), pgx.StrictNamedArgs{
			"connection_id": command.ConnectionID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
		}).Scan(&raw, &databaseNow); err != nil {
			return err
		}
		if json.Unmarshal(raw, &result) != nil {
			return errors.New("stored provider revocation state is invalid")
		}
		if result.Version != command.ExpectedVersion || result.Generation != command.ExpectedGeneration || result.Status == "REVOKED" {
			return errs.ErrConflict
		}
		result.Version, result.RevokeGeneration, result.Status, result.UpdatedAt = result.Version+1, result.RevokeGeneration+1, "REVOKED", databaseNow
		payload, digest, _ := managementPayload(result)
		var changed string
		if err = tx.tx.QueryRow(ctx, managementSQL("connection__revoke"), pgx.StrictNamedArgs{
			"connection_id": result.ID, "expected_version": command.ExpectedVersion,
			"expected_generation": command.ExpectedGeneration, "version": result.Version,
			"revoke_generation": result.RevokeGeneration, "payload": payload, "updated_at": databaseNow,
		}).Scan(&changed); err != nil {
			return err
		}
		if err = insertManagementEffect(ctx, tx, "PROVIDER_REVOKE", "managed_provider_connection", changed,
			result.Version, result.Generation, command.RequestHash, databaseNow); err != nil {
			return err
		}
		if err = insertManagementReceipt(ctx, tx, "provider_connection.revoke", command.IdempotencyHash, command.RequestHash,
			"managed_provider_connection", changed, result.Version, digest, databaseNow); err != nil {
			return err
		}
		command.Audit.OccurredAt = databaseNow
		if err = tx.appendAudit(ctx, command.Audit); err != nil {
			return err
		}
		return nil
	})
	return result, replay, err
}

func (repository *Repository) NextManagementScope(ctx context.Context) (domainrepo.Scope, bool, error) {
	var scope domainrepo.Scope
	err := repository.pool.QueryRow(ctx, managementSQL("effect__next_scope"), pgx.StrictNamedArgs{}).Scan(&scope.TenantID, &scope.ProjectID, &scope.ActorID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (scope.TenantID == "" || scope.ProjectID == "") {
		return domainrepo.Scope{}, false, nil
	}
	if err != nil {
		return domainrepo.Scope{}, false, mapError(err)
	}
	return scope, true, nil
}

func (repository *Repository) ClaimManagementEffect(ctx context.Context, scope domainrepo.Scope, now time.Time, lease time.Duration) (entity.ManagementEffect, bool, error) {
	var result entity.ManagementEffect
	leaseID := uuid.NewString()
	err := repository.managementTransact(ctx, scope, func(tx *transaction) error {
		return tx.tx.QueryRow(ctx, managementSQL("effect__claim"), pgx.StrictNamedArgs{
			"lease_id": leaseID, "lease_duration": fmt.Sprintf("%f seconds", lease.Seconds()),
		}).Scan(&result.ID, &result.Kind, &result.ResourceKind, &result.ResourceID,
			&result.ResourceVersion, &result.ResourceGeneration, &result.IntentDigest,
			&result.Status, &result.LeaseID, &result.LeaseFence, &result.LeaseExpiresAt,
			&result.Attempts, &result.Payload)
	})
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errs.ErrNotFound) {
		return entity.ManagementEffect{}, false, nil
	}
	return result, err == nil, err
}

func (repository *Repository) RenewManagementEffect(ctx context.Context, scope domainrepo.Scope, effectID, leaseID string, fence uint64, lease time.Duration) error {
	return repository.managementTransact(ctx, scope, func(tx *transaction) error {
		var id string
		return tx.tx.QueryRow(ctx, managementSQL("effect__renew"), pgx.StrictNamedArgs{
			"effect_id": effectID, "lease_id": leaseID, "lease_fence": fence,
			"lease_duration": fmt.Sprintf("%f seconds", lease.Seconds()),
		}).Scan(&id)
	})
}

func (repository *Repository) ManagementEffectSucceeded(ctx context.Context, scope domainrepo.Scope, effectID string) (bool, error) {
	var succeeded bool
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, managementSQL("effect__succeeded"), pgx.StrictNamedArgs{
			"effect_id": effectID, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
		}).Scan(&succeeded)
	})
	return succeeded, err
}

func (repository *Repository) CompleteManagementEffect(ctx context.Context, completion managementrepo.EffectCompletion) error {
	return repository.managementTransact(ctx, completion.Scope, func(tx *transaction) error {
		var id string
		return tx.tx.QueryRow(ctx, managementSQL("effect__complete"), pgx.StrictNamedArgs{
			"effect_id": completion.EffectID, "lease_id": completion.LeaseID,
			"lease_fence": completion.LeaseFence, "status": completion.Status,
			"failure_category": completion.FailureCategory, "updated_at": completion.At,
		}).Scan(&id)
	})
}

func (repository *Repository) FailManagementEffect(ctx context.Context, failure managementrepo.EffectFailure) error {
	return repository.managementTransact(ctx, failure.Scope, func(tx *transaction) error {
		var id string
		return tx.tx.QueryRow(ctx, managementSQL("effect__fail"), pgx.StrictNamedArgs{
			"effect_id": failure.EffectID, "lease_id": failure.LeaseID,
			"lease_fence": failure.LeaseFence, "status": failure.Status,
			"failure_category": failure.FailureCategory, "updated_at": failure.At,
		}).Scan(&id)
	})
}

func (repository *Repository) CompleteProviderSync(ctx context.Context, completion managementrepo.ProviderSyncCompletion) (entity.ManagedProviderConnection, error) {
	var result entity.ManagedProviderConnection
	err := repository.managementTransact(ctx, completion.Scope, func(tx *transaction) error {
		var resourceID string
		if err := tx.tx.QueryRow(ctx, managementSQL("effect__claim_by_id"), pgx.StrictNamedArgs{
			"effect_id": completion.EffectID,
		}).Scan(&resourceID); err != nil {
			return err
		}
		current, err := getManagedConnectionTx(ctx, tx.tx, completion.Scope, resourceID)
		if err != nil {
			return err
		}
		if current.Version != completion.ExpectedVersion || current.Generation != completion.ExpectedGeneration || current.Status != "PENDING" && current.Status != "VALID" {
			return errs.ErrConflict
		}
		credential, err := getCredentialGenerationTx(ctx, tx.tx, completion.Scope, current.ID, completion.ExpectedGeneration)
		if err != nil {
			return err
		}
		if credential.Status != "PENDING" {
			return errs.ErrConflict
		}
		current.Version++
		current.Status = "VALID"
		current.ActiveCredential = credential.Generation
		current.CredentialBindingID = credential.CredentialBindingID
		current.CredentialBindingVersion = credential.CredentialBindingVersion
		current.CredentialBindingDigest = credential.CredentialBindingDigest
		current.MaskedAccount, current.MaskedLabel = credential.MaskedAccount, credential.MaskedLabel
		current.ObservationDigest, current.ObservedAt, current.UpdatedAt = completion.ObservationDigest, &completion.ObservedAt, completion.ObservedAt
		current.ControlPlaneID, current.ControlPlaneVersion, current.ControlPlaneDigest = completion.ControlPlaneID, completion.ControlPlaneVersion, completion.ControlPlaneDigest
		payload, _, err := managementPayload(current)
		if err != nil {
			return err
		}
		var changed string
		if err = tx.tx.QueryRow(ctx, managementSQL("connection__sync_complete"), pgx.StrictNamedArgs{
			"connection_id": current.ID, "expected_version": completion.ExpectedVersion,
			"expected_generation": completion.ExpectedGeneration, "observation_sha256": completion.ObservationDigest,
			"observed_at": completion.ObservedAt, "control_plane_resource_id": completion.ControlPlaneID,
			"control_plane_version": completion.ControlPlaneVersion, "control_plane_sha256": completion.ControlPlaneDigest,
			"active_credential_generation": credential.Generation, "masked_account": credential.MaskedAccount, "masked_label": credential.MaskedLabel,
			"payload": payload, "effect_id": completion.EffectID, "lease_id": completion.LeaseID,
			"lease_fence": completion.LeaseFence,
		}).Scan(&changed); err != nil {
			return err
		}
		result = current
		return nil
	})
	return result, err
}

func (repository *Repository) CheckManagement(ctx context.Context) error {
	var ready bool
	if err := repository.pool.QueryRow(ctx, managementSQL("readiness__check")).Scan(&ready); err != nil || !ready {
		return errors.New("management PostgreSQL path is not ready")
	}
	return nil
}

func validPageToken(value string) bool {
	return value == "" || uuid.Validate(value) == nil
}

func pageSizeString(value int) string { return strconv.Itoa(pageLimit(value)) }
