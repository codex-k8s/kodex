package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_catalog__bind_session.sql
var queryRuntimeCatalogBindSession string

//go:embed sql/runtime_catalog__session_account.sql
var queryRuntimeCatalogSessionAccount string

//go:embed sql/runtime_catalog__read_session_binding.sql
var queryRuntimeCatalogReadSessionBinding string

type sessionCatalogBinding struct {
	CatalogRevision, CatalogDigest, Provider string
	PolicyID, PolicyRef, PolicyDigest        string
	PolicyVersion                            int64
	Models                                   []platformrepo.ProviderModelCatalogRecord
}

func bindSessionModelCatalog(ctx context.Context, tx pgx.Tx, organizationID, sessionID, agentRef string) error {
	var accountRef string
	if err := tx.QueryRow(ctx, queryRuntimeCatalogSessionAccount, sessionID, organizationID).Scan(&accountRef); err != nil {
		return errs.ErrConflict
	}
	configuration, overlay, err := readRuntimeCatalogConfiguration(ctx, tx, organizationID, agentRef, "")
	if err != nil {
		return err
	}
	selected := []entity.ProviderAccountCandidate{}
	for _, candidate := range configuration.ProviderPolicy.AccountCandidates {
		if candidate.AccountRef == accountRef {
			selected = append(selected, candidate)
		}
	}
	if len(selected) != 1 {
		return errs.ErrConflict
	}
	if _, _, err := validateRuntimeCatalogCandidates(ctx, tx, scope{organizationID: organizationID}, configuration.Provider, configuration.Model, overlay, selected, false); err != nil {
		return err
	}
	catalog, err := readModelCatalogTx(ctx, tx, scope{organizationID: organizationID}, configuration.Provider, accountRef)
	if err != nil {
		return err
	}
	models := []platformrepo.ProviderModelCatalogRecord{}
	for _, model := range catalog.Models {
		if !model.Available {
			return errs.ErrConflict
		}
		models = append(models, platformrepo.ProviderModelCatalogRecord{ID: model.ID, DefaultReasoningEffort: model.DefaultReasoningEffort, ReasoningEfforts: model.ReasoningEfforts})
	}
	result, err := tx.Exec(ctx, queryRuntimeCatalogBindSession, sessionID, organizationID, agentRef, catalog.Revision, catalog.Digest, asJSON(models))
	if err != nil {
		return errs.ErrUnavailable
	}
	if result.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

func readSessionModelCatalog(ctx context.Context, tx pgx.Tx, organizationID, sessionID, accountRef string) (sessionCatalogBinding, error) {
	var binding sessionCatalogBinding
	var raw []byte
	if err := tx.QueryRow(ctx, queryRuntimeCatalogReadSessionBinding, sessionID, organizationID, accountRef).Scan(&binding.CatalogRevision, &binding.CatalogDigest, &raw, &binding.Provider, &binding.PolicyID, &binding.PolicyRef, &binding.PolicyVersion, &binding.PolicyDigest); err != nil {
		return binding, errs.ErrConflict
	}
	if decodeStrict(raw, &binding.Models) != nil {
		return binding, errs.ErrUnavailable
	}
	return binding, nil
}

func checkedSessionModelCatalog(ctx context.Context, tx pgx.Tx, organizationID, sessionID, accountRef string, configuration entity.AgentRuntimeConfiguration, overlay string) (entity.ProviderAccountCandidate, *sessionCatalogBinding, error) {
	binding, err := readSessionModelCatalog(ctx, tx, organizationID, sessionID, accountRef)
	if err != nil {
		return entity.ProviderAccountCandidate{}, nil, err
	}
	selected := []entity.ProviderAccountCandidate{}
	var retained *sessionCatalogBinding
	for _, candidate := range configuration.ProviderPolicy.AccountCandidates {
		if candidate.AccountRef == accountRef {
			selected = append(selected, candidate)
		}
	}
	if len(selected) == 0 && binding.Provider == configuration.Provider {
		for _, model := range binding.Models {
			if model.ID == configuration.Model {
				selected = append(selected, entity.ProviderAccountCandidate{AccountRef: accountRef, Weight: 1, ProviderDefinitionKey: binding.Provider, CatalogRevision: binding.CatalogRevision, CatalogDigest: binding.CatalogDigest, DefaultReasoningEffort: model.DefaultReasoningEffort})
				retained = &binding
				break
			}
		}
	}
	if len(selected) != 1 {
		return entity.ProviderAccountCandidate{}, nil, errs.ErrConflict
	}
	verified, _, err := validateRuntimeCatalogCandidates(ctx, tx, scope{organizationID: organizationID}, configuration.Provider, configuration.Model, overlay, selected, false)
	if err != nil {
		return entity.ProviderAccountCandidate{}, nil, err
	}
	return verified[0], retained, nil
}

func validateSessionRuntimeCatalog(ctx context.Context, tx pgx.Tx, organizationID, sessionID, agentRef string) error {
	var accountRef string
	if err := tx.QueryRow(ctx, queryRuntimeCatalogSessionAccount, sessionID, organizationID).Scan(&accountRef); err != nil {
		return errs.ErrConflict
	}
	configuration, overlay, err := readRuntimeCatalogConfiguration(ctx, tx, organizationID, agentRef, "")
	if err != nil {
		return err
	}
	_, _, err = checkedSessionModelCatalog(ctx, tx, organizationID, sessionID, accountRef, configuration, overlay)
	return err
}
