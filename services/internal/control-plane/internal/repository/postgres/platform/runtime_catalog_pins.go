package platform

import (
	"context"
	_ "embed"
	"errors"
	"slices"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_catalog__read_configuration.sql
var queryRuntimeCatalogReadConfiguration string

//go:embed sql/runtime_catalog__lock_accounts.sql
var queryRuntimeCatalogLockAccounts string

//go:embed sql/runtime_catalog__bootstrap_accounts.sql
var queryRuntimeCatalogBootstrapAccounts string

//go:embed sql/runtime_catalog__lock_agent.sql
var queryRuntimeCatalogLockAgent string

// Bootstrap сохраняет существующий account pool, но отсутствие pin блокирует
// выполнение до отдельной публикации проверенного server catalog snapshot.
func bootstrapUnpinnedCatalogCandidates(ctx context.Context, tx pgx.Tx, organizationID, provider string) ([]entity.ProviderAccountCandidate, error) {
	rows, err := tx.Query(ctx, queryRuntimeCatalogBootstrapAccounts, organizationID, provider)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := []entity.ProviderAccountCandidate{}
	for rows.Next() {
		candidate := entity.ProviderAccountCandidate{Weight: 1}
		if rows.Scan(&candidate.AccountRef) != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, candidate)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	if len(result) == 0 {
		return nil, errs.ErrConflict
	}
	return result, nil
}

func validRuntimeCatalogPin(candidate entity.ProviderAccountCandidate) bool {
	if len(candidate.CatalogDigest) != 64 || candidate.CatalogRevision != "mcat_"+candidate.CatalogDigest || !validStableKey(candidate.ProviderDefinitionKey) {
		return false
	}
	for _, character := range candidate.CatalogDigest {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

// validateRuntimeCatalogCandidates читает тот же snapshot, что публичный
// каталог, в caller owner-транзакции. Account locks закрывают TOCTOU с revoke.
func validateRuntimeCatalogCandidates(ctx context.Context, tx pgx.Tx, current scope, provider, model, overlay string, candidates []entity.ProviderAccountCandidate, input bool) ([]entity.ProviderAccountCandidate, []string, error) {
	if len(candidates) == 0 {
		return nil, nil, errs.ErrConflict
	}
	result := slices.Clone(candidates)
	slices.SortFunc(result, func(a, b entity.ProviderAccountCandidate) int { return strings.Compare(a.AccountRef, b.AccountRef) })
	efforts := []string{}
	for index := range result {
		candidate := &result[index]
		if !validRuntimeCatalogPin(*candidate) || candidate.ProviderDefinitionKey != provider || input && candidate.DefaultReasoningEffort != "" {
			return nil, nil, errs.ErrInvalid
		}
		var ref string
		if err := tx.QueryRow(ctx, queryRuntimeCatalogLockAccounts, current.organizationID, candidate.AccountRef, provider).Scan(&ref); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil, errs.ErrConflict
			}
			return nil, nil, errs.ErrUnavailable
		}
		catalog, err := readModelCatalogTx(ctx, tx, current, provider, candidate.AccountRef)
		if err != nil {
			return nil, nil, err
		}
		if catalog.Revision != candidate.CatalogRevision || catalog.Digest != candidate.CatalogDigest {
			return nil, nil, errs.ErrVersionMismatch
		}
		found := false
		for _, capability := range catalog.Models {
			if capability.ID != model {
				continue
			}
			validDefault := len(capability.ReasoningEfforts) == 0 && capability.DefaultReasoningEffort == "" || slices.Contains(capability.ReasoningEfforts, capability.DefaultReasoningEffort)
			if !capability.Available || !slices.Contains(capability.EligibleProviderAccountRefs, candidate.AccountRef) || !validDefault {
				return nil, nil, errs.ErrConflict
			}
			if !input && candidate.DefaultReasoningEffort != capability.DefaultReasoningEffort {
				return nil, nil, errs.ErrConflict
			}
			candidate.DefaultReasoningEffort = capability.DefaultReasoningEffort
			modelEfforts := append([]string{}, capability.ReasoningEfforts...)
			if len(runtimecontract.DiagnoseConfigOverlay(overlay, modelEfforts)) != 0 {
				return nil, nil, errs.ErrConflict
			}
			if index == 0 {
				efforts = modelEfforts
			} else {
				efforts = slices.DeleteFunc(efforts, func(effort string) bool { return !slices.Contains(capability.ReasoningEfforts, effort) })
			}
			found = true
			break
		}
		if !found {
			return nil, nil, errs.ErrConflict
		}
	}
	return result, efforts, nil
}

func readRuntimeCatalogConfiguration(ctx context.Context, tx pgx.Tx, organizationID, agentRef, configID string) (entity.AgentRuntimeConfiguration, string, error) {
	var config entity.AgentRuntimeConfiguration
	var raw []byte
	var overlay string
	if err := tx.QueryRow(ctx, queryRuntimeCatalogReadConfiguration, organizationID, agentRef, configID).Scan(&config.Provider, &config.Model, &raw, &overlay); err != nil {
		return config, "", errs.ErrUnavailable
	}
	if decodeStrict(raw, &config.ProviderPolicy.AccountCandidates) != nil {
		return config, "", errs.ErrUnavailable
	}
	return config, overlay, nil
}

// captureRuntimeCatalogPins вызывают только server-owned bootstrap/reconcile.
// Пользовательская публикация обязана передать ожидаемый snapshot отдельно.
func captureRuntimeCatalogPins(ctx context.Context, tx pgx.Tx, current scope, provider, model string, candidates []entity.ProviderAccountCandidate) ([]entity.ProviderAccountCandidate, error) {
	if candidates == nil {
		catalog, err := readModelCatalogTx(ctx, tx, current, provider, "")
		if err != nil {
			return nil, err
		}
		for _, capability := range catalog.Models {
			if capability.ID != model || !capability.Available {
				continue
			}
			for _, ref := range capability.EligibleProviderAccountRefs {
				candidates = append(candidates, entity.ProviderAccountCandidate{AccountRef: ref, Weight: 1})
			}
			break
		}
	}
	if len(candidates) == 0 || len(candidates) > 128 {
		return nil, errs.ErrConflict
	}
	eligible := make([]entity.ProviderAccountCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		catalog, err := readModelCatalogTx(ctx, tx, current, provider, candidate.AccountRef)
		if err != nil {
			return nil, err
		}
		available := false
		for _, capability := range catalog.Models {
			if capability.ID == model && capability.Available {
				available = true
				break
			}
		}
		if !available {
			continue
		}
		candidate.ProviderDefinitionKey = provider
		candidate.CatalogRevision, candidate.CatalogDigest = catalog.Revision, catalog.Digest
		candidate.DefaultReasoningEffort = ""
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return nil, errs.ErrConflict
	}
	result, _, err := validateRuntimeCatalogCandidates(ctx, tx, current, provider, model, "", eligible, true)
	return result, err
}
