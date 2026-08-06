package controlplane

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/jackc/pgx/v5"
)

var (
	_ domainrepo.ProtectedTransaction = (*transaction)(nil)
	_ domainrepo.ProtectedRepository  = (*Repository)(nil)
)

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
	_, err := wrapped.tx.Exec(ctx, sqlRuntimeIncidentHistoryInsert, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID,
		"project_id":      wrapped.projectID,
		"incident_id":     entry.IncidentID,
		"version":         entry.Version,
		"owner_actor_id":  entry.OwnerActorID,
		"state":           entry.State,
		"action":          entry.Action,
		"reason_code":     entry.ReasonCode,
		"occurred_at":     entry.OccurredAt,
	})
	return mapError(err)
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
			if err := rows.Scan(&entry.IncidentID, &entry.Version, &entry.State, &entry.Action,
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
