package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryProviderQueuedWorkLock embeds a tenant-bound run lock returning current version/state,
// exact account QUEUED_TURN linkage and canonical cancellability. Permissions and terminal outcome
// classification remain separate caller checks.
// queryProviderQueuedWorkLock встраивает tenant-bound run lock с current version/state, exact
// account QUEUED_TURN linkage и canonical cancellability. Permissions и terminal outcome
// classification остаются отдельными caller checks.
//
//go:embed sql/provider_queued_work_lock.sql
var queryProviderQueuedWorkLock string

// cancelProviderAccountQueuedWork validates a bounded unique run selection and current
// account-blocker digest before processing each run in order. It returns per-run closed outcomes
// plus refreshed account data; the first error discards the result while earlier mutations await
// caller rollback. Access, account OCC and commit belong to the enclosing command.
// cancelProviderAccountQueuedWork проверяет ограниченный уникальный run selection и current
// account-blocker digest до последовательной обработки каждого run. Возвращает per-run closed
// outcomes и обновлённый account; первая ошибка отбрасывает result, а прежние mutations ожидают
// caller rollback. Access, account OCC и commit принадлежат enclosing command.
func (repository *Repository) cancelProviderAccountQueuedWork(ctx context.Context, tx pgx.Tx, current scope, input command.Command, payload command.ProviderAccountInput, accountID string, version int64) (commandOutcome, error) {
	if len(payload.SelectedRunRefs) < 1 || len(payload.SelectedRunRefs) > 64 || len(payload.BlockersDigest) != 64 {
		return commandOutcome{}, errs.ErrInvalid
	}
	seen := make(map[string]bool, len(payload.SelectedRunRefs))
	for _, ref := range payload.SelectedRunRefs {
		if !strings.HasPrefix(ref, "run_") || len(ref) > 96 || seen[ref] {
			return commandOutcome{}, errs.ErrInvalid
		}
		seen[ref] = true
	}
	page, err := repository.providerAccountBlockerPage(ctx, tx, current, accountID, version, query.ProviderAccountBlockers{AccountRef: payload.AccountRef, Page: query.Page{Size: 1}})
	if err != nil {
		return commandOutcome{}, err
	}
	if page.ContextDigest != payload.BlockersDigest {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	results := make([]entity.ProviderQueuedWorkCancellationResult, 0, len(payload.SelectedRunRefs))
	for _, ref := range payload.SelectedRunRefs {
		outcome, err := repository.cancelSelectedProviderRun(ctx, tx, current, accountID, ref)
		if err != nil {
			return commandOutcome{}, err
		}
		results = append(results, entity.ProviderQueuedWorkCancellationResult{RunRef: ref, Outcome: outcome})
	}
	account, err := repository.providerAccountByRef(ctx, tx, current, payload.AccountRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{ProviderAccount: &account, ProviderQueuedWorkResults: results}, resourceKind: "PROVIDER_ACCOUNT", resourceRef: account.Ref,
		summary: "i18n:PROVIDER_ACCOUNT_QUEUED_WORK_PROCESSED", platformAggregateVersion: account.Version, platformState: account.State}, nil
}

// cancelSelectedProviderRun checks run.view then run.cancel before locking exact tenant run state
// and account linkage. Hidden, unauthorized, terminal, blocked and stale cases return distinct
// safe outcomes; only an eligible linked run reaches canonical changeRun cancellation, preserving
// its full graph closure and events in the caller transaction.
// cancelSelectedProviderRun проверяет run.view, затем run.cancel до lock exact tenant run state и
// account linkage. Hidden, unauthorized, terminal, blocked и stale случаи возвращают разные safe
// outcomes; только допустимый linked run достигает canonical changeRun cancellation с полным graph
// closure/events в caller transaction.
func (repository *Repository) cancelSelectedProviderRun(ctx context.Context, tx pgx.Tx, current scope, accountID, ref string) (string, error) {
	target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: ref}
	if err := repository.requireAccess(ctx, tx, current, "run.view", target); err != nil {
		if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) {
			return "NOT_FOUND", nil
		}
		return "", err
	}
	if err := repository.requireAccess(ctx, tx, current, "run.cancel", target); err != nil {
		if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) {
			return "PERMISSION_REQUIRED", nil
		}
		return "", err
	}
	var version int64
	var state string
	var linked, safe bool
	if err := tx.QueryRow(ctx, queryProviderQueuedWorkLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "account_id": accountID, "run_ref": ref}).Scan(&version, &state, &linked, &safe); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "NOT_FOUND", nil
		}
		return "", errs.ErrUnavailable
	}
	if contains([]string{"SUCCEEDED", "FAILED", "CANCELLED"}, state) {
		return "ALREADY_TERMINAL", nil
	}
	if !contains([]string{"QUEUED", "RUNNING"}, state) || !safe {
		return "BLOCKED", nil
	}
	if !linked {
		return "STALE", nil
	}
	// Полный переход Run остаётся владельцем закрытия узлов, leases, turns и событий.
	_, err := repository.changeRun(ctx, tx, current, command.Command{Kind: command.CancelRun, Mutation: value.Mutation{ExpectedVersion: &version}, Payload: command.RunCommandInput{RunRef: ref}})
	if err != nil {
		return "", err
	}
	return "CANCELLED", nil
}
