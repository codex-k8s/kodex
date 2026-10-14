package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/provider_financial_receipt.sql
var queryProviderFinancialReceipt string

// Semantic callback не зависит от transport correlation. Все текущие owner
// guards выполняются до чтения receipt, поэтому replay не сохраняет старое право.
func (repository *Repository) bindFinancialCallbackReceipt(ctx context.Context, tx pgx.Tx, current scope, input *command.Command) error {
	if input.Kind != command.ReportExecutionProgress {
		return nil
	}
	payload, ok := input.Payload.(command.LeaseInput)
	if !ok || payload.Financial == nil {
		return nil
	}
	observation := payload.Financial.Observation
	if payload.Message != nil || payload.Progress != "" || observation.Validate() != nil {
		return errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, current, payload, true)
	if err != nil {
		return err
	}
	var rootID, rootState string
	if tx.QueryRow(ctx, queryRuntimeCompleteexecutionLockRootRun, lease["rootRunID"]).Scan(&rootID, &rootState) != nil {
		return errs.ErrUnavailable
	}
	if rootID != stringMap(lease, "rootRunID") || contains([]string{"SUCCEEDED", "FAILED", "CANCELLED"}, rootState) {
		return errs.ErrForbidden
	}
	if _, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, stringMap(lease, "runID")); err != nil {
		return err
	} else if expired {
		return errs.ErrForbidden
	}
	var created time.Time
	var previousCreated *time.Time
	var previousLease string
	var previousSequence int64
	var previous []byte
	if err := tx.QueryRow(ctx, queryProviderFinancialGuard, pgx.StrictNamedArgs{
		"runtime_revision_id": lease["runtimeRevisionID"], "organization_id": current.organizationID,
		"lease_id": lease["leaseID"], "root_run_id": lease["rootRunID"],
	}).Scan(&created, &previousCreated, &previousLease, &previousSequence, &previous); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrForbidden
		}
		return errs.ErrUnavailable
	}
	input.Mutation.IdempotencyKey = financialCallbackKey(stringMap(lease, "leaseID"), payload.Generation, observation.Sequence)
	input.Mutation.IntentDigest = digestBytes(asJSON(observation))
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID, current.actorID,
		input.Mutation.Operation, input.Mutation.IdempotencyKey); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func financialCallbackKey(lease string, generation, sequence int64) string {
	return digestBytes(asJSON([]any{"provider-financial-callback-v1", lease, generation, sequence}))
}
