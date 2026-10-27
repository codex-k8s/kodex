package platform

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// GetPromptMaterializationSnapshot resolves principal scope before validating RUN/SESSION target
// syntax, then reads the latest stored promptSnapshot for that tenant/target in ReadOnly
// RepeatableRead. It rejects missing, undecodable or empty-template data, validates platform
// instructions and checks run.view on the selected authoritative run before commit. No rows/denial
// is NotFound, read/decode/commit failure is Unavailable. It returns historical materialization
// without refreshing current bindings, recomputing the stored snapshot or starting execution.
// GetPromptMaterializationSnapshot разрешает principal scope до проверки синтаксиса RUN/SESSION
// target, затем читает последний сохранённый promptSnapshot точных tenant/target в ReadOnly
// RepeatableRead. Отклоняет отсутствующие, неразбираемые данные или пустой template, проверяет
// platform instructions и run.view на выбранном авторитетном run до commit. Отсутствие
// строки/доступа даёт NotFound, read/decode/commit failure — Unavailable. Возвращает историческую
// materialization без обновления текущих bindings, пересчёта сохранённого snapshot или запуска
// выполнения.
func (repository *Repository) GetPromptMaterializationSnapshot(ctx context.Context, principal value.Principal, targetKind, targetRef string) (entity.PromptMaterializationSnapshot, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	var raw []byte
	if targetKind != "RUN" && targetKind != "SESSION" || targetRef == "" {
		return entity.PromptMaterializationSnapshot{}, errs.ErrInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var runRef string
	err = tx.QueryRow(ctx, queryPromptPreviewSnapshot, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "target_kind": targetKind, "target_ref": targetRef,
	}).Scan(&raw, &runRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
	}
	var result entity.PromptMaterializationSnapshot
	if err != nil || json.Unmarshal(raw, &result) != nil || result.TemplateRef == "" || result.TemplateContent == "" {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	if err := validatePlatformInstructions(result); err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	_, target, err := repository.resolveCommandTarget(ctx, tx, current, "run.view", "RUN", runRef, "")
	if err != nil || repository.requireAccess(ctx, tx, current, "run.view", target) != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	return result, nil
}
