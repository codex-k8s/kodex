package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryProviderFinancialPublish embeds guarded financial-observation replacement for the exact
// live lease/revision/node/turn/session and current authorized credential. Only a newer collector
// or increasing sequence changes account version; it does not perform billing or expose credential
// bytes.
// queryProviderFinancialPublish встраивает guarded замену financial observation для exact live
// lease/revision/node/turn/session и current authorized credential. Account version меняется
// только при более новом collector либо растущем sequence; billing и выдача credential bytes
// отсутствуют.
//
//go:embed sql/provider_financial_publish.sql
var queryProviderFinancialPublish string

// queryProviderFinancialGuard embeds account locking through exact live execution/credential pins
// and returns previous observation only for the current credential revision. It rejects obsolete
// generations and terminal roots; caller-supplied lease metadata alone is not authority.
// queryProviderFinancialGuard встраивает account lock через exact live execution/credential pins и
// возвращает previous observation только для current credential revision. Отклоняет obsolete
// generations и terminal roots; переданной caller lease metadata недостаточно для authority.
//
//go:embed sql/provider_financial_guard.sql
var queryProviderFinancialGuard string

// publishFinancial validates observation shape, locks a nonterminal root and eligible account, and
// orders collector/sequence before updating metadata and emitting an account snapshot. Exact
// replay or older observations are no-ops; conflicting same-sequence content is IdempotencyReuse.
// All effects stay in the caller transaction without commit or external billing.
// publishFinancial проверяет observation shape, блокирует nonterminal root и допустимый account,
// упорядочивает collector/sequence до metadata update и account snapshot. Exact replay либо старые
// observations дают no-op; противоречивый same-sequence content — IdempotencyReuse. Все effects
// остаются в caller transaction без commit или внешнего billing.
func (repository *Repository) publishFinancial(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, observation runtimecontract.ProviderFinancialObservation) error {
	if observation.Validate() != nil {
		return errs.ErrInvalid
	}
	var rootID, rootState string
	if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionLockRootRun, lease["rootRunID"]).Scan(&rootID, &rootState); err != nil {
		return errs.ErrUnavailable
	}
	if rootID != stringMap(lease, "rootRunID") || rootState == "SUCCEEDED" || rootState == "FAILED" || rootState == "CANCELLED" {
		return errs.ErrConflict
	}
	collector, err := readProviderFinancialCollector(ctx, tx, current, lease)
	if err != nil {
		return err
	}
	accepted, err := acceptFinancialCollector(stringMap(lease, "leaseID"), collector.createdAt, observation.Sequence, asJSON(observation), collector.previousLease, collector.previousCreatedAt, collector.previousSequence, collector.previous)
	if err != nil {
		return errs.ErrIdempotencyReuse
	}
	if !accepted {
		return nil
	}

	var ref string
	var version int64
	err = tx.QueryRow(ctx, queryProviderFinancialPublish, pgx.StrictNamedArgs{
		"observation": asJSON(observation), "sequence": observation.Sequence,
		"runtime_revision_id": lease["runtimeRevisionID"], "organization_id": current.organizationID,
		"lease_id": lease["leaseID"], "root_run_id": lease["rootRunID"],
	}).Scan(&ref, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	return repository.emitPlatformEventSnapshot(ctx, tx, current, "PROVIDER_ACCOUNT_CHANGED", "", ref, "i18n:PROVIDER_ACCOUNT_UPDATED", version, "AUTHORIZED")
}

// queryProviderFinancialAgents embeds ordered non-archived agent refs within one
// organization/project. Runtime configuration and view/manage/launch eligibility are resolved
// separately before account aggregation.
// queryProviderFinancialAgents встраивает упорядоченные non-archived agent refs одного
// organization/project. Runtime configuration и view/manage/launch eligibility разрешаются
// отдельно до агрегации accounts.
//
//go:embed sql/provider_financial_agents.sql
var queryProviderFinancialAgents string

// financialEligibility allows organization-wide eligibility only with organization.manage, or
// gathers accounts from enabled openai-codex agents in the authority project that are viewable and
// manageable or launchable. Hidden agents are skipped, unexpected access failures propagate, and
// candidate/selected refs form a set; each account still needs its own view check.
// financialEligibility разрешает organization-wide eligibility только с organization.manage либо
// собирает accounts enabled openai-codex agents authority project, доступных для view и manage
// либо launch. Hidden agents пропускаются, неожиданные access failures возвращаются,
// candidate/selected refs образуют set; каждому account всё ещё нужен отдельный view check.
func (repository *Repository) financialEligibility(ctx context.Context, tx pgx.Tx, current scope) (map[string]bool, bool, error) {
	if current.authorityProjectID == "" {
		err := repository.requireAccess(ctx, tx, current, "organization.manage", organizationTarget(current.organizationRef))
		if err == nil {
			return nil, true, nil
		}
		if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) {
			return nil, false, nil
		}
		return nil, false, err
	}
	rows, err := tx.Query(ctx, queryProviderFinancialAgents, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": current.authorityProjectID})
	if err != nil {
		return nil, false, errs.ErrUnavailable
	}
	refs := []string{}
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return nil, false, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, false, errs.ErrUnavailable
	}
	allowed := map[string]bool{}
	if len(refs) == 0 {
		return allowed, false, nil
	}
	agents := map[string]providerUsageAgent{}
	for start := 0; start < len(refs); start += 4096 {
		batch, err := readProviderUsageAgents(ctx, tx, current.organizationID, refs[start:min(start+4096, len(refs))])
		if err != nil {
			return nil, false, err
		}
		for ref, agent := range batch {
			agents[ref] = agent
		}
	}
	for _, agent := range agents {
		if !agent.Enabled || agent.State == "ARCHIVED" || agent.Provider != "openai-codex" {
			continue
		}
		_, target, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.view", agent.Ref)
		if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		canConfigure := repository.requireAccess(ctx, tx, current, "agent.manage", target)
		canLaunch := repository.requireAccess(ctx, tx, current, "agent.launch", target)
		for _, failure := range []error{canConfigure, canLaunch} {
			if failure != nil && !errors.Is(failure, errs.ErrForbidden) && !errors.Is(failure, errs.ErrNotFound) {
				return nil, false, failure
			}
		}
		if canConfigure != nil && canLaunch != nil {
			continue
		}
		for _, candidate := range agent.Candidates {
			allowed[candidate.AccountRef] = true
		}
		if agent.SelectedAccount != "" {
			allowed[agent.SelectedAccount] = true
		}
	}
	return allowed, false, nil
}

// providerFinancialCollector holds current lease time and prior observation ordering for one locked
// account/current credential. It is transaction-local database metadata, not an authorization grant.
// providerFinancialCollector хранит current lease time и prior observation ordering одного locked
// account/current credential. Это transaction-local DB metadata, а не authorization grant.
type providerFinancialCollector struct {
	createdAt         time.Time
	previousCreatedAt *time.Time
	previousLease     string
	previousSequence  int64
	previous          []byte
}

// readProviderFinancialCollector locks and reads the exact current financial account through live
// lease/revision/node/turn/session/credential pins. No row is Forbidden, other scan errors Unavailable;
// callers retain root/deadline/actor checks, ordering/replay decisions, writes and transaction commit.
// readProviderFinancialCollector блокирует и читает exact current financial account через live
// lease/revision/node/turn/session/credential pins. No row даёт Forbidden, прочие scan errors —
// Unavailable; root/deadline/actor checks, ordering/replay, writes и commit остаются у callers.
func readProviderFinancialCollector(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any) (providerFinancialCollector, error) {
	var result providerFinancialCollector
	if err := tx.QueryRow(ctx, queryProviderFinancialGuard, pgx.StrictNamedArgs{
		"runtime_revision_id": lease["runtimeRevisionID"], "organization_id": current.organizationID,
		"lease_id": lease["leaseID"], "root_run_id": lease["rootRunID"],
	}).Scan(&result.createdAt, &result.previousCreatedAt, &result.previousLease, &result.previousSequence, &result.previous); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return providerFinancialCollector{}, errs.ErrForbidden
		}
		return providerFinancialCollector{}, errs.ErrUnavailable
	}
	return result, nil
}
