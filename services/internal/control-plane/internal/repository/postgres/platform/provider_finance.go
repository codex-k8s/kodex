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

//go:embed sql/provider_financial_publish.sql
var queryProviderFinancialPublish string

//go:embed sql/provider_financial_guard.sql
var queryProviderFinancialGuard string

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
	var createdAt time.Time
	var previousCreatedAt *time.Time
	var previousLease string
	var previousSequence int64
	var previous []byte
	if err := tx.QueryRow(ctx, queryProviderFinancialGuard, pgx.StrictNamedArgs{
		"runtime_revision_id": lease["runtimeRevisionID"], "organization_id": current.organizationID,
		"lease_id": lease["leaseID"], "root_run_id": lease["rootRunID"],
	}).Scan(&createdAt, &previousCreatedAt, &previousLease, &previousSequence, &previous); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrForbidden
		}
		return errs.ErrUnavailable
	}
	accepted, err := acceptFinancialCollector(stringMap(lease, "leaseID"), createdAt, observation.Sequence, asJSON(observation), previousLease, previousCreatedAt, previousSequence, previous)
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

//go:embed sql/provider_financial_agents.sql
var queryProviderFinancialAgents string

// Eligibility вычисляется до финансовых counts/rank. Project-scoped чтение
// использует только accounts текущих configs доступных configure/launch agents.
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
