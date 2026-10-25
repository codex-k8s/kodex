package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	accessservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryProviderFinancialOverview embeds organization-scoped nondeleted openai-codex account
// display rows. Financial data is projected only for enabled AUTHORIZED accounts with matching
// current credential; actor eligibility/view filtering remains with the reader.
// queryProviderFinancialOverview встраивает organization-scoped display rows неудалённых
// openai-codex accounts. Financial data проецируется только для enabled AUTHORIZED accounts с
// matching current credential; actor eligibility/view filtering остаётся у reader.
//
//go:embed sql/provider_financial_overview.sql
var queryProviderFinancialOverview string

// GetProviderFinancialOverview uses a read-only RepeatableRead snapshot to load the current access
// subject/bindings and eligible accounts, then filters account view permission before
// parsing/aggregating financial data. The display expires five minutes after observation time.
// Commit/rollback failures clear partial output; this is a stored observation view, not a live
// provider balance or authorization grant.
// GetProviderFinancialOverview использует read-only RepeatableRead snapshot для current access
// subject/bindings и eligible accounts, затем фильтрует account view permission до
// parsing/aggregation financial data. Display истекает через пять минут после observation time.
// Commit/rollback failures очищают partial output; это stored observation view, а не live provider
// balance или authorization grant.
func (repository *Repository) GetProviderFinancialOverview(ctx context.Context, p value.Principal) (result entity.ProviderFinancialOverview, resultError error) {
	current, tx, err := repository.beginScopedSnapshotRead(ctx, p)
	if err != nil {
		return entity.ProviderFinancialOverview{}, err
	}
	defer func() {
		rollbackAssistantLockedRead(ctx, tx, &resultError)
		if resultError != nil {
			result = entity.ProviderFinancialOverview{}
		}
	}()
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return entity.ProviderFinancialOverview{}, err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return entity.ProviderFinancialOverview{}, err
	}
	refs, all, err := repository.financialEligibility(ctx, tx, current)
	if err != nil {
		return entity.ProviderFinancialOverview{}, err
	}
	result = entity.ProviderFinancialOverview{Accounts: []entity.ProviderFinancialDisplayAccount{}, ObservedAt: time.Now().UTC()}
	result.ValidUntil = result.ObservedAt.Add(5 * time.Minute)
	rows, err := tx.Query(ctx, queryProviderFinancialOverview, pgx.StrictNamedArgs{"organization_id": current.organizationID})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	for rows.Next() {
		var account entity.ProviderFinancialDisplayAccount
		var raw []byte
		var received *time.Time
		var version int64
		if rows.Scan(&account.Ref, &account.Name, &raw, &received, &version) != nil {
			rows.Close()
			return entity.ProviderFinancialOverview{}, errs.ErrUnavailable
		}
		target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROVIDER_ACCOUNT", ResourceRef: account.Ref}
		if !(all || refs[account.Ref]) || !accessservice.Evaluate(subject.AccessSubject, "provider.account.view", target, "", bindings, result.ObservedAt).Allowed {
			continue
		}
		if len(raw) > 0 {
			if received == nil {
				rows.Close()
				return entity.ProviderFinancialOverview{}, errs.ErrUnavailable
			}
			summary := &entity.ProviderFinancialSummary{ReceivedAt: *received, Version: version}
			observation, err := runtimecontract.ParseProviderFinancialObservation(raw)
			if err != nil {
				rows.Close()
				return entity.ProviderFinancialOverview{}, errs.ErrUnavailable
			}
			summary.Observation = observation
			account.FinancialSummary = summary
		}
		result.Add(account)
	}
	rows.Close()
	if rows.Err() != nil {
		return entity.ProviderFinancialOverview{}, errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return entity.ProviderFinancialOverview{}, errs.ErrConflict
	}
	return result, nil
}
