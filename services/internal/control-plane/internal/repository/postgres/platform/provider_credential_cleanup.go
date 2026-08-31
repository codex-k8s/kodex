package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

const (
	providerCredentialCleanupRetention   = 24 * time.Hour
	providerCredentialCleanupLease       = 2 * time.Minute
	providerCredentialCleanupMaxAttempts = 5
)

type providerCredentialCleanupCandidate struct {
	id, ref, accountRef                               string
	secretName, secretUID, secretVersion, contentHash string
}

type lockedProviderCredentialCleanupTask struct {
	state, leaseOwner, safeErrorCode, terminalReceipt string
	generation                                        int64
	attempts, maximumAttempts                         int32
	leaseExpiresAt                                    *time.Time
}

func (repository *Repository) ClaimProviderCredentialCleanupTasks(
	ctx context.Context,
	leaseOwner string,
	limit int32,
) ([]domainrepo.ProviderCredentialCleanupTask, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || len(leaseOwner) > 128 || limit < 1 || limit > 16 {
		return nil, errs.ErrInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryProviderCredentialCleanupExpireTerminalClaims); err != nil {
		return nil, fmt.Errorf("expire terminal provider credential cleanup claims: %w", errs.ErrUnavailable)
	}
	accountRows, err := tx.Query(ctx, queryProviderCredentialCleanupLockClaimableAccounts,
		pgx.StrictNamedArgs{"limit": limit})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	accountIDs := make([]string, 0, limit)
	for accountRows.Next() {
		var accountID, accountRef string
		if err := accountRows.Scan(&accountID, &accountRef); err != nil {
			accountRows.Close()
			return nil, errs.ErrUnavailable
		}
		accountIDs = append(accountIDs, accountID)
	}
	if err := accountRows.Err(); err != nil {
		accountRows.Close()
		return nil, errs.ErrUnavailable
	}
	accountRows.Close()

	now := time.Now().UTC()
	items := make([]domainrepo.ProviderCredentialCleanupTask, 0, limit)
	for _, accountID := range accountIDs {
		remaining := limit - int32(len(items))
		if remaining == 0 {
			break
		}
		rows, err := tx.Query(ctx, queryProviderCredentialCleanupSelectClaimableTasks,
			pgx.StrictNamedArgs{"account_id": accountID, "limit": remaining})
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		candidates := make([]providerCredentialCleanupCandidate, 0, remaining)
		for rows.Next() {
			var candidate providerCredentialCleanupCandidate
			if err := rows.Scan(&candidate.id, &candidate.ref, &candidate.accountRef,
				&candidate.secretName, &candidate.secretUID, &candidate.secretVersion,
				&candidate.contentHash); err != nil {
				rows.Close()
				return nil, errs.ErrUnavailable
			}
			candidates = append(candidates, candidate)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		rows.Close()
		for _, candidate := range candidates {
			expiresAt := now.Add(providerCredentialCleanupLease)
			var attempt int32
			var generation int64
			if err := tx.QueryRow(ctx, queryProviderCredentialCleanupClaimTask, pgx.StrictNamedArgs{
				"task_id": candidate.id, "lease_owner": leaseOwner, "lease_expires_at": expiresAt,
			}).Scan(&attempt, &generation, &expiresAt); errors.Is(err, pgx.ErrNoRows) {
				return nil, errs.ErrConflict
			} else if err != nil {
				return nil, errs.ErrUnavailable
			}
			items = append(items, domainrepo.ProviderCredentialCleanupTask{
				Ref: candidate.ref, AccountRef: candidate.accountRef, Attempt: attempt,
				Generation: generation, LeaseExpiresAt: expiresAt,
				Credential: entity.ProviderCredentialDescriptor{
					SecretName: candidate.secretName, SecretUID: candidate.secretUID,
					SecretResourceVersion: candidate.secretVersion, ContentSHA256: candidate.contentHash,
				},
			})
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return items, nil
}

func (repository *Repository) CompleteProviderCredentialCleanupTask(
	ctx context.Context,
	taskRef, leaseOwner string,
	generation int64,
	terminalReceipt string,
) (domainrepo.ProviderCredentialCleanupResult, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	terminalReceipt = strings.TrimSpace(terminalReceipt)
	if !strings.HasPrefix(taskRef, "pcct_") || leaseOwner == "" || len(leaseOwner) > 128 ||
		generation < 1 || terminalReceipt == "" || len(terminalReceipt) > 512 {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrInvalid
	}
	tx, task, err := repository.lockProviderCredentialCleanupTask(ctx, taskRef)
	if err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if task.state == "COMPLETED" && task.generation == generation && task.terminalReceipt == terminalReceipt {
		if err := tx.Commit(ctx); err != nil {
			return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
		}
		return providerCredentialCleanupResult(taskRef, task, false), nil
	}
	if !validProviderCredentialCleanupClaim(task, leaseOwner, generation) {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	tag, err := tx.Exec(ctx, queryProviderCredentialCleanupCompleteTask, pgx.StrictNamedArgs{
		"task_ref": taskRef, "lease_owner": leaseOwner, "lease_generation": generation,
		"terminal_receipt": terminalReceipt,
	})
	if err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	task.state, task.safeErrorCode, task.terminalReceipt = "COMPLETED", "", terminalReceipt
	return providerCredentialCleanupResult(taskRef, task, false), nil
}

func (repository *Repository) FailProviderCredentialCleanupTask(
	ctx context.Context,
	taskRef, leaseOwner string,
	generation int64,
	safeErrorCode string,
) (domainrepo.ProviderCredentialCleanupResult, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if !strings.HasPrefix(taskRef, "pcct_") || leaseOwner == "" || len(leaseOwner) > 128 ||
		generation < 1 || !validProviderCredentialCleanupError(safeErrorCode) {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrInvalid
	}
	tx, task, err := repository.lockProviderCredentialCleanupTask(ctx, taskRef)
	if err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if (task.state == "PENDING" || task.state == "DEAD_LETTER") && task.generation == generation &&
		task.safeErrorCode == safeErrorCode {
		if err := tx.Commit(ctx); err != nil {
			return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
		}
		return providerCredentialCleanupResult(taskRef, task, task.state == "PENDING"), nil
	}
	if !validProviderCredentialCleanupClaim(task, leaseOwner, generation) {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	now := time.Now().UTC()
	state, receipt := "PENDING", ""
	var completedAt *time.Time
	if task.attempts >= task.maximumAttempts {
		state = "DEAD_LETTER"
		completedAt = &now
		receipt = fmt.Sprintf("dead-letter:%s:g%d:a%d:%s", taskRef, generation, task.attempts, safeErrorCode)
	}
	eligibleAt := now.Add(providerCredentialCleanupBackoff(task.attempts))
	tag, err := tx.Exec(ctx, queryProviderCredentialCleanupFailTask, pgx.StrictNamedArgs{
		"task_ref": taskRef, "lease_owner": leaseOwner, "lease_generation": generation,
		"state": state, "eligible_at": eligibleAt, "safe_error_code": safeErrorCode,
		"terminal_receipt": receipt, "completed_at": completedAt,
	})
	if err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	task.state, task.safeErrorCode, task.terminalReceipt = state, safeErrorCode, receipt
	return providerCredentialCleanupResult(taskRef, task, state == "PENDING"), nil
}

func (repository *Repository) lockProviderCredentialCleanupTask(
	ctx context.Context,
	taskRef string,
) (pgx.Tx, lockedProviderCredentialCleanupTask, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, lockedProviderCredentialCleanupTask{}, errs.ErrUnavailable
	}
	var task lockedProviderCredentialCleanupTask
	err = tx.QueryRow(ctx, queryProviderCredentialCleanupLockTask,
		pgx.StrictNamedArgs{"task_ref": taskRef}).Scan(
		&task.state, &task.leaseOwner, &task.generation, &task.leaseExpiresAt,
		&task.attempts, &task.maximumAttempts, &task.safeErrorCode, &task.terminalReceipt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return nil, lockedProviderCredentialCleanupTask{}, errs.ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, lockedProviderCredentialCleanupTask{}, errs.ErrUnavailable
	}
	return tx, task, nil
}

func validProviderCredentialCleanupClaim(task lockedProviderCredentialCleanupTask, owner string, generation int64) bool {
	return task.state == "CLAIMED" && task.leaseOwner == owner && task.generation == generation &&
		task.leaseExpiresAt != nil && task.leaseExpiresAt.After(time.Now())
}

func providerCredentialCleanupResult(
	ref string,
	task lockedProviderCredentialCleanupTask,
	retryScheduled bool,
) domainrepo.ProviderCredentialCleanupResult {
	return domainrepo.ProviderCredentialCleanupResult{
		Ref: ref, State: task.state, SafeErrorCode: task.safeErrorCode,
		TerminalReceipt: task.terminalReceipt, RetryScheduled: retryScheduled,
	}
}

func providerCredentialCleanupBackoff(attempt int32) time.Duration {
	shift := max(attempt-1, 0)
	if shift > 6 {
		shift = 6
	}
	backoff := 5 * time.Second * time.Duration(1<<shift)
	return min(backoff, 5*time.Minute)
}

func validProviderCredentialCleanupError(value string) bool {
	switch value {
	case "PROVIDER_CREDENTIAL_CLEANUP_UNAVAILABLE",
		"PROVIDER_CREDENTIAL_CLEANUP_REJECTED",
		"PROVIDER_CREDENTIAL_CLEANUP_TIMEOUT",
		"PROVIDER_CREDENTIAL_CLEANUP_FAILED":
		return true
	default:
		return false
	}
}

func (repository *Repository) scheduleProviderCredentialCleanup(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, accountID, credentialRevisionID string,
	eligibleAt time.Time,
) error {
	if credentialRevisionID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, queryProviderCredentialCleanupScheduleRevision, pgx.StrictNamedArgs{
		"organization_id": organizationID, "account_id": accountID,
		"credential_revision_id": credentialRevisionID, "eligible_at": eligibleAt,
		"maximum_attempts": providerCredentialCleanupMaxAttempts,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
