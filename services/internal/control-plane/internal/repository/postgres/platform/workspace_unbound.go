package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// queryWorkspaceUnboundReceipt reads an immutable stopped-before-BIND fact by exact retired lease.
// queryWorkspaceUnboundReceipt читает immutable stopped-before-BIND fact по exact retired lease.
//
//go:embed sql/workspace_unbound__receipt.sql
var queryWorkspaceUnboundReceipt string

// queryWorkspaceUnboundRetire atomically preserves the stopped fact and clears only never-bound intents.
// queryWorkspaceUnboundRetire атомарно сохраняет stopped fact и очищает лишь never-bound intents.
//
//go:embed sql/workspace_unbound__retire.sql
var queryWorkspaceUnboundRetire string

// queryWorkspaceUnboundPrepare stores the immutable no-BIND witness after every owner guard.
// queryWorkspaceUnboundPrepare сохраняет immutable no-BIND witness после всех owner guards.
//
//go:embed sql/workspace_unbound__prepare.sql
var queryWorkspaceUnboundPrepare string

// queryWorkspaceUnboundNative retires exactly one native intent while retaining existing history.
// queryWorkspaceUnboundNative закрывает ровно один native intent с сохранением существующей history.
//
//go:embed sql/workspace_unbound__native.sql
var queryWorkspaceUnboundNative string

// authorizeWorkspaceUnbound verifies a closed lease and its never-admitted storage intent, or exact
// historical receipt replay. It never grants BIND, changes a business outcome or accepts a writer ACK.
// authorizeWorkspaceUnbound проверяет closed lease и never-admitted storage intent либо exact
// historical receipt replay. BIND не разрешает, business outcome не меняет и writer ACK не принимает.
func authorizeWorkspaceUnbound(ctx context.Context, tx pgx.Tx, current scope, locked workspaceObservationLease, payload command.WorkspaceObservationInput) error {
	if len(payload.UnboundStop) == 0 {
		var stored string
		if err := tx.QueryRow(ctx, queryWorkspaceUnboundReceipt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_id": locked.leaseID}).Scan(&stored); errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrForbidden
		} else if err != nil {
			return errs.ErrUnavailable
		}
		record, err := runtimecontract.DecodeWorkspaceUnboundStop([]byte(stored))
		if err != nil || !record.Matches(locked.input) || payload.BlockReason != "" || payload.PodUID != "" || payload.PVCUID != "" || len(payload.Quiescence) != 0 {
			return errs.ErrForbidden
		}
		return nil
	}
	record, err := runtimecontract.DecodeWorkspaceUnboundStop(payload.UnboundStop)
	if err != nil || !record.Matches(locked.input) || payload.BlockReason != "" || payload.PodUID != "" || payload.PVCUID != "" || len(payload.Quiescence) != 0 {
		return errs.ErrInvalid
	}
	var stored string
	err = tx.QueryRow(ctx, queryWorkspaceUnboundReceipt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_id": locked.leaseID}).Scan(&stored)
	if err == nil {
		if stored == string(payload.UnboundStop) {
			return nil
		}
		return errs.ErrIdempotencyReuse
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnavailable
	}
	if locked.leaseState == "CLAIMED" || locked.active || locked.writerLeaseID != locked.leaseID || locked.storageGeneration != payload.WorkspaceGeneration ||
		locked.storageState != "WRITER_PENDING" || locked.ackRaw != "" || locked.joinRaw != "" || locked.blockReason != "" || locked.writerReleased {
		return errs.ErrForbidden
	}
	return nil
}

// observeWorkspaceUnbound uses one owner transaction to retire the closed intent and preserve its
// native predecessor. Initial empty native intent is removed only when no history or effects exist.
// observeWorkspaceUnbound в одной owner transaction закрывает closed intent и сохраняет native
// predecessor. Initial empty native intent удаляется лишь при отсутствии history и effects.
func observeWorkspaceUnbound(ctx context.Context, tx pgx.Tx, current scope, locked workspaceObservationLease, payload command.WorkspaceObservationInput) (commandOutcome, error) {
	if err := authorizeWorkspaceUnbound(ctx, tx, current, locked, payload); err != nil {
		return commandOutcome{}, err
	}
	var state, stored string
	receiptErr := tx.QueryRow(ctx, queryWorkspaceUnboundReceipt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_id": locked.leaseID}).Scan(&stored)
	if receiptErr == nil {
		return commandOutcome{result: command.Result{Runtime: map[string]any{"workspaceUnboundStop": stored, "workspaceCommittedPhase": runtimecontract.WorkspaceObserveUnboundStop}}, projectID: locked.projectID, projectRef: locked.input.ProjectRef, resourceKind: "RUN", resourceRef: locked.input.RunRef, summary: "i18n:SESSION_WORKSPACE_OBSERVED"}, nil
	}
	if !errors.Is(receiptErr, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrUnavailable
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "workspace_id": locked.storageID, "workspace_generation": locked.storageGeneration, "lease_id": locked.leaseID, "version": locked.storageVersion, "record": string(payload.UnboundStop)}
	if err := tx.QueryRow(ctx, queryWorkspaceUnboundPrepare, args).Scan(&state); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrConflict
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var affected int64
	if err := tx.QueryRow(ctx, queryWorkspaceUnboundNative, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_id": locked.leaseID}).Scan(&affected); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if affected != 1 {
		return commandOutcome{}, errs.ErrConflict
	}
	err := tx.QueryRow(ctx, queryWorkspaceUnboundRetire, pgx.StrictNamedArgs{"organization_id": current.organizationID, "workspace_id": locked.storageID, "workspace_generation": locked.storageGeneration, "lease_id": locked.leaseID, "version": locked.storageVersion, "record": string(payload.UnboundStop)}).Scan(&state, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrConflict
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{Runtime: map[string]any{"workspaceUnboundStop": stored, "workspaceCommittedPhase": runtimecontract.WorkspaceObserveUnboundStop, "workspaceStorageState": state, "workspaceCurrentTuple": true}}, projectID: locked.projectID, projectRef: locked.input.ProjectRef, resourceKind: "RUN", resourceRef: locked.input.RunRef, summary: "i18n:SESSION_WORKSPACE_OBSERVED"}, nil
}

// refreshWorkspaceUnboundReceipt preserves immutable stopped bytes while reporting no new authority.
// refreshWorkspaceUnboundReceipt сохраняет immutable stopped bytes без выдачи новой authority.
func refreshWorkspaceUnboundReceipt(ctx context.Context, tx pgx.Tx, current scope, locked workspaceObservationLease, result *command.Result) error {
	var stored string
	if err := tx.QueryRow(ctx, queryWorkspaceUnboundReceipt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_id": locked.leaseID}).Scan(&stored); err != nil {
		return errs.ErrUnavailable
	}
	if stored != stringMap(result.Runtime, "workspaceUnboundStop") {
		return errs.ErrConflict
	}
	result.Runtime = map[string]any{"workspaceUnboundStop": stored, "workspaceCommittedPhase": runtimecontract.WorkspaceObserveUnboundStop, "workspaceCurrentTuple": false}
	return nil
}
