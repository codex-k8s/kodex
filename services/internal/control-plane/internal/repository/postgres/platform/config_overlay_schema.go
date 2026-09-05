package platform

import (
	"context"
	_ "embed"
	"slices"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_catalog__read_overlay_metadata.sql
var queryRuntimeCatalogReadOverlayMetadata string

//go:embed sql/runtime_catalog__save_overlay_metadata.sql
var queryRuntimeCatalogSaveOverlayMetadata string

func runtimeOverlaySchema(ctx context.Context, tx pgx.Tx, current scope, config entity.AgentRuntimeConfiguration) (runtimecontract.ConfigOverlaySchema, error) {
	efforts := []string{}
	defaultEffort := ""
	for index, candidate := range config.ProviderPolicy.AccountCandidates {
		catalog, err := readModelCatalogTx(ctx, tx, current, config.Provider, candidate.AccountRef)
		if err != nil {
			return runtimecontract.ConfigOverlaySchema{}, err
		}
		var available []string
		for _, model := range catalog.Models {
			if model.ID == config.Model && model.Available {
				available = model.ReasoningEfforts
				if index == 0 {
					defaultEffort = model.DefaultReasoningEffort
				} else if defaultEffort != model.DefaultReasoningEffort {
					defaultEffort = ""
				}
				break
			}
		}
		if index == 0 {
			efforts = append([]string{}, available...)
		} else {
			efforts = slices.DeleteFunc(efforts, func(effort string) bool { return !slices.Contains(available, effort) })
		}
	}
	return runtimecontract.OverlaySchema(efforts, defaultEffort), nil
}

func populateRuntimeOverlaySchema(ctx context.Context, tx pgx.Tx, current scope, view *entity.AgentRuntimeConfigurationView) error {
	schema, err := runtimeOverlaySchema(ctx, tx, current, view.Configuration)
	if err != nil {
		return err
	}
	view.OverlaySchema = schema
	refs := []string{view.PublishedOverlay.Ref}
	if view.DraftOverlay != nil {
		refs = append(refs, view.DraftOverlay.Ref)
	}
	rows, err := tx.Query(ctx, queryRuntimeCatalogReadOverlayMetadata, current.organizationID, refs)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var ref, revision, digest string
		var raw []byte
		if rows.Scan(&ref, &raw, &revision, &digest) != nil {
			return errs.ErrUnavailable
		}
		var diagnostics []runtimecontract.ConfigOverlayDiagnostic
		if decodeStrict(raw, &diagnostics) != nil {
			return errs.ErrUnavailable
		}
		target := &view.PublishedOverlay
		if view.DraftOverlay != nil && ref == view.DraftOverlay.Ref {
			target = view.DraftOverlay
		}
		target.SchemaRevision, target.SchemaDigest, target.Diagnostics = revision, digest, diagnostics
	}
	return rows.Err()
}

func saveRuntimeOverlayDiagnostics(ctx context.Context, tx pgx.Tx, current scope, view *entity.AgentRuntimeConfigurationView, draft bool) error {
	target := &view.PublishedOverlay
	if draft {
		if view.DraftOverlay == nil {
			return errs.ErrConflict
		}
		target = view.DraftOverlay
	}
	schema := view.OverlaySchema
	diagnostics := runtimecontract.DiagnoseConfigOverlay(target.Content, schema.Fields[0].AllowedValues)
	if diagnostics == nil {
		diagnostics = []runtimecontract.ConfigOverlayDiagnostic{}
	}
	if !draft && len(diagnostics) != 0 {
		return errs.ErrConflict
	}
	if !draft {
		if target.SchemaRevision != schema.Revision || target.SchemaDigest != schema.Digest || len(target.Diagnostics) != 0 {
			return errs.ErrUnavailable
		}
		return nil
	}
	if _, err := tx.Exec(ctx, queryRuntimeCatalogSaveOverlayMetadata, current.organizationID, target.Ref, asJSON(diagnostics), schema.Revision, schema.Digest); err != nil {
		return errs.ErrUnavailable
	}
	target.SchemaRevision, target.SchemaDigest, target.Diagnostics = schema.Revision, schema.Digest, diagnostics
	return nil
}
