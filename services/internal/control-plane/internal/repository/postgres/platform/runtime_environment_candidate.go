package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryRuntimeClaimEnvironmentCandidate locks the active ENV, agent and exact binding in dependent stages. Agent/ENV metadata owns configuration scope: a canonical SYSTEM run may have project context while using organization-owned ENV; only that system profile permits an unpinned current-version fallback.
// queryRuntimeClaimEnvironmentCandidate блокирует активное ENV, агента и exact binding зависимыми этапами. Metadata агента/ENV задаёт область конфигурации: канонический SYSTEM run может иметь проектный контекст с общим ENV; лишь этот системный профиль допускает current-version fallback без pin.
//
//go:embed sql/runtime_claimexecution_lock_environment_candidate.sql
var queryRuntimeClaimEnvironmentCandidate string

// retainRuntimeEnvironmentCandidate fences the cached queue projection before materializing a revision. SKIP LOCKED avoids waiting on contended rows, not all possible database waits; the caller context bounds the statement. Miss/drift rolls back partial savepoint locks and leaves work queued. Successful locks survive savepoint release until the owner commit, preserving binding/active-execution secret retention without taking inverse-order secret locks.
// retainRuntimeEnvironmentCandidate ограждает сохранённую queue projection до создания ревизии. SKIP LOCKED не ждёт занятых строк, но не исключает все database waits; statement ограничен caller context. Miss/drift откатывает частичные savepoint locks, сохраняя очередь. Успешные locks переживают release savepoint до owner commit и сохраняют удержание секрета binding/активным execution без обратного порядка secret locks.
func retainRuntimeEnvironmentCandidate(ctx context.Context, tx pgx.Tx, organizationID string, candidate claimableExecution) (bool, error) {
	guard, err := tx.Begin(ctx)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	var bindingRef string
	err = guard.QueryRow(ctx, queryRuntimeClaimEnvironmentCandidate, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": candidate.projectID, "agent_id": candidate.agentID,
		"environment_ref": candidate.runtimeEnvironmentRef, "environment_version_id": candidate.runtimeEnvironmentID,
		"environment_digest": candidate.runtimeEnvironmentDigest, "binding_id": candidate.environmentBindingID,
		"binding_ref": candidate.environmentBindingRef, "binding_version": candidate.environmentBindingVersion,
		"binding_digest": candidate.environmentBindingDigest,
	}).Scan(&bindingRef)
	if err != nil || bindingRef != candidate.environmentBindingRef {
		if rollbackErr := guard.Rollback(ctx); rollbackErr != nil {
			return false, errs.ErrUnavailable
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, errs.ErrUnavailable
	}
	if err := guard.Commit(ctx); err != nil {
		return false, errs.ErrUnavailable
	}
	return true, nil
}
