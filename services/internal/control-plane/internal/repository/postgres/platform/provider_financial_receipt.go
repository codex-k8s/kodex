package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// queryProviderFinancialReceipt embeds a locked idempotency receipt lookup by exact
// tenant/actor/operation/key. Fresh execution, credential and deadline guards must run first; a
// stored receipt cannot restore old authority.
// queryProviderFinancialReceipt встраивает locked idempotency receipt lookup по exact
// tenant/actor/operation/key. Fresh execution, credential и deadline guards выполняются заранее;
// stored receipt не восстанавливает прежние полномочия.
//
//go:embed sql/provider_financial_receipt.sql
var queryProviderFinancialReceipt string

// bindFinancialCallbackReceipt ignores commands without a financial progress callback, otherwise
// requires a standalone valid observation and fresh lease/root/deadline/current-credential guards.
// It rewrites mutation key/digest from semantic observation and locks that idempotency scope; it
// neither reads the receipt nor publishes the observation here, and does not commit.
// bindFinancialCallbackReceipt пропускает commands без financial progress callback, иначе требует
// отдельную допустимую observation и fresh lease/root/deadline/current-credential guards.
// Переписывает mutation key/digest по semantic observation и блокирует idempotency scope; здесь не
// читает receipt, не публикует observation и не выполняет commit.
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
	if _, err := readProviderFinancialCollector(ctx, tx, current, lease); err != nil {
		return err
	}
	input.Mutation.IdempotencyKey = financialCallbackKey(stringMap(lease, "leaseID"), payload.Generation, observation.Sequence)
	input.Mutation.IntentDigest = digestBytes(asJSON(observation))
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID, current.actorID,
		input.Mutation.Operation, input.Mutation.IdempotencyKey); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// financialCallbackKey hashes a versioned JSON tuple of lease, generation and observation sequence
// into the callback idempotency key. Transport correlation and observation content are excluded;
// content is bound separately by IntentDigest after fresh authority checks.
// financialCallbackKey хеширует versioned JSON tuple из lease, generation и observation sequence в
// callback idempotency key. Transport correlation и observation content исключены; content
// связывается отдельно через IntentDigest после fresh authority checks.
func financialCallbackKey(lease string, generation, sequence int64) string {
	return digestBytes(asJSON([]any{"provider-financial-callback-v1", lease, generation, sequence}))
}
