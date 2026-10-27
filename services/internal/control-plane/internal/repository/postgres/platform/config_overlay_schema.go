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

// queryRuntimeCatalogReadOverlayMetadata embeds organization-scoped metadata lookup for explicit
// overlay refs. Returned diagnostics and schema pins describe stored revisions, not an
// authorization decision.
// queryRuntimeCatalogReadOverlayMetadata встраивает чтение metadata явных overlay refs в
// организации. Возвращённые diagnostics и schema pins описывают сохранённые ревизии, а не решение
// авторизации.
//
//go:embed sql/runtime_catalog__read_overlay_metadata.sql
var queryRuntimeCatalogReadOverlayMetadata string

// queryRuntimeCatalogSaveOverlayMetadata embeds diagnostics/schema-pin updates only for mutable
// DRAFT/VALID/INVALID overlays in the organization. Published content is not rewritten; the caller
// owns transaction and row-count policy.
// queryRuntimeCatalogSaveOverlayMetadata встраивает обновление diagnostics/schema pins только
// изменяемых DRAFT/VALID/INVALID overlay организации. Опубликованное содержимое не переписывается;
// caller отвечает за транзакцию и политику числа строк.
//
//go:embed sql/runtime_catalog__save_overlay_metadata.sql
var queryRuntimeCatalogSaveOverlayMetadata string

// runtimeOverlaySchema intersects reasoning efforts across candidate account catalogs for the
// configured available model. Matching models update the shared default; absent models contribute
// an empty effort set. Catalog failures propagate. It delegates schema construction without
// probing providers or granting account access.
// runtimeOverlaySchema пересекает reasoning efforts каталогов accounts-кандидатов для настроенной
// доступной модели. Найденные модели уточняют общий default, отсутствие модели даёт пустой effort
// set. Ошибки каталогов передаются выше. Передаёт построение schema без provider probes и выдачи
// доступа к accounts.
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

// populateRuntimeOverlaySchema first assigns the computed schema, then reads published/draft
// metadata and strictly decodes diagnostics before mutating matching view targets.
// Query/scan/decode failures are Unavailable, final rows.Err propagates; it neither requires all
// metadata rows nor rolls back prior in-memory changes.
// populateRuntimeOverlaySchema сначала назначает вычисленную schema, затем читает published/draft
// metadata и строго декодирует diagnostics перед изменением targets представления. Ошибки
// query/scan/decode дают Unavailable, итоговый rows.Err передаётся выше; не требует наличия всех
// metadata rows и не откатывает прежние изменения в памяти.
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

// saveRuntimeOverlayDiagnostics diagnoses the selected overlay against explicit allowed reasoning
// values. Published overlays must already have matching schema pins and no diagnostics; drafts
// persist even nonempty diagnostics and update the view after Exec. A missing draft conflicts;
// this function does not commit or require exactly one updated row.
// saveRuntimeOverlayDiagnostics проверяет выбранный overlay по явному набору допустимых reasoning
// values. Published overlay уже должен иметь matching schema pins и пустые diagnostics; draft
// сохраняет также непустые diagnostics и меняет view после Exec. Отсутствие draft даёт Conflict;
// функция не делает Commit и не требует ровно одной изменённой строки.
func saveRuntimeOverlayDiagnostics(ctx context.Context, tx pgx.Tx, current scope, view *entity.AgentRuntimeConfigurationView, draft bool) error {
	target := &view.PublishedOverlay
	if draft {
		if view.DraftOverlay == nil {
			return errs.ErrConflict
		}
		target = view.DraftOverlay
	}
	schema := view.OverlaySchema
	efforts, err := runtimeOverlayReasoningValues(schema)
	if err != nil {
		return err
	}
	diagnostics := runtimecontract.DiagnoseConfigOverlay(target.Content, efforts)
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

// runtimeOverlayReasoningValues finds exactly one string-valued model_reasoning_effort field
// regardless of presentation order. Missing/duplicate/wrong-type fields are Unavailable; an
// explicit nonnil empty values slice preserves a closed compatibility restriction rather than
// disabling diagnosis.
// runtimeOverlayReasoningValues находит ровно одно строковое поле model_reasoning_effort
// независимо от presentation-порядка. Отсутствующее, повторное либо неверно типизированное поле
// даёт Unavailable; явный nonnil пустой slice values сохраняет закрытое ограничение совместимости
// вместо отключения проверки.
func runtimeOverlayReasoningValues(schema runtimecontract.ConfigOverlaySchema) ([]string, error) {
	var values []string
	found := false
	for _, field := range schema.Fields {
		if field.Key != "model_reasoning_effort" {
			continue
		}
		if found || field.ValueType != "string" {
			return nil, errs.ErrUnavailable
		}
		found = true
		// nil в Diagnose означает отсутствие compatibility проверки; пустой
		// подтверждённый набор обязан оставаться явным закрытым ограничением.
		values = append([]string{}, field.AllowedValues...)
	}
	if !found {
		return nil, errs.ErrUnavailable
	}
	return values, nil
}
