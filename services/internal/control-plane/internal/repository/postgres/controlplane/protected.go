package controlplane

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	_ domainrepo.ProtectedTransaction = (*transaction)(nil)
	_ domainrepo.ProtectedRepository  = (*Repository)(nil)
)

func (repository *Repository) GetLegacyConfigurationCutover(
	ctx context.Context,
	organizationID, projectID, actorID, legacyRoleID string,
) (domainrepo.LegacyConfigurationCutover, error) {
	var result domainrepo.LegacyConfigurationCutover
	err := repository.read(ctx, organizationID, projectID, actorID, func(tx pgx.Tx) error {
		var scanErr error
		result, scanErr = scanLegacyConfigurationCutover(tx.QueryRow(ctx, sqlLegacyConfigurationCutoverGet,
			pgx.StrictNamedArgs{"organization_id": organizationID, "project_id": projectID,
				"actor_id": actorID, "legacy_role_id": legacyRoleID}))
		return scanErr
	})
	return result, err
}

func (repository *Repository) ListLegacyConfigurationCutovers(
	ctx context.Context,
	organizationID, projectID, actorID, afterLegacyRoleID string,
	limit int,
) ([]domainrepo.LegacyConfigurationCutover, error) {
	var result []domainrepo.LegacyConfigurationCutover
	err := repository.read(ctx, organizationID, projectID, actorID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlLegacyConfigurationCutoverList, pgx.StrictNamedArgs{
			"organization_id": organizationID, "project_id": projectID, "actor_id": actorID,
			"after_legacy_role_id": afterLegacyRoleID, "limit": limit})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, scanErr := scanLegacyConfigurationCutover(rows)
			if scanErr != nil {
				return scanErr
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, err
}

func (wrapped *transaction) GetLegacyConfigurationCutoverForUpdate(
	ctx context.Context,
	legacyRoleID string,
) (domainrepo.LegacyConfigurationCutover, error) {
	return scanLegacyConfigurationCutover(wrapped.tx.QueryRow(ctx, sqlLegacyConfigurationCutoverGetForUpdate,
		pgx.StrictNamedArgs{"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
			"actor_id": wrapped.actorID, "legacy_role_id": legacyRoleID}))
}

func (wrapped *transaction) MarkLegacyConfigurationCutoverMigrated(
	ctx context.Context,
	cutover domainrepo.LegacyConfigurationCutover,
) error {
	tag, err := wrapped.tx.Exec(ctx, sqlLegacyConfigurationCutoverMarkMigrated, pgx.StrictNamedArgs{
		"organization_id": cutover.OrganizationID, "project_id": cutover.ProjectID,
		"actor_id": cutover.OwnerActorID, "legacy_role_id": cutover.LegacyRoleID,
		"result_agent_version": cutover.ResultAgentVersion,
		"result_agent_sha256":  cutover.ResultAgentSHA256, "resolved_at": cutover.ResolvedAt})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func scanLegacyConfigurationCutover(row rowScanner) (domainrepo.LegacyConfigurationCutover, error) {
	var result domainrepo.LegacyConfigurationCutover
	var resolved pgtype.Timestamptz
	err := row.Scan(&result.OrganizationID, &result.ProjectID, &result.OwnerActorID,
		&result.LegacyRoleID, &result.LegacyRoleVersion, &result.LegacyPromptProfileID,
		&result.LegacyPromptVersion, &result.SourceRoleSHA256, &result.SourcePromptSHA256,
		&result.SourceCredentialIDs, &result.TargetRoleDefinitionID, &result.TargetAgentID,
		&result.TargetInstructionSetID, &result.TargetProviderPoolID, &result.TargetAgentAssignmentID,
		&result.TargetProviderReferenceIDs,
		&result.State, &result.BlockCode, &result.ManualAction, &result.ResultAgentVersion,
		&result.ResultAgentSHA256, &result.CreatedAt, &resolved)
	if err != nil {
		return domainrepo.LegacyConfigurationCutover{}, mapError(err)
	}
	result.CreatedAt = result.CreatedAt.UTC()
	if resolved.Valid {
		result.ResolvedAt = resolved.Time.UTC()
	}
	return result, nil
}

func (wrapped *transaction) GetByStableKeyForUpdate(
	ctx context.Context,
	organizationID, projectID string,
	kind enum.Kind,
	stableKey string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(ctx, sqlResourceGetByStableKeyForUpdate, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"kind":            string(kind),
		"stable_key":      stableKey,
	}))
}

func (wrapped *transaction) GetByNameForUpdate(
	ctx context.Context,
	organizationID, projectID string,
	kind enum.Kind,
	name string,
) (entity.Resource, error) {
	return scanResource(wrapped.tx.QueryRow(ctx, sqlResourceGetByNameForUpdate, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"project_id":      projectID,
		"kind":            string(kind),
		"name":            name,
	}))
}

func (wrapped *transaction) ReserveExternalCommandReceipt(
	ctx context.Context,
	receipt domainrepo.ExternalCommandReceipt,
) (domainrepo.ExternalCommandReceipt, bool, error) {
	tag, err := wrapped.tx.Exec(ctx, sqlExternalCommandReceiptReserve, pgx.StrictNamedArgs{
		"issuer": receipt.Issuer, "purpose": receipt.Purpose, "receipt_id": receipt.ReceiptID,
		"organization_id": receipt.OrganizationID, "project_id": receipt.ProjectID,
		"owner_actor_id": receipt.OwnerActorID, "target_kind": receipt.TargetKind,
		"target_resource_id": receipt.TargetResourceID, "target_stable_key": receipt.TargetStableKey,
		"action": receipt.Action, "effect": receipt.Effect, "effect_generation": receipt.EffectGeneration,
		"effect_sha256": receipt.EffectSHA256, "command_intent_sha256": receipt.CommandIntentSHA256,
		"authority_sha256": receipt.AuthoritySHA256, "consumed_at": receipt.ConsumedAt,
	})
	if err != nil {
		return domainrepo.ExternalCommandReceipt{}, false, mapError(err)
	}
	stored, err := scanExternalCommandReceipt(wrapped.tx.QueryRow(ctx, sqlExternalCommandReceiptGet,
		pgx.StrictNamedArgs{"issuer": receipt.Issuer, "purpose": receipt.Purpose, "receipt_id": receipt.ReceiptID}))
	if err != nil {
		return domainrepo.ExternalCommandReceipt{}, false, err
	}
	return stored, tag.RowsAffected() == 1, nil
}

func (wrapped *transaction) GetExternalCommandReceipt(
	ctx context.Context,
	issuer, purpose, receiptID string,
) (domainrepo.ExternalCommandReceipt, error) {
	return scanExternalCommandReceipt(wrapped.tx.QueryRow(ctx, sqlExternalCommandReceiptGet,
		pgx.StrictNamedArgs{"issuer": issuer, "purpose": purpose, "receipt_id": receiptID}))
}

func (wrapped *transaction) FinalizeExternalCommandReceipt(
	ctx context.Context,
	receipt domainrepo.ExternalCommandReceipt,
) error {
	digest, digestErr := entity.ProjectionSHA256(receipt.Result)
	if digestErr != nil || receipt.Result.ID != receipt.ResultResourceID ||
		receipt.Result.Version != receipt.ResultVersion || digest != receipt.ResultSHA256 {
		return errs.ErrInternal
	}
	snapshot, err := marshalResource(receipt.Result)
	if err != nil {
		return errs.ErrInternal
	}
	tag, err := wrapped.tx.Exec(ctx, sqlExternalCommandReceiptFinalize, pgx.StrictNamedArgs{
		"issuer": receipt.Issuer, "purpose": receipt.Purpose, "receipt_id": receipt.ReceiptID,
		"command_intent_sha256": receipt.CommandIntentSHA256,
		"result_resource_id":    receipt.ResultResourceID, "result_version": receipt.ResultVersion,
		"result_sha256": receipt.ResultSHA256, "result_snapshot": string(snapshot),
	})
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func scanExternalCommandReceipt(row rowScanner) (domainrepo.ExternalCommandReceipt, error) {
	var result domainrepo.ExternalCommandReceipt
	var snapshot []byte
	err := row.Scan(&result.Issuer, &result.Purpose, &result.ReceiptID,
		&result.OrganizationID, &result.ProjectID, &result.OwnerActorID,
		&result.TargetKind, &result.TargetResourceID, &result.TargetStableKey,
		&result.Action, &result.Effect, &result.EffectGeneration, &result.EffectSHA256,
		&result.CommandIntentSHA256, &result.AuthoritySHA256,
		&result.ResultResourceID, &result.ResultVersion, &result.ResultSHA256, &snapshot, &result.ConsumedAt)
	if err != nil {
		return domainrepo.ExternalCommandReceipt{}, mapError(err)
	}
	if len(snapshot) != 0 {
		result.Result, err = unmarshalResource(snapshot)
		if err != nil {
			return domainrepo.ExternalCommandReceipt{}, errs.ErrInternal
		}
	}
	result.ConsumedAt = result.ConsumedAt.UTC()
	return result, nil
}

func (wrapped *transaction) AppendProtectedResourceHistory(
	ctx context.Context,
	entry domainrepo.ProtectedResourceHistory,
) error {
	snapshot, err := marshalResource(entry.Resource)
	if err != nil {
		return errs.ErrInternal
	}
	digest, err := entity.ProjectionSHA256(entry.Resource)
	if err != nil || digest != entry.SnapshotSHA256 {
		return errs.ErrInternal
	}
	_, err = wrapped.tx.Exec(ctx, sqlProtectedHistoryInsert, pgx.StrictNamedArgs{
		"organization_id":  entry.Resource.OrganizationID,
		"project_id":       entry.Resource.ProjectID,
		"resource_id":      entry.Resource.ID,
		"resource_version": entry.Resource.Version,
		"resource_kind":    string(entry.Resource.Kind),
		"owner_actor_id":   entry.Resource.OwnerActorID,
		"action":           entry.Action,
		"snapshot":         string(snapshot),
		"snapshot_sha256":  entry.SnapshotSHA256,
		"occurred_at":      entry.OccurredAt,
	})
	return mapError(err)
}

func (wrapped *transaction) GetProtectedResourceHistoryVersion(
	ctx context.Context,
	resourceID string,
	resourceVersion uint64,
) (domainrepo.ProtectedResourceHistory, error) {
	return scanProtectedResourceHistory(wrapped.tx.QueryRow(
		ctx,
		sqlProtectedHistoryGetVersion,
		pgx.StrictNamedArgs{
			"organization_id":  wrapped.organizationID,
			"project_id":       wrapped.projectID,
			"resource_id":      resourceID,
			"resource_version": resourceVersion,
		},
	))
}

func (wrapped *transaction) GetInstructionHistoryContentVersion(
	ctx context.Context,
	resourceID string,
	contentVersion uint64,
) (domainrepo.ProtectedResourceHistory, error) {
	return scanProtectedResourceHistory(wrapped.tx.QueryRow(
		ctx,
		sqlInstructionHistoryGetContentVersion,
		pgx.StrictNamedArgs{
			"organization_id": wrapped.organizationID,
			"project_id":      wrapped.projectID,
			"resource_id":     resourceID,
			"content_version": contentVersion,
		},
	))
}

func (repository *Repository) ListProtectedResourceHistory(
	ctx context.Context,
	organizationID, projectID, actorID, resourceID string,
	beforeVersion uint64,
	limit int,
) ([]domainrepo.ProtectedResourceHistory, error) {
	var result []domainrepo.ProtectedResourceHistory
	err := repository.read(ctx, organizationID, projectID, actorID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlProtectedHistoryList, pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"actor_id":        actorID,
			"resource_id":     resourceID,
			"before_version":  beforeVersion,
			"limit":           limit,
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			entry, scanErr := scanProtectedResourceHistory(rows)
			if scanErr != nil {
				return scanErr
			}
			result = append(result, entry)
		}
		return rows.Err()
	})
	return result, err
}

func (wrapped *transaction) GetRuntimeIncidentForUpdate(
	ctx context.Context,
	incidentID string,
) (domainrepo.RuntimeIncident, error) {
	return scanRuntimeIncident(wrapped.tx.QueryRow(ctx, sqlRuntimeIncidentGetForUpdate, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID,
		"project_id":      wrapped.projectID,
		"incident_id":     incidentID,
	}))
}

func (wrapped *transaction) UpdateRuntimeIncident(
	ctx context.Context,
	incident domainrepo.RuntimeIncident,
	expectedVersion uint64,
) error {
	commandTag, err := wrapped.tx.Exec(ctx, sqlRuntimeIncidentUpdate, pgx.StrictNamedArgs{
		"organization_id":  incident.OrganizationID,
		"project_id":       incident.ProjectID,
		"incident_id":      incident.ID,
		"version":          incident.Version,
		"execution_fence":  incident.ExecutionFence,
		"state":            incident.State,
		"reason_code":      incident.ReasonCode,
		"updated_at":       incident.UpdatedAt,
		"expected_version": expectedVersion,
	})
	if err != nil {
		return mapError(err)
	}
	if commandTag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) AppendRuntimeIncidentHistory(
	ctx context.Context,
	entry domainrepo.RuntimeIncidentHistory,
) error {
	commandTag, err := wrapped.tx.Exec(ctx, sqlRuntimeIncidentHistoryInsert, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID,
		"project_id":      wrapped.projectID,
		"incident_id":     entry.IncidentID,
		"version":         entry.Version,
		"execution_fence": entry.ExecutionFence,
		"owner_actor_id":  entry.OwnerActorID,
		"state":           entry.State,
		"action":          entry.Action,
		"reason_code":     entry.ReasonCode,
		"occurred_at":     entry.OccurredAt,
	})
	if err != nil {
		return mapError(err)
	}
	if commandTag.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (repository *Repository) GetRuntimeIncident(
	ctx context.Context,
	organizationID, projectID, actorID, incidentID string,
) (domainrepo.RuntimeIncident, error) {
	var result domainrepo.RuntimeIncident
	err := repository.read(ctx, organizationID, projectID, actorID, func(tx pgx.Tx) error {
		var scanErr error
		result, scanErr = scanRuntimeIncident(tx.QueryRow(ctx, sqlRuntimeIncidentOwnerGet, pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"actor_id":        actorID,
			"incident_id":     incidentID,
		}))
		return scanErr
	})
	return result, err
}

func (repository *Repository) ListRuntimeIncidentHistory(
	ctx context.Context,
	organizationID, projectID, actorID, incidentID string,
	beforeVersion uint64,
	limit int,
) ([]domainrepo.RuntimeIncidentHistory, error) {
	var result []domainrepo.RuntimeIncidentHistory
	err := repository.read(ctx, organizationID, projectID, actorID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sqlRuntimeIncidentHistoryList, pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"project_id":      projectID,
			"actor_id":        actorID,
			"incident_id":     incidentID,
			"before_version":  beforeVersion,
			"limit":           limit,
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var entry domainrepo.RuntimeIncidentHistory
			if err := rows.Scan(&entry.IncidentID, &entry.Version, &entry.ExecutionFence, &entry.State, &entry.Action,
				&entry.ReasonCode, &entry.OccurredAt, &entry.OwnerActorID); err != nil {
				return err
			}
			entry.OccurredAt = entry.OccurredAt.UTC()
			result = append(result, entry)
		}
		return rows.Err()
	})
	return result, err
}

func scanProtectedResourceHistory(row rowScanner) (domainrepo.ProtectedResourceHistory, error) {
	var snapshot []byte
	var result domainrepo.ProtectedResourceHistory
	if err := row.Scan(&snapshot, &result.Action, &result.SnapshotSHA256, &result.OccurredAt); err != nil {
		return domainrepo.ProtectedResourceHistory{}, mapError(err)
	}
	resource, err := unmarshalResource(snapshot)
	if err != nil {
		return domainrepo.ProtectedResourceHistory{}, errs.ErrInternal
	}
	digest, err := entity.ProjectionSHA256(resource)
	if err != nil || digest != result.SnapshotSHA256 {
		return domainrepo.ProtectedResourceHistory{}, errs.ErrInternal
	}
	result.Resource = resource
	result.OccurredAt = result.OccurredAt.UTC()
	return result, nil
}

func scanRuntimeIncident(row rowScanner) (domainrepo.RuntimeIncident, error) {
	var incident domainrepo.RuntimeIncident
	if err := row.Scan(
		&incident.ID, &incident.OrganizationID, &incident.ProjectID,
		&incident.ExecutionID, &incident.ExecutionFence, &incident.Kind,
		&incident.EvidenceSHA256, &incident.WorkloadID, &incident.OccurredAt,
		&incident.Version, &incident.State, &incident.ReasonCode, &incident.UpdatedAt,
	); err != nil {
		return domainrepo.RuntimeIncident{}, mapError(err)
	}
	if incident.Version == 0 || incident.OccurredAt.IsZero() || incident.UpdatedAt.Before(incident.OccurredAt) {
		return domainrepo.RuntimeIncident{}, errs.ErrInternal
	}
	switch incident.State {
	case "OPEN", "ACKNOWLEDGED", "RETRYING", "RELEASED", "CLOSED":
	default:
		return domainrepo.RuntimeIncident{}, errs.ErrInternal
	}
	incident.OccurredAt = incident.OccurredAt.UTC()
	incident.UpdatedAt = incident.UpdatedAt.UTC()
	return incident, nil
}
