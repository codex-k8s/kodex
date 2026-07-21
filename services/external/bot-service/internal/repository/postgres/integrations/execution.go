package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domain "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/integrations"
	"github.com/jackc/pgx/v5"
)

func (repo *Repository) ClaimExecution(ctx context.Context, workerID string, proposedFence string, now time.Time, leaseDuration time.Duration) (domain.ExecutionClaim, bool, error) {
	if workerID == "" || proposedFence == "" || leaseDuration <= 0 {
		return domain.ExecutionClaim{}, false, domain.ErrInvalidInput
	}
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return domain.ExecutionClaim{}, false, fmt.Errorf("begin integration execution claim: %w", err)
	}
	defer rollback(ctx, tx)
	var claim domain.ExecutionClaim
	var state domain.InvocationStatus
	var existingFence string
	err = tx.QueryRow(ctx, query("execution__claim_select.sql"), now).Scan(
		&claim.InvocationID, &claim.InvocationPublicID, &state, &existingFence,
		&claim.Arguments.Namespace, &claim.Arguments.WorkloadKind, &claim.Arguments.WorkloadName,
		&claim.ArgumentsHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExecutionClaim{}, false, nil
	}
	if err != nil {
		return domain.ExecutionClaim{}, false, fmt.Errorf("select integration execution claim: %w", err)
	}
	claim.ExecutionFence = proposedFence
	if state == domain.InvocationStatusExecuting {
		claim.ExecutionFence = existingFence
	}
	claim.LeaseOwner = workerID
	command, err := tx.Exec(ctx, query("execution__claim_update.sql"),
		now, claim.ExecutionFence, workerID, now.Add(leaseDuration), claim.InvocationID,
	)
	if err != nil {
		return domain.ExecutionClaim{}, false, fmt.Errorf("update integration execution claim: %w", err)
	}
	if command.RowsAffected() != 1 {
		return domain.ExecutionClaim{}, false, domain.ErrNoExecution
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ExecutionClaim{}, false, fmt.Errorf("commit integration execution claim: %w", err)
	}
	return claim, true, nil
}

func (repo *Repository) RecordExecution(ctx context.Context, claim domain.ExecutionClaim, executionID string, now time.Time) (domain.ExecutionReceipt, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return domain.ExecutionReceipt{}, fmt.Errorf("begin recording execution: %w", err)
	}
	defer rollback(ctx, tx)
	existing, err := readReceipt(ctx, tx, claim.InvocationID)
	if err == nil {
		if existing.ExecutionFence != claim.ExecutionFence || existing.ArgumentsHash != claim.ArgumentsHash {
			return domain.ExecutionReceipt{}, domain.ErrApprovalBinding
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.ExecutionReceipt{}, fmt.Errorf("commit recording execution readback: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, domain.ErrReceiptMissing) {
		return domain.ExecutionReceipt{}, err
	}
	var authorizedID int64
	err = tx.QueryRow(ctx, query("execution__authorize.sql"),
		claim.InvocationID, claim.ExecutionFence, claim.LeaseOwner, now,
	).Scan(&authorizedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExecutionReceipt{}, domain.ErrAuthorizationChanged
	}
	if err != nil {
		return domain.ExecutionReceipt{}, fmt.Errorf("authorize recording execution: %w", err)
	}
	result := domain.ExecutionResult{
		ExecutionID: executionID, Namespace: claim.Arguments.Namespace,
		WorkloadKind: claim.Arguments.WorkloadKind, WorkloadName: claim.Arguments.WorkloadName,
		RecordedAt: now.UTC().Format(time.RFC3339Nano),
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return domain.ExecutionReceipt{}, fmt.Errorf("encode recording execution: %w", err)
	}
	argumentsHash, err := decodeHash(claim.ArgumentsHash)
	if err != nil {
		return domain.ExecutionReceipt{}, err
	}
	if _, err := tx.Exec(ctx, query("execution__receipt_insert.sql"),
		claim.InvocationID, executionID, claim.ExecutionFence, argumentsHash, resultJSON, now,
	); err != nil {
		return domain.ExecutionReceipt{}, fmt.Errorf("insert recording execution receipt: %w", err)
	}
	receipt, err := readReceipt(ctx, tx, claim.InvocationID)
	if err != nil {
		return domain.ExecutionReceipt{}, err
	}
	if receipt.ExecutionFence != claim.ExecutionFence || receipt.ArgumentsHash != claim.ArgumentsHash {
		return domain.ExecutionReceipt{}, domain.ErrApprovalBinding
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ExecutionReceipt{}, fmt.Errorf("commit recording execution receipt: %w", err)
	}
	return receipt, nil
}

func readReceipt(ctx context.Context, db repositoryDB, invocationID int64) (domain.ExecutionReceipt, error) {
	var receipt domain.ExecutionReceipt
	receipt.InvocationID = invocationID
	err := db.QueryRow(ctx, query("execution__receipt_get.sql"), invocationID).Scan(
		&receipt.Result.ExecutionID, &receipt.ExecutionFence, &receipt.ArgumentsHash,
		&receipt.Result.Namespace, &receipt.Result.WorkloadKind, &receipt.Result.WorkloadName, &receipt.Result.RecordedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExecutionReceipt{}, domain.ErrReceiptMissing
	}
	if err != nil {
		return domain.ExecutionReceipt{}, fmt.Errorf("read recording execution receipt: %w", err)
	}
	return receipt, nil
}

type lockedExecution struct {
	state             domain.InvocationStatus
	fence             string
	correlationID     string
	installationScope string
	workspaceScope    string
	sessionScope      string
	subjectRef        string
	publicID          string
}

func (repo *Repository) FinalizeExecution(ctx context.Context, claim domain.ExecutionClaim, now time.Time) (domain.Invocation, error) {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return domain.Invocation{}, fmt.Errorf("begin integration execution finalize: %w", err)
	}
	defer rollback(ctx, tx)
	txRepo := newTransactionalRepository(tx)
	locked, err := lockExecution(ctx, tx, claim.InvocationID)
	if err != nil {
		return domain.Invocation{}, err
	}
	if locked.fence != claim.ExecutionFence {
		return domain.Invocation{}, domain.ErrApprovalBinding
	}
	if locked.state == domain.InvocationStatusSucceeded {
		invocation, err := txRepo.invocationByID(ctx, claim.InvocationID)
		if err != nil {
			return domain.Invocation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Invocation{}, fmt.Errorf("commit repeated execution finalize: %w", err)
		}
		return invocation, nil
	}
	if locked.state != domain.InvocationStatusExecuting {
		return domain.Invocation{}, domain.ErrNoExecution
	}
	receipt, err := readReceipt(ctx, tx, claim.InvocationID)
	if err != nil {
		return domain.Invocation{}, err
	}
	if receipt.ExecutionFence != claim.ExecutionFence || receipt.ArgumentsHash != claim.ArgumentsHash {
		return domain.Invocation{}, domain.ErrApprovalBinding
	}
	resultJSON, err := json.Marshal(receipt.Result)
	if err != nil {
		return domain.Invocation{}, fmt.Errorf("encode finalized recording result: %w", err)
	}
	command, err := tx.Exec(ctx, query("execution__finalize.sql"), claim.InvocationID, claim.ExecutionFence, resultJSON, now)
	if err != nil {
		return domain.Invocation{}, fmt.Errorf("finalize integration execution: %w", err)
	}
	if command.RowsAffected() != 1 {
		return domain.Invocation{}, domain.ErrNoExecution
	}
	if err := txRepo.appendAudit(ctx, auditInput{
		EventType: "integration.invocation.executed", ActorUserID: locked.subjectRef, ActorUser: "recording_worker",
		ResourceType: "tool_invocation", ResourceName: locked.publicID,
		Summary: "Recording executor сохранил безопасную квитанцию.", CorrelationID: locked.correlationID,
		InstallationScope: locked.installationScope, WorkspaceScope: locked.workspaceScope,
		SessionScope: locked.sessionScope, Outcome: "succeeded", ReasonCode: "",
		Metadata: auditMetadata{InvocationID: locked.publicID, ExecutionID: receipt.Result.ExecutionID}, Now: now,
	}); err != nil {
		return domain.Invocation{}, err
	}
	invocation, err := txRepo.invocationByID(ctx, claim.InvocationID)
	if err != nil {
		return domain.Invocation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Invocation{}, fmt.Errorf("commit integration execution finalize: %w", err)
	}
	return invocation, nil
}

func (repo *Repository) CancelExecution(ctx context.Context, claim domain.ExecutionClaim, reasonCode string, now time.Time) error {
	tx, err := repo.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin integration execution cancellation: %w", err)
	}
	defer rollback(ctx, tx)
	txRepo := newTransactionalRepository(tx)
	locked, err := lockExecution(ctx, tx, claim.InvocationID)
	if err != nil {
		return err
	}
	if locked.state == domain.InvocationStatusCancelled && locked.fence == claim.ExecutionFence {
		return tx.Commit(ctx)
	}
	if locked.state != domain.InvocationStatusExecuting || locked.fence != claim.ExecutionFence {
		return domain.ErrNoExecution
	}
	command, err := tx.Exec(ctx, query("execution__cancel.sql"),
		claim.InvocationID, claim.ExecutionFence, reasonCode, now, claim.LeaseOwner,
	)
	if err != nil {
		return fmt.Errorf("cancel integration execution: %w", err)
	}
	if command.RowsAffected() != 1 {
		return domain.ErrNoExecution
	}
	if err := txRepo.appendAudit(ctx, auditInput{
		EventType: "integration.invocation.cancelled", ActorUserID: locked.subjectRef, ActorUser: "recording_worker",
		ResourceType: "tool_invocation", ResourceName: locked.publicID,
		Summary: "Выполнение опасной capability заблокировано.", CorrelationID: locked.correlationID,
		InstallationScope: locked.installationScope, WorkspaceScope: locked.workspaceScope,
		SessionScope: locked.sessionScope, Outcome: "cancelled", ReasonCode: reasonCode,
		Metadata: auditMetadata{InvocationID: locked.publicID}, Now: now,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit integration execution cancellation: %w", err)
	}
	return nil
}

func lockExecution(ctx context.Context, tx pgx.Tx, invocationID int64) (lockedExecution, error) {
	var item lockedExecution
	err := tx.QueryRow(ctx, query("execution__finalize_lock.sql"), invocationID).Scan(
		&item.state, &item.fence, &item.correlationID, &item.installationScope,
		&item.workspaceScope, &item.sessionScope, &item.subjectRef, &item.publicID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedExecution{}, domain.ErrNoExecution
	}
	if err != nil {
		return lockedExecution{}, fmt.Errorf("lock integration execution: %w", err)
	}
	return item, nil
}
