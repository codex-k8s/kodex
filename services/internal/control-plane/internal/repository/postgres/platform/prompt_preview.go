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

func (repository *Repository) GetPromptMaterializationSnapshot(ctx context.Context, principal value.Principal, targetKind, targetRef string) (entity.PromptMaterializationSnapshot, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.PromptMaterializationSnapshot{}, err
	}
	var raw []byte
	err = repository.pool.QueryRow(ctx, queryPromptPreviewSnapshot, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_platform_role": current.role,
		"actor_id": current.actorID, "target_kind": targetKind, "target_ref": targetRef,
	}).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.PromptMaterializationSnapshot{}, errs.ErrNotFound
	}
	var result entity.PromptMaterializationSnapshot
	if err != nil || json.Unmarshal(raw, &result) != nil || result.TemplateRef == "" || result.TemplateContent == "" {
		return entity.PromptMaterializationSnapshot{}, errs.ErrUnavailable
	}
	return result, nil
}
