package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed sql/runtime_claim__fail_graph.sql
var queryRuntimeClaimFailGraph string

const runtimeClaimEligibilityChanged = "i18n:RUNTIME_CLAIM_ELIGIBILITY_CHANGED"
const runtimeLeaseExpiredSummary = "i18n:RUNTIME_LEASE_EXPIRED"

// Даже expiry без новых claims является устойчивым переходом, а не idle poll.
func (repository *Repository) expireRuntimeClaimLeases(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (bool, error) {
	rows, err := tx.Query(ctx, queryRuntimeClaimexecutionExpireStaleLeases, current.organizationID)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	type expiredLease struct{ ref, projectID, runRef string }
	var leases []expiredLease
	for rows.Next() {
		var lease expiredLease
		if err := rows.Scan(&lease.ref, &lease.projectID, &lease.runRef); err != nil {
			rows.Close()
			return false, errs.ErrUnavailable
		}
		leases = append(leases, lease)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, errs.ErrUnavailable
	}
	for _, lease := range leases {
		if err := repository.auditRuntimeClaimTransition(ctx, tx, current, input, lease.projectID, lease.runRef, runtimeLeaseExpiredSummary); err != nil {
			return false, err
		}
	}
	return len(leases) > 0, nil
}

func (repository *Repository) auditRuntimeClaimTransition(ctx context.Context, tx pgx.Tx, current scope, input command.Command, projectID, runRef, summary string) error {
	ref, err := newRef("aud")
	if err != nil {
		return err
	}
	var project any
	if projectID != "" {
		project = projectID
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, ref, current.organizationID, project, current.actorID, input.Mutation.Operation, "RUN", runRef, summary, input.Principal.CorrelationRef); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func runtimeCandidateEligibilityFailure(err error) bool {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errs.ErrUnavailable) {
		return false
	}
	return errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrVersionMismatch) || errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrCapabilityRequired) || errors.Is(err, errs.ErrInvalid)
}

// Отказ кандидата закрывает весь принадлежащий владельцу граф. Независимые
// root run той же пачки сохраняют свои leases и продолжают исполняться.
func (repository *Repository) failRuntimeCandidateGraph(ctx context.Context, tx pgx.Tx, current scope, input command.Command, candidate claimableExecution) error {
	rows, err := tx.Query(ctx, queryRuntimeClaimFailGraph, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "root_run_id": candidate.rootRunID,
		"failed_node_id": candidate.nodeID, "actor_id": current.actorID,
		"summary": runtimeClaimEligibilityChanged,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	type transition struct{ kind, ref, nodeRef, state string }
	var transitions []transition
	for rows.Next() {
		var item transition
		if err := rows.Scan(&item.kind, &item.ref, &item.nodeRef, &item.state); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		transitions = append(transitions, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errs.ErrUnavailable
	}
	for _, item := range transitions {
		if item.kind == "RUN" {
			if err := repository.auditRuntimeClaimTransition(ctx, tx, current, input, candidate.projectID, item.ref, runtimeClaimEligibilityChanged); err != nil {
				return err
			}
		}
		eventKind, gateRef, nodeState := "RUN_STATE_CHANGED", "", ""
		switch item.kind {
		case "NODE":
			eventKind, nodeState = "NODE_STATE_CHANGED", item.state
		case "GATE":
			eventKind, gateRef, nodeState = "OWNER_GATE_RESOLVED", item.ref, "CANCELLED"
		}
		if _, err := repository.emitRunEvent(ctx, tx, current, candidate.projectID, candidate.rootRunID,
			item.ref, eventKind, item.nodeRef, "", gateRef, "", runtimeClaimEligibilityChanged, "FAILED", nodeState); err != nil {
			return err
		}
	}
	return repository.enqueueTerminalInteractionDeliveries(ctx, tx, current, candidate.projectID, candidate.rootRunID)
}
