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

//go:embed sql/provider_queued_work_lock.sql
var queryProviderQueuedWorkLock string

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
