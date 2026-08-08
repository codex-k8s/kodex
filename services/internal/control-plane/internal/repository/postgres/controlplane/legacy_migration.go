package controlplane

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/jackc/pgx/v5"
)

var _ domainrepo.LegacyGraphMigrationTransaction = (*transaction)(nil)

func (wrapped *transaction) InsertLegacyGraphPlan(
	ctx context.Context,
	plan domainrepo.LegacyGraphPlanRecord,
) error {
	result, err := wrapped.tx.Exec(ctx, sqlLegacyGraphPlanInsert, pgx.StrictNamedArgs{
		"plan_id": plan.PlanID, "organization_id": plan.OrganizationID,
		"owner_actor_id": plan.OwnerActorID, "source_root_reference": plan.SourceRootReference,
		"source_root_sha256": plan.SourceRootSHA256, "source_snapshot_sha256": plan.SourceSnapshotSHA256,
		"idempotency_key_sha256": plan.IdempotencyKeySHA256, "request_sha256": plan.RequestSHA256,
		"semantic_sha256": plan.SemanticSHA256, "project_id": plan.ProjectID,
		"operation_count": plan.OperationCount, "archived_source_count": plan.ArchivedSourceCount,
		"plan_payload": plan.Payload, "prepared_at": plan.PreparedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if result.RowsAffected() != 1 {
		return errs.ErrAborted
	}
	return nil
}

func (wrapped *transaction) GetLegacyGraphPlanForUpdate(
	ctx context.Context,
	planID string,
) (domainrepo.LegacyGraphPlanRecord, error) {
	var plan domainrepo.LegacyGraphPlanRecord
	err := wrapped.tx.QueryRow(ctx, sqlLegacyGraphPlanGetForUpdate, pgx.StrictNamedArgs{
		"plan_id": planID,
	}).Scan(
		&plan.PlanID, &plan.OrganizationID, &plan.OwnerActorID,
		&plan.SourceRootReference, &plan.SourceRootSHA256, &plan.SourceSnapshotSHA256,
		&plan.IdempotencyKeySHA256, &plan.RequestSHA256, &plan.SemanticSHA256,
		&plan.ProjectID, &plan.State, &plan.VerificationState, &plan.Payload,
		&plan.OperationCount, &plan.ArchivedSourceCount, &plan.PreparedAt, &plan.TerminalAt,
	)
	if err != nil {
		return domainrepo.LegacyGraphPlanRecord{}, mapError(err)
	}
	return plan, nil
}

func (wrapped *transaction) InsertLegacySourceDisposition(
	ctx context.Context,
	disposition domainrepo.LegacySourceDispositionRecord,
) error {
	result, err := wrapped.tx.Exec(ctx, sqlLegacyGraphSourceInsert, pgx.StrictNamedArgs{
		"plan_id": disposition.PlanID, "source_table": disposition.SourceTable,
		"disposition": disposition.Disposition, "row_count": disposition.RowCount,
		"source_sha256":         disposition.SourceSHA256,
		"terminal_state_sha256": disposition.TerminalStateSHA256,
	})
	if err != nil {
		return mapError(err)
	}
	if result.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) ListLegacySourceDispositions(
	ctx context.Context,
	planID string,
) ([]domainrepo.LegacySourceDispositionRecord, error) {
	rows, err := wrapped.tx.Query(ctx, sqlLegacyGraphSourceList, pgx.StrictNamedArgs{"plan_id": planID})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := make([]domainrepo.LegacySourceDispositionRecord, 0, 50)
	for rows.Next() {
		var disposition domainrepo.LegacySourceDispositionRecord
		if err := rows.Scan(
			&disposition.PlanID,
			&disposition.SourceTable,
			&disposition.Disposition,
			&disposition.RowCount,
			&disposition.SourceSHA256,
			&disposition.TerminalStateSHA256,
		); err != nil {
			return nil, mapError(err)
		}
		result = append(result, disposition)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return result, nil
}

func (wrapped *transaction) InsertLegacyOperationIntent(
	ctx context.Context,
	operation domainrepo.LegacyOperationRecord,
) error {
	result, err := wrapped.tx.Exec(ctx, sqlLegacyGraphOperationInsert, pgx.StrictNamedArgs{
		"plan_id": operation.PlanID, "ordinal": operation.Ordinal,
		"operation_kind": operation.OperationKind, "input_sha256": operation.InputSHA256,
		"target_id": operation.TargetID, "target_kind": operation.TargetKind,
		"provenance_sha256": operation.ProvenanceSHA256,
	})
	if err != nil {
		return mapError(err)
	}
	if result.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) ListLegacyOperationReceipts(
	ctx context.Context,
	planID string,
) ([]domainrepo.LegacyOperationRecord, error) {
	rows, err := wrapped.tx.Query(ctx, sqlLegacyGraphOperationList, pgx.StrictNamedArgs{"plan_id": planID})
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := make([]domainrepo.LegacyOperationRecord, 0)
	for rows.Next() {
		var operation domainrepo.LegacyOperationRecord
		var eventSequences []int64
		var targetState string
		if err := rows.Scan(
			&operation.PlanID, &operation.Ordinal, &operation.OperationKind,
			&operation.InputSHA256, &operation.TargetID, &operation.TargetKind,
			&operation.TargetVersion, &targetState, &operation.ProjectionSHA256,
			&operation.ProvenanceSHA256, &operation.ProvenanceEvidenceSHA256,
			&operation.AuditIDs, &operation.EventIDs,
			&eventSequences, &operation.MaterializedAt,
		); err != nil {
			return nil, mapError(err)
		}
		operation.TargetState = enum.State(targetState)
		operation.EventSequences = make([]uint64, len(eventSequences))
		for index, sequence := range eventSequences {
			if sequence < 0 {
				return nil, errs.ErrInternal
			}
			operation.EventSequences[index] = uint64(sequence)
		}
		result = append(result, operation)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return result, nil
}

func (wrapped *transaction) MaterializeLegacyOperationReceipt(
	ctx context.Context,
	operation domainrepo.LegacyOperationRecord,
) error {
	sequences := make([]int64, len(operation.EventSequences))
	for index, sequence := range operation.EventSequences {
		sequences[index] = int64(sequence)
	}
	result, err := wrapped.tx.Exec(ctx, sqlLegacyGraphOperationMaterialize, pgx.StrictNamedArgs{
		"plan_id": operation.PlanID, "ordinal": operation.Ordinal,
		"operation_kind": operation.OperationKind, "input_sha256": operation.InputSHA256,
		"target_id": operation.TargetID, "target_kind": operation.TargetKind,
		"target_version": operation.TargetVersion, "target_state": string(operation.TargetState),
		"projection_sha256":          operation.ProjectionSHA256,
		"provenance_sha256":          operation.ProvenanceSHA256,
		"provenance_evidence_sha256": operation.ProvenanceEvidenceSHA256,
		"audit_ids":                  operation.AuditIDs, "event_ids": operation.EventIDs,
		"event_sequences": sequences, "materialized_at": operation.MaterializedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if result.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) SetLegacyGraphPlanTerminal(
	ctx context.Context,
	planID, state, verificationState string,
	terminalAt time.Time,
) error {
	result, err := wrapped.tx.Exec(ctx, sqlLegacyGraphPlanTerminal, pgx.StrictNamedArgs{
		"plan_id": planID, "state": state, "verification_state": verificationState,
		"terminal_at": terminalAt,
	})
	if err != nil {
		return mapError(err)
	}
	if result.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) AppendLegacyProvenance(
	ctx context.Context,
	provenance domainrepo.LegacyProvenanceRecord,
) error {
	result, err := wrapped.tx.Exec(ctx, sqlLegacyGraphProvenanceInsert, pgx.StrictNamedArgs{
		"plan_id": provenance.PlanID, "ordinal": provenance.Ordinal,
		"target_id": provenance.TargetID, "target_kind": provenance.TargetKind,
		"source_table": provenance.SourceTable, "source_ref": provenance.SourceRef,
		"source_revision": provenance.SourceRevision, "source_sha256": provenance.SourceSHA256,
		"immutable_input_sha256": provenance.ImmutableInputSHA256,
		"root_actor_id":          provenance.RootActorID, "root_session_id": provenance.RootSessionID,
		"root_turn_id": provenance.RootTurnID, "root_attempt": provenance.RootAttempt,
		"runtime_revision_id":         provenance.RuntimeRevisionID,
		"runtime_revision_version":    provenance.RuntimeRevisionVersion,
		"parent_target_id":            provenance.ParentTargetID,
		"launching_turn_id":           provenance.LaunchingTurnID,
		"launching_attempt":           provenance.LaunchingAttempt,
		"launching_attempt_target_id": provenance.LaunchingAttemptTargetID,
		"machine_policy_revision":     provenance.MachinePolicyRevision,
		"machine_policy_sha256":       provenance.MachinePolicySHA256,
		"legacy_policy_revision":      provenance.LegacyPolicyRevision,
		"legacy_policy_sha256":        provenance.LegacyPolicySHA256,
		"lineage_sha256":              provenance.LineageSHA256,
	})
	if err != nil {
		return mapError(err)
	}
	if result.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) GetLegacyProvenanceProjection(
	ctx context.Context,
	planID string,
	ordinal uint32,
) (string, error) {
	var projection string
	if err := wrapped.tx.QueryRow(ctx, sqlLegacyGraphProvenanceProjection, pgx.StrictNamedArgs{
		"plan_id": planID,
		"ordinal": ordinal,
	}).Scan(&projection); err != nil {
		return "", mapError(err)
	}
	if projection == "" {
		return "", errs.ErrDataLoss
	}
	return projection, nil
}

func (wrapped *transaction) SaveLegacyCallbackManifest(
	ctx context.Context,
	manifest domainrepo.LegacyCallbackManifest,
) error {
	result, err := wrapped.tx.Exec(ctx, sqlLegacyGraphCallbackManifestInsert, pgx.StrictNamedArgs{
		"id": manifest.ID, "plan_id": manifest.PlanID, "delegation_id": manifest.DelegationID,
		"callback_process_id": manifest.CallbackProcessID, "destinations": manifest.Destinations,
		"manifest_sha256": manifest.ManifestSHA256, "created_at": manifest.CreatedAt,
	})
	if err != nil {
		return mapError(err)
	}
	if result.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) SaveLegacyCallbackDelivery(
	ctx context.Context,
	delivery domainrepo.LegacyCallbackDelivery,
) error {
	result, err := wrapped.tx.Exec(ctx, sqlLegacyGraphCallbackDeliveryInsert, pgx.StrictNamedArgs{
		"id": delivery.ID, "plan_id": delivery.PlanID, "manifest_id": delivery.ManifestID,
		"destination": delivery.Destination, "receipt_sha256": delivery.ReceiptSHA256,
		"state": delivery.State, "delivered_at": delivery.DeliveredAt,
	})
	if err != nil {
		return mapError(err)
	}
	if result.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) SaveLegacyTurnAttempt(
	ctx context.Context,
	attempt domainrepo.TurnAttempt,
	runtimeRevisionID string,
	runtimeRevisionVersion uint64,
) error {
	var finishedAt any
	if !attempt.FinishedAt.IsZero() {
		finishedAt = attempt.FinishedAt
	}
	result, err := wrapped.tx.Exec(ctx, sqlLegacyGraphTurnAttemptInsert, pgx.StrictNamedArgs{
		"turn_id": attempt.TurnID, "attempt": attempt.Attempt,
		"workload_id": attempt.WorkloadID, "authority_generation": attempt.AuthorityGeneration,
		"state": attempt.State, "input_sha256": attempt.InputSHA256,
		"lease_fence": attempt.LeaseFence, "started_at": attempt.StartedAt,
		"finished_at": finishedAt, "outcome": attempt.Outcome,
		"runtime_revision_id":      runtimeRevisionID,
		"runtime_revision_version": runtimeRevisionVersion,
	})
	if err != nil {
		return mapError(err)
	}
	if result.RowsAffected() != 1 {
		return errs.ErrStateConflict
	}
	return nil
}

func (wrapped *transaction) GetLegacyCustomOperationProjection(
	ctx context.Context,
	planID string,
	ordinal uint32,
) (string, error) {
	var projection string
	if err := wrapped.tx.QueryRow(ctx, sqlLegacyGraphOperationCustomProjection, pgx.StrictNamedArgs{
		"plan_id": planID,
		"ordinal": ordinal,
	}).Scan(&projection); err != nil {
		return "", mapError(err)
	}
	if projection == "" {
		return "", errs.ErrDataLoss
	}
	return projection, nil
}

func (wrapped *transaction) VerifyLegacyOperationEvidence(
	ctx context.Context,
	operation domainrepo.LegacyOperationRecord,
) (bool, error) {
	var valid bool
	if err := wrapped.tx.QueryRow(ctx, sqlLegacyGraphOperationVerify, pgx.StrictNamedArgs{
		"plan_id": operation.PlanID, "ordinal": operation.Ordinal,
	}).Scan(&valid); err != nil {
		return false, mapError(err)
	}
	return valid, nil
}
