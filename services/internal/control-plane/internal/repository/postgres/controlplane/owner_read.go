package controlplane

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

var _ domainrepo.OwnerReadTransaction = (*transaction)(nil)

func (wrapped *transaction) OwnerSnapshotFence(ctx context.Context) (string, error) {
	var snapshot string
	if err := wrapped.tx.QueryRow(ctx, sqlOwnerSnapshotFence, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
		"actor_id": wrapped.actorID,
	}).Scan(&snapshot); err != nil {
		return "", mapError(err)
	}
	if snapshot == "" || len(snapshot) > 2048 {
		return "", errs.ErrStateConflict
	}
	return snapshot, nil
}

func (wrapped *transaction) ListOwnerResources(
	ctx context.Context,
	filter query.ResourceFilter,
) ([]entity.Resource, error) {
	states := make([]string, 0, len(filter.States))
	for _, state := range filter.States {
		states = append(states, string(state))
	}
	rows, err := wrapped.tx.Query(ctx, sqlOwnerResourceList, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
		"actor_id": wrapped.actorID, "kind": string(filter.Kind), "parent_id": filter.ParentID,
		"backup_id": filter.BackupID,
		"states":    states, "after_id": filter.AfterID, "limit": filter.Limit,
	})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := make([]entity.Resource, 0, filter.Limit)
	for rows.Next() {
		item, scanErr := scanResource(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, mapError(rows.Err())
}

func (wrapped *transaction) ListRuntimeIncidentsSnapshot(
	ctx context.Context,
	filter query.RuntimeIncidentFilter,
) ([]domainrepo.RuntimeIncident, error) {
	rows, err := wrapped.tx.Query(ctx, sqlRuntimeIncidentList, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
		"actor_id": wrapped.actorID, "execution_id": filter.ExecutionID,
		"after_id": filter.AfterID, "limit": filter.Limit,
	})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := make([]domainrepo.RuntimeIncident, 0, filter.Limit)
	for rows.Next() {
		item, scanErr := scanRuntimeIncident(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, mapError(rows.Err())
}

func (wrapped *transaction) ListProtectedResourceHistorySnapshot(
	ctx context.Context,
	resourceID string,
	beforeVersion uint64,
	limit int,
) ([]domainrepo.ProtectedResourceHistory, error) {
	rows, err := wrapped.tx.Query(ctx, sqlProtectedHistoryList, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
		"actor_id": wrapped.actorID, "resource_id": resourceID,
		"before_version": beforeVersion, "limit": limit,
	})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := make([]domainrepo.ProtectedResourceHistory, 0, limit)
	for rows.Next() {
		item, scanErr := scanProtectedResourceHistory(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, mapError(rows.Err())
}

func (wrapped *transaction) ListRuntimeIncidentHistorySnapshot(
	ctx context.Context,
	incidentID string,
	beforeVersion uint64,
	limit int,
) ([]domainrepo.RuntimeIncidentHistory, error) {
	rows, err := wrapped.tx.Query(ctx, sqlRuntimeIncidentHistoryList, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
		"actor_id": wrapped.actorID, "incident_id": incidentID,
		"before_version": beforeVersion, "limit": limit,
	})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := make([]domainrepo.RuntimeIncidentHistory, 0, limit)
	for rows.Next() {
		var item domainrepo.RuntimeIncidentHistory
		if scanErr := rows.Scan(&item.IncidentID, &item.Version, &item.ExecutionFence, &item.State,
			&item.Action, &item.ReasonCode, &item.OccurredAt, &item.OwnerActorID); scanErr != nil {
			return nil, mapError(scanErr)
		}
		item.OccurredAt = item.OccurredAt.UTC()
		result = append(result, item)
	}
	return result, mapError(rows.Err())
}

func (wrapped *transaction) ListRunGraphNodesPage(
	ctx context.Context,
	processRunID, afterNodeType, afterNodeID string,
	limit int,
) ([]domainrepo.RunGraphNode, bool, error) {
	rows, err := wrapped.tx.Query(ctx, sqlRunGraphNodes, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
		"actor_id": wrapped.actorID, "process_run_id": processRunID,
		"graph_process_limit": 1001, "graph_node_limit": 1001, "graph_hard_limit": 1000,
		"after_node_type": afterNodeType,
		"after_node_id":   afterNodeID, "limit": limit + 1,
	})
	if err != nil {
		return nil, false, mapError(err)
	}
	defer rows.Close()
	result := make([]domainrepo.RunGraphNode, 0, limit+1)
	overflow := false
	for rows.Next() {
		var item domainrepo.RunGraphNode
		if scanErr := rows.Scan(&item.NodeType, &item.ID, &item.State, &item.ParentProcessRunID,
			&item.ProcessRunID, &item.SessionID, &item.TurnID, &item.RuntimeRevisionID,
			&item.PredecessorID, &item.SuccessorID, &item.Version, &item.RuntimeRevisionVersion,
			&item.Attempt, &item.OccurredAt, &item.UpdatedAt, &item.DisplayName, &overflow); scanErr != nil {
			return nil, false, mapError(scanErr)
		}
		item.OccurredAt, item.UpdatedAt = item.OccurredAt.UTC(), item.UpdatedAt.UTC()
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, mapError(err)
	}
	if overflow {
		return nil, false, errs.ErrStateConflict
	}
	truncated := len(result) > limit
	if truncated {
		result = result[:limit]
	}
	return result, truncated, nil
}

func (wrapped *transaction) ListRunTimelineAuditSnapshot(
	ctx context.Context,
	resourceIDs []string,
	afterOccurredAt time.Time,
	afterID string,
	limit int,
) ([]domainrepo.Audit, error) {
	rows, err := wrapped.tx.Query(ctx, sqlAuditRunTimeline, pgx.StrictNamedArgs{
		"organization_id": wrapped.organizationID, "project_id": wrapped.projectID,
		"resource_ids": resourceIDs, "has_after": afterID != "",
		"after_occurred_at": afterOccurredAt, "after_id": afterID, "limit": limit,
	})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := make([]domainrepo.Audit, 0, limit)
	for rows.Next() {
		var item domainrepo.Audit
		if scanErr := rows.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.ActorID,
			&item.Action, &item.ResourceID, &item.ResourceKind, &item.ResourceVersion,
			&item.Outcome, &item.CorrelationID, &item.PolicyRevision, &item.OccurredAt); scanErr != nil {
			return nil, mapError(scanErr)
		}
		result = append(result, item)
	}
	return result, mapError(rows.Err())
}
