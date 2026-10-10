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

const runtimeCandidateEligibilityDiagnosticMessage = "runtime candidate eligibility rejected"

const runtimeEligibilityReasonAttribute = "safe_reason"

const (
	runtimeReasonRuntimeContext                   = "RUNTIME_CONTEXT"
	runtimeReasonSessionResume                    = "SESSION_RESUME"
	runtimeReasonContinuationPreviousRead         = "CONTINUATION_PREVIOUS_READ"
	runtimeReasonContinuationPreviousJson         = "CONTINUATION_PREVIOUS_JSON"
	runtimeReasonContinuationPreviousDependencies = "CONTINUATION_PREVIOUS_DEPENDENCIES"
	runtimeReasonContinuationCurrentDependencies  = "CONTINUATION_CURRENT_DEPENDENCIES"
	runtimeReasonContinuationDiff                 = "CONTINUATION_DIFF"
	runtimeReasonContinuationSnapshot             = "CONTINUATION_SNAPSHOT"
	runtimeReasonContinuationTemplateRead         = "CONTINUATION_TEMPLATE_READ"
	runtimeReasonContinuationTemplateBytes        = "CONTINUATION_TEMPLATE_BYTES"
	runtimeReasonContinuationTemplateDigest       = "CONTINUATION_TEMPLATE_DIGEST"
	runtimeReasonContinuationDiffJson             = "CONTINUATION_DIFF_JSON"
	runtimeReasonContinuationBinding              = "CONTINUATION_BINDING"
	runtimeReasonContinuationWorkflowContext      = "CONTINUATION_WORKFLOW_CONTEXT"
	runtimeReasonContinuationMaterialization      = "CONTINUATION_MATERIALIZATION"
	runtimeReasonContinuationIncomplete           = "CONTINUATION_INCOMPLETE"
	runtimeReasonContinuationNoticeBytes          = "CONTINUATION_NOTICE_BYTES"
	runtimeReasonContinuationSessionContext       = "CONTINUATION_SESSION_CONTEXT"
	runtimeReasonContinuationTaskProjection       = "CONTINUATION_TASK_PROJECTION"
)

// Причина принадлежит закрытой диагностике producer; она не меняет error class
// или lifecycle и не переносит исходные данные в Error()/log.
type runtimeEligibilityDiagnosticError struct {
	reason string
	err    error
}

func (err runtimeEligibilityDiagnosticError) Error() string {
	return "runtime eligibility validation failed"
}
func (err runtimeEligibilityDiagnosticError) Unwrap() error { return err.err }

func runtimeEligibilityDiagnostic(reason string, err error) error {
	return runtimeEligibilityDiagnosticError{reason: reason, err: err}
}

func runtimeEligibilityReason(err error) string {
	var diagnostic runtimeEligibilityDiagnosticError
	if !errors.As(err, &diagnostic) {
		return "UNKNOWN"
	}
	switch diagnostic.reason {
	case runtimeReasonRuntimeContext, runtimeReasonSessionResume, runtimeReasonContinuationPreviousRead, runtimeReasonContinuationPreviousJson, runtimeReasonContinuationPreviousDependencies, runtimeReasonContinuationCurrentDependencies,
		runtimeReasonContinuationDiff, runtimeReasonContinuationSnapshot, runtimeReasonContinuationTemplateRead, runtimeReasonContinuationTemplateBytes, runtimeReasonContinuationTemplateDigest, runtimeReasonContinuationDiffJson,
		runtimeReasonContinuationBinding, runtimeReasonContinuationWorkflowContext, runtimeReasonContinuationMaterialization, runtimeReasonContinuationIncomplete, runtimeReasonContinuationNoticeBytes, runtimeReasonContinuationSessionContext, runtimeReasonContinuationTaskProjection:
		return diagnostic.reason
	default:
		return "UNKNOWN"
	}
}

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

func runtimeEligibilityErrorClass(err error) string {
	for _, candidate := range []struct {
		err   error
		class string
	}{
		{errs.ErrConflict, "CONFLICT"},
		{errs.ErrVersionMismatch, "VERSION_MISMATCH"},
		{errs.ErrNotFound, "NOT_FOUND"},
		{errs.ErrForbidden, "FORBIDDEN"},
		{errs.ErrCapabilityRequired, "CAPABILITY_REQUIRED"},
		{errs.ErrInvalid, "INVALID"},
	} {
		if errors.Is(err, candidate.err) {
			return candidate.class
		}
	}
	return "UNKNOWN"
}

func (repository *Repository) recordSystemAssistantTerminalTurn(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	sessionID, runID, content, state string,
) error {
	turnRef, err := newRef("trn")
	if err != nil {
		return err
	}
	var next int64
	if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectSessionsId, sessionID).Scan(&next); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertSessionTurnsRefSessionIdTurnNumber,
		turnRef, current.organizationID, sessionID, runID, next, content, state); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateAssistantConversationsVersionUpdatedAt, sessionID); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// Отказ кандидата закрывает весь принадлежащий владельцу граф. Независимые
// root run той же пачки сохраняют свои leases и продолжают исполняться.
func (repository *Repository) failRuntimeCandidateGraph(ctx context.Context, tx pgx.Tx, current scope, input command.Command, candidate claimableExecution) error {
	return repository.failRuntimeGraph(ctx, tx, current, input, candidate, "RUNTIME_INPUT_INVALID", runtimeClaimEligibilityChanged)
}

func (repository *Repository) failRuntimeGraph(ctx context.Context, tx pgx.Tx, current scope, input command.Command, candidate claimableExecution, code, summary string) error {
	if (code != "RUNTIME_INPUT_INVALID" || summary != runtimeClaimEligibilityChanged) && (code != "RUNTIME_TIMEOUT" || summary != runtimeTimeoutSummary) {
		return errs.ErrInvalid
	}
	rows, err := tx.Query(ctx, queryRuntimeClaimFailGraph, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "root_run_id": candidate.rootRunID,
		"failed_node_id": candidate.nodeID, "actor_id": current.actorID,
		"summary": summary, "safe_error_code": code,
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
			if err := repository.auditRuntimeClaimTransition(ctx, tx, current, input, candidate.projectID, item.ref, summary); err != nil {
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
			item.ref, eventKind, item.nodeRef, "", gateRef, "", summary, "FAILED", nodeState); err != nil {
			return err
		}
	}
	if candidate.stableKey == "system-assistant" {
		if err := repository.recordSystemAssistantTerminalTurn(ctx, tx, current,
			candidate.sessionID, candidate.runID, "i18n:RUNTIME_INPUT_INVALID", "FAILED"); err != nil {
			return err
		}
	}
	return repository.enqueueTerminalInteractionDeliveries(ctx, tx, current, candidate.projectID, candidate.rootRunID)
}
