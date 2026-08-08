package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	managementrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/management"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func getPoolTx(ctx context.Context, tx pgx.Tx, scope domainrepo.Scope, id string) (entity.ManagedProviderPool, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, managementSQL("pool__get"), pgx.StrictNamedArgs{
		"provider_pool_id": id, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
	}).Scan(&raw); err != nil {
		return entity.ManagedProviderPool{}, err
	}
	var value entity.ManagedProviderPool
	if json.Unmarshal(raw, &value) != nil {
		return value, errors.New("stored provider pool is invalid")
	}
	return value, nil
}

func (repository *Repository) ManagePool(ctx context.Context, command managementrepo.ManagePoolCommand) (entity.ManagedProviderPool, bool, error) {
	var result entity.ManagedProviderPool
	var replay bool
	err := repository.managementTransact(ctx, command.Scope, func(tx *transaction) error {
		var current entity.ManagedProviderPool
		databaseNow := command.Pool.UpdatedAt
		if command.Action != "CREATE" {
			var currentRaw []byte
			if err := tx.tx.QueryRow(ctx, managementSQL("pool__lock"), pgx.StrictNamedArgs{
				"provider_pool_id": command.Pool.ID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
			}).Scan(&currentRaw, &databaseNow); err != nil {
				return err
			}
			if json.Unmarshal(currentRaw, &current) != nil {
				return errors.New("stored provider pool update is invalid")
			}
		}
		stored, found, err := lockManagementReceipt(ctx, tx, "provider_pool."+command.Action, command.IdempotencyHash, command.RequestHash)
		if err != nil {
			return err
		}
		if found {
			result, err = replayManagementReceipt[entity.ManagedProviderPool](stored)
			replay = true
			return err
		}
		if command.Action == "CREATE" {
			for _, member := range command.Pool.Members {
				connection, getErr := getManagedConnectionTx(ctx, tx.tx, command.Scope, member.ConnectionID)
				if getErr != nil {
					return getErr
				}
				if connection.Version != member.ConnectionVersion || connection.Generation != member.ConnectionGeneration || !connectionEligibleSnapshot(connection, databaseNow) {
					return errs.ErrConflict
				}
			}
			payload, _, marshalErr := managementPayload(command.Pool)
			if marshalErr != nil {
				return marshalErr
			}
			_, err = tx.tx.Exec(ctx, managementSQL("pool__insert"), pgx.StrictNamedArgs{
				"provider_pool_id": command.Pool.ID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
				"stable_key": command.Pool.StableKey, "display_name": command.Pool.DisplayName,
				"policy": command.Pool.Policy, "version": command.Pool.Version,
				"desired_sha256": command.Pool.DesiredDigest, "observation_version": command.Pool.ObservationVersion,
				"observation_sha256": command.Pool.ObservationDigest, "effective_sha256": command.Pool.EffectiveDigest,
				"status": command.Pool.Status, "payload": payload,
				"created_at": command.Pool.CreatedAt, "updated_at": command.Pool.UpdatedAt,
			})
			if err != nil {
				return err
			}
			result = command.Pool
		} else {
			if current.Version != command.ExpectedVersion || current.StableKey != command.Pool.StableKey ||
				command.Action == "UPDATE" && current.Status != "ACTIVE" ||
				command.Action == "ARCHIVE" && current.Status != "ACTIVE" ||
				command.Action == "DELETE" && current.Status != "ARCHIVED" {
				return errs.ErrConflict
			}
			command.Pool.CreatedAt = current.CreatedAt
			command.Pool.Version, command.Pool.UpdatedAt = current.Version+1, databaseNow
			command.Pool.ObservationVersion = current.ObservationVersion + 1
			command.Pool.ControlPlaneID = current.ControlPlaneID
			command.Pool.ControlPlaneVersion = current.ControlPlaneVersion
			command.Pool.ControlPlaneDigest = current.ControlPlaneDigest
			if command.Action == "ARCHIVE" {
				command.Pool.Status = "ARCHIVED"
			}
			if command.Action == "DELETE" {
				command.Pool.Status = "DELETED"
			}
			if command.Action == "UPDATE" {
				for _, member := range command.Pool.Members {
					connection, getErr := getManagedConnectionTx(ctx, tx.tx, command.Scope, member.ConnectionID)
					if getErr != nil {
						return getErr
					}
					if connection.Version != member.ConnectionVersion || connection.Generation != member.ConnectionGeneration || !connectionEligibleSnapshot(connection, databaseNow) {
						return errs.ErrConflict
					}
				}
			}
			payload, _, marshalErr := managementPayload(command.Pool)
			if marshalErr != nil {
				return marshalErr
			}
			var changed string
			if err = tx.tx.QueryRow(ctx, managementSQL("pool__update"), pgx.StrictNamedArgs{
				"provider_pool_id": command.Pool.ID, "expected_version": command.ExpectedVersion,
				"display_name": command.Pool.DisplayName, "policy": command.Pool.Policy,
				"version": command.Pool.Version, "desired_sha256": command.Pool.DesiredDigest,
				"observation_version": command.Pool.ObservationVersion, "observation_sha256": command.Pool.ObservationDigest,
				"effective_sha256": command.Pool.EffectiveDigest, "status": command.Pool.Status,
				"payload": payload, "updated_at": databaseNow,
			}).Scan(&changed); err != nil {
				return err
			}
			result = command.Pool
		}
		resultPayload, _, _ := managementPayload(result)
		if err = insertManagementEffect(ctx, tx, "PROVIDER_POOL_SYNC", "managed_provider_pool", result.ID,
			result.Version, 0, managementEffectOwner{"managed_provider_pool", result.ID, result.Status, result.Version, 0}, command.RequestHash, databaseNow); err != nil {
			return err
		}
		if err = insertManagementReceipt(ctx, tx, "provider_pool."+command.Action, command.IdempotencyHash, command.RequestHash,
			"managed_provider_pool", result.ID, result.Version, resultPayload, databaseNow); err != nil {
			return err
		}
		command.Audit.ResourceID, command.Audit.OccurredAt = result.ID, databaseNow
		return tx.appendAudit(ctx, command.Audit)
	})
	return result, replay, err
}

func connectionEligibleSnapshot(connection entity.ManagedProviderConnection, now time.Time) bool {
	return connection.Status == "VALID" && connection.Generation > 0 && connection.ActiveCredential == connection.Generation &&
		connection.CredentialBindingID != "" && connection.CredentialBindingVersion > 0 && len(connection.CredentialBindingDigest) == 64 &&
		connection.ControlPlaneID != "" && connection.ControlPlaneVersion > 0 && len(connection.ControlPlaneDigest) == 64 && connection.ObservedAt != nil &&
		!connection.ObservedAt.After(now.Add(time.Second)) && now.Sub(*connection.ObservedAt) <= 5*time.Minute &&
		connection.Capacity.Limit > 0 && connection.Capacity.Usage <= connection.Capacity.Limit &&
		connection.Capacity.Revision > 0 && len(connection.Capacity.Digest) == 64 &&
		connection.Capacity.ExpiresAt.After(now)
}

func (repository *Repository) GetPool(ctx context.Context, scope domainrepo.Scope, id string) (entity.ManagedProviderPool, error) {
	var value entity.ManagedProviderPool
	err := repository.read(ctx, scope, func(tx pgx.Tx) error { var err error; value, err = getPoolTx(ctx, tx, scope, id); return err })
	return value, err
}

func (repository *Repository) ListPools(ctx context.Context, scope domainrepo.Scope, limit int, after string) ([]entity.ManagedProviderPool, string, error) {
	values := make([]entity.ManagedProviderPool, 0)
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, managementSQL("pool__list"), pgx.StrictNamedArgs{
			"tenant_id": scope.TenantID, "project_id": scope.ProjectID, "after_id": after, "page_limit": pageLimit(limit),
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			var value entity.ManagedProviderPool
			if rows.Scan(&raw) != nil || json.Unmarshal(raw, &value) != nil {
				return errors.New("stored provider pool list is invalid")
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	values, next := nextPage(values, limit, func(value entity.ManagedProviderPool) string { return value.ID })
	return values, next, err
}

func getConfigurationTx(ctx context.Context, tx pgx.Tx, scope domainrepo.Scope, id string) (entity.IntegrationConfiguration, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, managementSQL("configuration__get"), pgx.StrictNamedArgs{
		"configuration_id": id, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
	}).Scan(&raw); err != nil {
		return entity.IntegrationConfiguration{}, err
	}
	var value entity.IntegrationConfiguration
	if json.Unmarshal(raw, &value) != nil {
		return value, errors.New("stored integration configuration is invalid")
	}
	return value, nil
}

func (repository *Repository) ConfigureIntegration(ctx context.Context, command managementrepo.ConfigureIntegrationCommand) (entity.IntegrationConfiguration, bool, error) {
	var result entity.IntegrationConfiguration
	var replay bool
	err := repository.managementTransact(ctx, command.Scope, func(tx *transaction) error {
		var current entity.IntegrationConfiguration
		if command.ExpectedVersion > 0 {
			var raw []byte
			if err := tx.tx.QueryRow(ctx, managementSQL("configuration__lock"), pgx.StrictNamedArgs{
				"configuration_id": command.Configuration.ID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
			}).Scan(&raw); err != nil {
				return err
			}
			if json.Unmarshal(raw, &current) != nil {
				return errors.New("stored integration configuration update is invalid")
			}
		}
		stored, found, err := lockManagementReceipt(ctx, tx, "integration.configure", command.IdempotencyHash, command.RequestHash)
		if err != nil {
			return err
		}
		if found {
			result, err = replayManagementReceipt[entity.IntegrationConfiguration](stored)
			replay = true
			return err
		}
		if command.ExpectedVersion > 0 {
			if current.Version != command.ExpectedVersion {
				return errs.ErrConflict
			}
			command.Configuration.Version = current.Version + 1
			command.Configuration.CreatedAt = current.CreatedAt
		}
		connection, err := getManagedConnectionTx(ctx, tx.tx, command.Scope, command.Configuration.ConnectionID)
		if err != nil {
			return err
		}
		if connection.Version != command.Configuration.ConnectionVersion || connection.Generation != command.Configuration.ConnectionGeneration || connection.Status != "VALID" {
			return errs.ErrConflict
		}
		payload, _, err := managementPayload(command.Configuration)
		if err != nil {
			return err
		}
		_, err = tx.tx.Exec(ctx, managementSQL("configuration__insert"), pgx.StrictNamedArgs{
			"configuration_id": command.Configuration.ID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
			"stable_key": command.Configuration.StableKey, "version": command.Configuration.Version,
			"configuration_sha256": command.Configuration.Digest, "definition_id": command.Configuration.DefinitionID,
			"definition_version": command.Configuration.DefinitionVersion, "definition_sha256": command.Configuration.DefinitionDigest,
			"connection_id": command.Configuration.ConnectionID, "connection_version": command.Configuration.ConnectionVersion,
			"connection_generation": command.Configuration.ConnectionGeneration,
			"capability_sha256":     command.Configuration.CapabilityDigest, "effect_kind": command.Configuration.EffectKind,
			"status": command.Configuration.Status, "payload": payload,
			"created_at": command.Configuration.CreatedAt, "updated_at": command.Configuration.UpdatedAt,
		})
		if err != nil {
			return err
		}
		if err = insertManagementReceipt(ctx, tx, "integration.configure", command.IdempotencyHash, command.RequestHash,
			"integration_configuration", command.Configuration.ID, command.Configuration.Version, payload, command.Configuration.UpdatedAt); err != nil {
			return err
		}
		if err = tx.appendAudit(ctx, command.Audit); err != nil {
			return err
		}
		result = command.Configuration
		return nil
	})
	return result, replay, err
}

func (repository *Repository) GetIntegrationConfiguration(ctx context.Context, scope domainrepo.Scope, id string) (entity.IntegrationConfiguration, error) {
	var value entity.IntegrationConfiguration
	err := repository.read(ctx, scope, func(tx pgx.Tx) error { var err error; value, err = getConfigurationTx(ctx, tx, scope, id); return err })
	return value, err
}

func (repository *Repository) GetIntegrationConfigurationVersion(ctx context.Context, scope domainrepo.Scope, id string, version uint64) (entity.IntegrationConfiguration, error) {
	var value entity.IntegrationConfiguration
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, managementSQL("configuration__get_version"), pgx.StrictNamedArgs{
			"configuration_id": id, "version": version, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
		}).Scan(&raw); err != nil {
			return err
		}
		if json.Unmarshal(raw, &value) != nil {
			return errors.New("stored integration configuration version is invalid")
		}
		return nil
	})
	return value, err
}

func (repository *Repository) ListIntegrationConfigurations(ctx context.Context, scope domainrepo.Scope, limit int, after string) ([]entity.IntegrationConfiguration, string, error) {
	values := make([]entity.IntegrationConfiguration, 0)
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, managementSQL("configuration__list"), pgx.StrictNamedArgs{
			"tenant_id": scope.TenantID, "project_id": scope.ProjectID, "after_id": after, "page_limit": pageLimit(limit),
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			var value entity.IntegrationConfiguration
			if rows.Scan(&raw) != nil || json.Unmarshal(raw, &value) != nil {
				return errors.New("stored integration configuration list is invalid")
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	values, next := nextPage(values, limit, func(value entity.IntegrationConfiguration) string { return value.ID })
	return values, next, err
}

func getTestTx(ctx context.Context, tx pgx.Tx, scope domainrepo.Scope, id string) (entity.IntegrationTestReceipt, error) {
	value := entity.IntegrationTestReceipt{ID: id}
	err := tx.QueryRow(ctx, managementSQL("test__get"), pgx.StrictNamedArgs{
		"test_id": id, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
	}).Scan(&value.ConnectionID, &value.ConnectionVersion, &value.ConnectionGeneration,
		&value.DefinitionID, &value.DefinitionVersion, &value.DefinitionDigest,
		&value.ConfigurationID, &value.ConfigurationVersion, &value.ConfigurationDigest,
		&value.CredentialGeneration, &value.CredentialBindingID, &value.CredentialBindingVersion,
		&value.CredentialBindingDigest, &value.Category, &value.Digest, &value.ExpiresAt, &value.TestedAt)
	return value, err
}

func (repository *Repository) GetTest(ctx context.Context, scope domainrepo.Scope, id string) (entity.IntegrationTestReceipt, error) {
	var result entity.IntegrationTestReceipt
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		var err error
		result, err = getTestTx(ctx, tx, scope, id)
		return err
	})
	return result, err
}

func (repository *Repository) CreateTest(ctx context.Context, command managementrepo.CreateTestCommand) (entity.IntegrationTestReceipt, bool, error) {
	var result entity.IntegrationTestReceipt
	var replay bool
	err := repository.managementTransact(ctx, command.Scope, func(tx *transaction) error {
		var connectionRaw []byte
		var databaseNow time.Time
		if err := tx.tx.QueryRow(ctx, managementSQL("connection__revoke_lock"), pgx.StrictNamedArgs{
			"connection_id": command.Receipt.ConnectionID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
		}).Scan(&connectionRaw, &databaseNow); err != nil {
			return err
		}
		var connection entity.ManagedProviderConnection
		if json.Unmarshal(connectionRaw, &connection) != nil {
			return errors.New("stored integration test connection is invalid")
		}
		stored, found, err := lockManagementReceipt(ctx, tx, "integration.test", command.IdempotencyHash, command.RequestHash)
		if err != nil {
			return err
		}
		if found {
			result, err = replayManagementReceipt[entity.IntegrationTestReceipt](stored)
			replay = true
			return err
		}
		if connection.Version != command.Receipt.ConnectionVersion || connection.Generation != command.Receipt.ConnectionGeneration || connection.Status != "VALID" {
			return errs.ErrConflict
		}
		command.At = databaseNow
		_, err = tx.tx.Exec(ctx, managementSQL("test__insert"), pgx.StrictNamedArgs{
			"test_id": command.Receipt.ID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
			"connection_id": command.Receipt.ConnectionID, "connection_version": command.Receipt.ConnectionVersion,
			"connection_generation": command.Receipt.ConnectionGeneration, "definition_id": command.Receipt.DefinitionID,
			"definition_version": command.Receipt.DefinitionVersion, "definition_sha256": command.Receipt.DefinitionDigest,
			"configuration_id": command.Receipt.ConfigurationID, "configuration_version": command.Receipt.ConfigurationVersion,
			"configuration_sha256": command.Receipt.ConfigurationDigest, "credential_generation": command.Receipt.CredentialGeneration,
			"credential_binding_id": command.Receipt.CredentialBindingID, "credential_binding_version": command.Receipt.CredentialBindingVersion,
			"credential_binding_sha256": command.Receipt.CredentialBindingDigest, "category": command.Receipt.Category,
			"receipt_sha256": command.Receipt.Digest, "expires_at": command.Receipt.ExpiresAt, "created_at": command.At,
		})
		if err != nil {
			return err
		}
		if err = insertManagementEffect(ctx, tx, "INTEGRATION_TEST", "integration_test", command.Receipt.ID,
			1, command.Receipt.ConnectionGeneration, managementEffectOwner{"managed_provider_connection", command.Connection.ID, command.Connection.Status, command.Connection.Version, command.Connection.Generation}, command.RequestHash, command.At); err != nil {
			return err
		}
		receiptPayload, _, err := managementPayload(command.Receipt)
		if err != nil {
			return err
		}
		if err = insertManagementReceipt(ctx, tx, "integration.test", command.IdempotencyHash, command.RequestHash,
			"integration_test", command.Receipt.ID, 1, receiptPayload, command.At); err != nil {
			return err
		}
		if err = tx.appendAudit(ctx, command.Audit); err != nil {
			return err
		}
		result = command.Receipt
		return nil
	})
	return result, replay, err
}

func getGitBindingTx(ctx context.Context, tx pgx.Tx, scope domainrepo.Scope, id string) (entity.GitSourceBinding, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, managementSQL("git_binding__get"), pgx.StrictNamedArgs{
		"binding_id": id, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
	}).Scan(&raw); err != nil {
		return entity.GitSourceBinding{}, err
	}
	var value entity.GitSourceBinding
	if json.Unmarshal(raw, &value) != nil {
		return value, errors.New("stored Git source binding is invalid")
	}
	return value, nil
}

func (repository *Repository) ManageGitBinding(ctx context.Context, command managementrepo.ManageGitBindingCommand) (entity.GitSourceBinding, bool, error) {
	var result entity.GitSourceBinding
	var replay bool
	err := repository.managementTransact(ctx, command.Scope, func(tx *transaction) error {
		var current entity.GitSourceBinding
		var databaseNow time.Time
		if command.Action != "CREATE" {
			var raw []byte
			if err := tx.tx.QueryRow(ctx, managementSQL("git_binding__lock"), pgx.StrictNamedArgs{
				"binding_id": command.Binding.ID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
			}).Scan(&raw, &databaseNow); err != nil {
				return err
			}
			if json.Unmarshal(raw, &current) != nil {
				return errors.New("stored Git source binding update is invalid")
			}
		}
		stored, found, err := lockManagementReceipt(ctx, tx, "git_binding."+command.Action, command.IdempotencyHash, command.RequestHash)
		if err != nil {
			return err
		}
		if found {
			result, err = replayManagementReceipt[entity.GitSourceBinding](stored)
			replay = true
			return err
		}
		if command.Action == "CREATE" {
			payload, _, marshalErr := managementPayload(command.Binding)
			if marshalErr != nil {
				return marshalErr
			}
			_, err = tx.tx.Exec(ctx, managementSQL("git_binding__insert"), pgx.StrictNamedArgs{
				"binding_id": command.Binding.ID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
				"stable_key": command.Binding.StableKey, "version": command.Binding.Version, "generation": command.Binding.Generation, "status": command.Binding.Status,
				"repository_key": command.Binding.RepositoryKey, "ref_key": command.Binding.RefKey, "path_key": command.Binding.PathKey,
				"repository_connection_id":      command.Binding.RepositoryConnectionID,
				"repository_connection_version": command.Binding.RepositoryConnectionVersion,
				"repository_connection_sha256":  command.Binding.RepositoryConnectionDigest,
				"credential_binding_id":         command.Binding.CredentialBindingID,
				"credential_binding_version":    command.Binding.CredentialBindingVersion,
				"credential_binding_sha256":     command.Binding.CredentialBindingDigest,
				"target_kind":                   command.Binding.TargetKind, "target_stable_key": command.Binding.TargetStableKey,
				"payload": payload, "created_at": command.Binding.CreatedAt, "updated_at": command.Binding.UpdatedAt,
			})
			if err != nil {
				return err
			}
			result = command.Binding
		} else {
			if current.Version != command.ExpectedVersion || current.Status == "ARCHIVED" {
				return errs.ErrConflict
			}
			command.Binding.Version, command.Binding.Generation, command.Binding.CreatedAt, command.Binding.UpdatedAt = current.Version+1, current.Generation+1, current.CreatedAt, databaseNow
			if command.Action == "ARCHIVE" {
				command.Binding.Status = "ARCHIVED"
			}
			payload, _, _ := managementPayload(command.Binding)
			var changed string
			if err = tx.tx.QueryRow(ctx, managementSQL("git_binding__update"), pgx.StrictNamedArgs{
				"binding_id": command.Binding.ID, "expected_version": command.ExpectedVersion,
				"version": command.Binding.Version, "generation": command.Binding.Generation, "status": command.Binding.Status,
				"repository_key": command.Binding.RepositoryKey, "ref_key": command.Binding.RefKey, "path_key": command.Binding.PathKey,
				"repository_connection_id":      command.Binding.RepositoryConnectionID,
				"repository_connection_version": command.Binding.RepositoryConnectionVersion,
				"repository_connection_sha256":  command.Binding.RepositoryConnectionDigest,
				"credential_binding_id":         command.Binding.CredentialBindingID,
				"credential_binding_version":    command.Binding.CredentialBindingVersion,
				"credential_binding_sha256":     command.Binding.CredentialBindingDigest,
				"target_kind":                   command.Binding.TargetKind, "target_stable_key": command.Binding.TargetStableKey,
				"fetched_commit": command.Binding.FetchedCommit, "source_revision": command.Binding.SourceRevision,
				"source_sha256": command.Binding.SourceDigest, "fetched_at": command.Binding.FetchedAt,
				"payload": payload, "updated_at": command.Binding.UpdatedAt,
			}).Scan(&changed); err != nil {
				return err
			}
			result = command.Binding
		}
		resultPayload, _, _ := managementPayload(result)
		if err = insertManagementReceipt(ctx, tx, "git_binding."+command.Action, command.IdempotencyHash, command.RequestHash,
			"git_source_binding", result.ID, result.Version, resultPayload, result.UpdatedAt); err != nil {
			return err
		}
		command.Audit.ResourceID, command.Audit.OccurredAt = result.ID, result.UpdatedAt
		return tx.appendAudit(ctx, command.Audit)
	})
	return result, replay, err
}

func (repository *Repository) GetGitBinding(ctx context.Context, scope domainrepo.Scope, id string) (entity.GitSourceBinding, error) {
	var value entity.GitSourceBinding
	err := repository.read(ctx, scope, func(tx pgx.Tx) error { var err error; value, err = getGitBindingTx(ctx, tx, scope, id); return err })
	return value, err
}

func (repository *Repository) ListGitBindings(ctx context.Context, scope domainrepo.Scope, limit int, after string) ([]entity.GitSourceBinding, string, error) {
	values := make([]entity.GitSourceBinding, 0)
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, managementSQL("git_binding__list"), pgx.StrictNamedArgs{
			"tenant_id": scope.TenantID, "project_id": scope.ProjectID, "after_id": after, "page_limit": pageLimit(limit),
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			var value entity.GitSourceBinding
			if rows.Scan(&raw) != nil || json.Unmarshal(raw, &value) != nil {
				return errors.New("stored Git binding list is invalid")
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	values, next := nextPage(values, limit, func(value entity.GitSourceBinding) string { return value.ID })
	return values, next, err
}

func (repository *Repository) CreateGitReconciliation(ctx context.Context, command managementrepo.ReconcileGitCommand) (entity.GitReconciliation, bool, error) {
	var result entity.GitReconciliation
	var replay bool
	err := repository.managementTransact(ctx, command.Scope, func(tx *transaction) error {
		var raw []byte
		var databaseNow time.Time
		if err := tx.tx.QueryRow(ctx, managementSQL("git_binding__lock"), pgx.StrictNamedArgs{
			"binding_id": command.BindingID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
		}).Scan(&raw, &databaseNow); err != nil {
			return err
		}
		var binding entity.GitSourceBinding
		if json.Unmarshal(raw, &binding) != nil {
			return errors.New("stored Git reconciliation binding is invalid")
		}
		stored, found, err := lockManagementReceipt(ctx, tx, "git_binding.reconcile", command.IdempotencyHash, command.RequestHash)
		if err != nil {
			return err
		}
		if found {
			result, err = replayManagementReceipt[entity.GitReconciliation](stored)
			replay = true
			return err
		}
		command.Reconciliation.UpdatedAt = databaseNow
		if binding.Version != command.ExpectedVersion || binding.Status != "ACTIVE" || binding.SourceRevision != command.ExpectedSourceRevision {
			return errs.ErrConflict
		}
		_, err = tx.tx.Exec(ctx, managementSQL("git_reconciliation__insert"), pgx.StrictNamedArgs{
			"reconciliation_id": command.Reconciliation.ID, "tenant_id": tx.tenantID, "project_id": tx.projectID,
			"binding_id": binding.ID, "binding_version": binding.Version, "state": command.Reconciliation.State,
			"command_intent_sha256": command.Reconciliation.CommandIntentDigest,
			"receipt_id":            command.Reconciliation.ReceiptID, "receipt_sha256": command.Reconciliation.ReceiptDigest,
			"created_at": command.Reconciliation.UpdatedAt, "updated_at": command.Reconciliation.UpdatedAt,
		})
		if err != nil {
			return err
		}
		if err = insertManagementEffect(ctx, tx, "GIT_FETCH", "git_reconciliation", command.Reconciliation.ID,
			binding.Version, binding.Generation, managementEffectOwner{"git_source_binding", binding.ID, binding.Status, binding.Version, binding.Generation}, command.RequestHash, command.Reconciliation.UpdatedAt); err != nil {
			return err
		}
		resultPayload, _, _ := managementPayload(command.Reconciliation)
		if err = insertManagementReceipt(ctx, tx, "git_binding.reconcile", command.IdempotencyHash, command.RequestHash,
			"git_reconciliation", command.Reconciliation.ID, 1, resultPayload, command.Reconciliation.UpdatedAt); err != nil {
			return err
		}
		if err = tx.appendAudit(ctx, command.Audit); err != nil {
			return err
		}
		result = command.Reconciliation
		return nil
	})
	return result, replay, err
}

func getGitReconciliationTx(ctx context.Context, tx pgx.Tx, scope domainrepo.Scope, id string) (entity.GitReconciliation, error) {
	value := entity.GitReconciliation{ID: id}
	err := tx.QueryRow(ctx, managementSQL("git_reconciliation__get"), pgx.StrictNamedArgs{
		"reconciliation_id": id, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
	}).Scan(&value.BindingID, &value.BindingVersion, &value.State, &value.FetchedCommit,
		&value.SourceRevision, &value.SourceDigest, &value.EncryptedSnapshot,
		&value.TargetResourceID, &value.TargetVersion, &value.TargetDigest,
		&value.CommandIntentDigest, &value.ReceiptID, &value.ReceiptDigest,
		&value.FailureCategory, &value.UpdatedAt)
	return value, err
}

func (repository *Repository) GetGitReconciliation(ctx context.Context, scope domainrepo.Scope, id string) (entity.GitReconciliation, error) {
	var value entity.GitReconciliation
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		var err error
		value, err = getGitReconciliationTx(ctx, tx, scope, id)
		return err
	})
	return value, err
}

func (repository *Repository) GetApproval(ctx context.Context, scope domainrepo.Scope, id string) (entity.Approval, error) {
	var value entity.Approval
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		var raw []byte
		var version uint64
		if err := tx.QueryRow(ctx, managementSQL("approval__get"), pgx.StrictNamedArgs{
			"approval_id": id, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
		}).Scan(&raw, &version); err != nil {
			return err
		}
		if json.Unmarshal(raw, &value) != nil {
			return errors.New("stored approval is invalid")
		}
		value.Version = version
		return nil
	})
	return value, err
}

func (repository *Repository) ListApprovals(ctx context.Context, scope domainrepo.Scope, states []string, limit int, after string) ([]entity.Approval, string, error) {
	values := make([]entity.Approval, 0)
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, managementSQL("approval__list"), pgx.StrictNamedArgs{
			"tenant_id": scope.TenantID, "project_id": scope.ProjectID, "states": states,
			"after_id": after, "page_limit": pageLimit(limit),
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			var version uint64
			var value entity.Approval
			if rows.Scan(&raw, &version) != nil || json.Unmarshal(raw, &value) != nil {
				return errors.New("stored approval list is invalid")
			}
			value.Version = version
			values = append(values, value)
		}
		return rows.Err()
	})
	values, next := nextPage(values, limit, func(value entity.Approval) string { return value.ID })
	return values, next, err
}

func (repository *Repository) MarkAuthorizationCode(ctx context.Context, scope domainrepo.Scope, id, leaseID string, fence uint64, encryptedDeviceResult []byte, expiresAt, at time.Time) error {
	return repository.managementTransact(ctx, scope, func(tx *transaction) error {
		var changed string
		return tx.tx.QueryRow(ctx, managementSQL("authorization__code"), pgx.StrictNamedArgs{
			"authorization_id": id, "lease_id": leaseID, "lease_generation": fence,
			"device_result_ciphertext": encryptedDeviceResult,
			"code_expires_at":          expiresAt, "updated_at": at,
		}).Scan(&changed)
	})
}

func (repository *Repository) AuthorizationCancelled(ctx context.Context, scope domainrepo.Scope, id, leaseID string, fence uint64) (bool, error) {
	var cancelled bool
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, managementSQL("authorization__cancelled"), pgx.StrictNamedArgs{
			"authorization_id": id, "lease_id": leaseID, "lease_generation": fence,
		}).Scan(&cancelled)
	})
	if errors.Is(err, errs.ErrNotFound) {
		return true, nil
	}
	return cancelled, err
}

func (repository *Repository) GetCredentialGeneration(ctx context.Context, scope domainrepo.Scope, connectionID string, generation uint64) (entity.CredentialGeneration, error) {
	var value entity.CredentialGeneration
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		var err error
		value, err = getCredentialGenerationTx(ctx, tx, scope, connectionID, generation)
		return err
	})
	return value, err
}

func (repository *Repository) ListCredentialGenerations(ctx context.Context, scope domainrepo.Scope, connectionID string) ([]entity.CredentialGeneration, error) {
	values := make([]entity.CredentialGeneration, 0)
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, managementSQL("credential__list"), pgx.StrictNamedArgs{
			"connection_id": connectionID, "tenant_id": scope.TenantID, "project_id": scope.ProjectID,
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var value entity.CredentialGeneration
			value.ConnectionID = connectionID
			if err = rows.Scan(&value.Generation, &value.AuthorizationID, &value.Status, &value.SecretRef,
				&value.SecretVersion, &value.SecretContentDigest, &value.CredentialBindingID,
				&value.CredentialBindingVersion, &value.CredentialBindingDigest,
				&value.MaskedAccount, &value.MaskedLabel, &value.Capacity.Usage, &value.Capacity.Limit,
				&value.Capacity.Revision, &value.Capacity.ObservedAt, &value.Capacity.WindowSeconds,
				&value.Capacity.ResetsAt, &value.Capacity.ExpiresAt, &value.Capacity.Digest); err != nil {
				return err
			}
			values = append(values, value)
		}
		return rows.Err()
	})
	return values, err
}

func getCredentialGenerationTx(ctx context.Context, tx pgx.Tx, scope domainrepo.Scope, connectionID string, generation uint64) (entity.CredentialGeneration, error) {
	value := entity.CredentialGeneration{ConnectionID: connectionID, Generation: generation}
	err := tx.QueryRow(ctx, managementSQL("credential__get"), pgx.StrictNamedArgs{"connection_id": connectionID, "generation": generation, "tenant_id": scope.TenantID, "project_id": scope.ProjectID}).Scan(&value.AuthorizationID, &value.Status, &value.SecretRef, &value.SecretVersion, &value.SecretContentDigest, &value.CredentialBindingID, &value.CredentialBindingVersion, &value.CredentialBindingDigest, &value.MaskedAccount, &value.MaskedLabel, &value.Capacity.Usage, &value.Capacity.Limit, &value.Capacity.Revision, &value.Capacity.ObservedAt, &value.Capacity.WindowSeconds, &value.Capacity.ResetsAt, &value.Capacity.ExpiresAt, &value.Capacity.Digest)
	return value, err
}

func (repository *Repository) CompletePoolSync(ctx context.Context, completion managementrepo.PoolSyncCompletion) (entity.ManagedProviderPool, error) {
	var result entity.ManagedProviderPool
	err := repository.managementTransact(ctx, completion.Scope, func(tx *transaction) error {
		var effectResourceID string
		if err := tx.tx.QueryRow(ctx, managementSQL("effect__claim_by_id"), pgx.StrictNamedArgs{"effect_id": completion.EffectID}).Scan(&effectResourceID); err != nil {
			return err
		}
		current, err := getPoolTx(ctx, tx.tx, completion.Scope, effectResourceID)
		if err != nil {
			return err
		}
		if current.Version != completion.ExpectedVersion {
			return errs.ErrConflict
		}
		if current.Status == "PENDING" {
			current.Status = "ACTIVE"
		}
		current.ControlPlaneID, current.ControlPlaneVersion, current.ControlPlaneDigest, current.UpdatedAt = completion.ControlPlaneID, completion.ControlPlaneVersion, completion.ControlPlaneDigest, completion.At
		payload, _, err := managementPayload(current)
		if err != nil {
			return err
		}
		var changed string
		if err = tx.tx.QueryRow(ctx, managementSQL("pool__sync_complete"), pgx.StrictNamedArgs{"provider_pool_id": current.ID, "expected_version": completion.ExpectedVersion, "control_plane_resource_id": completion.ControlPlaneID, "control_plane_version": completion.ControlPlaneVersion, "control_plane_sha256": completion.ControlPlaneDigest, "payload": payload, "updated_at": completion.At, "effect_id": completion.EffectID, "lease_id": completion.LeaseID, "lease_fence": completion.LeaseFence}).Scan(&changed); err != nil {
			return err
		}
		result = current
		return nil
	})
	return result, err
}

func (repository *Repository) CompleteTest(ctx context.Context, completion managementrepo.TestCompletion) (entity.IntegrationTestReceipt, error) {
	var result entity.IntegrationTestReceipt
	err := repository.managementTransact(ctx, completion.Scope, func(tx *transaction) error {
		var changed string
		if err := tx.tx.QueryRow(ctx, managementSQL("test__complete"), pgx.StrictNamedArgs{"test_id": completion.TestID, "category": completion.Category, "receipt_sha256": completion.Digest, "tested_at": completion.At, "effect_id": completion.EffectID, "lease_id": completion.LeaseID, "lease_fence": completion.LeaseFence}).Scan(&changed); err != nil {
			return err
		}
		var err error
		result, err = getTestTx(ctx, tx.tx, completion.Scope, changed)
		return err
	})
	return result, err
}

func (repository *Repository) CompleteGitFetch(ctx context.Context, completion managementrepo.GitFetchCompletion) (entity.GitReconciliation, error) {
	var result entity.GitReconciliation
	err := repository.managementTransact(ctx, completion.Scope, func(tx *transaction) error {
		bindingPayload, _, err := managementPayload(completion.Binding)
		if err != nil {
			return err
		}
		var applyEffectID string
		if err = tx.tx.QueryRow(ctx, managementSQL("git__fetch_complete"), pgx.StrictNamedArgs{"binding_id": completion.Binding.ID, "binding_version": completion.Binding.Version, "binding_generation": completion.Binding.Generation, "fetched_commit": completion.Reconciliation.FetchedCommit, "source_revision": completion.Reconciliation.SourceRevision, "source_sha256": completion.Reconciliation.SourceDigest, "command_intent_sha256": completion.Reconciliation.CommandIntentDigest, "fetch_input_sha256": completion.InputDigest, "fetched_at": completion.At, "binding_payload": bindingPayload, "reconciliation_id": completion.Reconciliation.ID, "encrypted_snapshot": completion.Reconciliation.EncryptedSnapshot, "receipt_sha256": completion.Reconciliation.ReceiptDigest, "effect_id": completion.EffectID, "lease_id": completion.LeaseID, "lease_fence": completion.LeaseFence, "apply_effect_id": uuid.NewString(), "tenant_id": completion.Scope.TenantID, "project_id": completion.Scope.ProjectID, "actor_id": completion.Scope.ActorID, "intent_sha256": completion.Reconciliation.CommandIntentDigest, "effect_payload": managementEffectPayload(completion.Reconciliation.ID, completion.Binding.Version, completion.Binding.Generation)}).Scan(&applyEffectID); err != nil {
			return err
		}
		result, err = getGitReconciliationTx(ctx, tx.tx, completion.Scope, completion.Reconciliation.ID)
		return err
	})
	return result, err
}

func (repository *Repository) CompleteGitApply(ctx context.Context, completion managementrepo.GitApplyCompletion) (entity.GitReconciliation, error) {
	var result entity.GitReconciliation
	err := repository.managementTransact(ctx, completion.Scope, func(tx *transaction) error {
		var changed string
		if err := tx.tx.QueryRow(ctx, managementSQL("git__apply_complete"), pgx.StrictNamedArgs{"reconciliation_id": completion.ReconciliationID, "binding_id": completion.BindingID, "binding_version": completion.BindingVersion, "binding_generation": completion.BindingGeneration, "source_revision": completion.SourceRevision, "source_sha256": completion.SourceDigest, "target_resource_id": completion.ReadbackID, "target_version": completion.ReadbackVersion, "target_sha256": completion.ReadbackDigest, "updated_at": completion.At, "effect_id": completion.EffectID, "lease_id": completion.LeaseID, "lease_fence": completion.LeaseFence}).Scan(&changed); err != nil {
			return err
		}
		var err error
		result, err = getGitReconciliationTx(ctx, tx.tx, completion.Scope, changed)
		return err
	})
	return result, err
}

func (repository *Repository) CompleteAuthorization(ctx context.Context, scope domainrepo.Scope, id, effectID, leaseID string, fence uint64, credential entity.CredentialGeneration, maskedLabel, intentDigest string, at time.Time) error {
	return repository.managementTransact(ctx, scope, func(tx *transaction) error {
		connection, err := getManagedConnectionTx(ctx, tx.tx, scope, credential.ConnectionID)
		if err != nil {
			return err
		}
		if connection.Status == "REVOKED" || connection.Generation != credential.Generation {
			return errs.ErrConflict
		}
		effectPayload := managementEffectPayload(connection.ID, connection.Version, connection.Generation)
		var providerSyncEffectID string
		return tx.tx.QueryRow(ctx, managementSQL("authorization__complete"), pgx.StrictNamedArgs{
			"authorization_id": id, "authorization_effect_id": effectID, "lease_id": leaseID, "lease_generation": fence,
			"tenant_id": scope.TenantID, "project_id": scope.ProjectID,
			"secret_ref": credential.SecretRef, "secret_version": credential.SecretVersion,
			"secret_content_sha256":      credential.SecretContentDigest,
			"credential_binding_id":      credential.CredentialBindingID,
			"credential_binding_version": credential.CredentialBindingVersion,
			"credential_binding_sha256":  credential.CredentialBindingDigest,
			"masked_account":             credential.MaskedAccount, "masked_label": maskedLabel,
			"observed_usage": credential.Capacity.Usage, "observed_limit": credential.Capacity.Limit,
			"observation_revision": credential.Capacity.Revision, "capacity_observed_at": credential.Capacity.ObservedAt,
			"window_duration_seconds": credential.Capacity.WindowSeconds, "resets_at": credential.Capacity.ResetsAt,
			"observation_expires_at": credential.Capacity.ExpiresAt, "observation_sha256": credential.Capacity.Digest,
			"effect_id": uuid.NewString(), "actor_id": scope.ActorID,
			"intent_sha256": intentDigest, "effect_payload": effectPayload, "updated_at": at,
		}).Scan(&providerSyncEffectID)
	})
}

func (repository *Repository) FailAuthorization(ctx context.Context, scope domainrepo.Scope, id, leaseID string, fence uint64, category string, at time.Time) error {
	state := "FAILED"
	if category == "DENIED" {
		state = "DENIED"
	}
	if category == "EXPIRED" {
		state = "EXPIRED"
	}
	return repository.managementTransact(ctx, scope, func(tx *transaction) error {
		var changed string
		return tx.tx.QueryRow(ctx, managementSQL("authorization__fail"), pgx.StrictNamedArgs{
			"authorization_id": id, "lease_id": leaseID, "lease_generation": fence,
			"state": state, "failure_category": category, "updated_at": at,
		}).Scan(&changed)
	})
}

func (repository *Repository) GetEffectResource(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect) (json.RawMessage, error) {
	var raw []byte
	err := repository.read(ctx, scope, func(tx pgx.Tx) error {
		switch effect.ResourceKind {
		case "provider_authorization":
			return tx.QueryRow(ctx, managementSQL("effect_resource__authorization"), pgx.StrictNamedArgs{"resource_id": effect.ResourceID, "tenant_id": scope.TenantID, "project_id": scope.ProjectID}).Scan(&raw)
		case "managed_provider_connection":
			return tx.QueryRow(ctx, managementSQL("effect_resource__connection"), pgx.StrictNamedArgs{"resource_id": effect.ResourceID, "tenant_id": scope.TenantID, "project_id": scope.ProjectID}).Scan(&raw)
		case "managed_provider_pool":
			return tx.QueryRow(ctx, managementSQL("effect_resource__pool"), pgx.StrictNamedArgs{"resource_id": effect.ResourceID, "tenant_id": scope.TenantID, "project_id": scope.ProjectID}).Scan(&raw)
		case "git_reconciliation":
			value, getErr := getGitReconciliationTx(ctx, tx, scope, effect.ResourceID)
			if getErr != nil {
				return getErr
			}
			raw, _, getErr = managementPayload(value)
			return getErr
		default:
			return errors.New("management effect resource kind is unsupported")
		}
	})
	return raw, err
}
