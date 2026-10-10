package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/systemassistant"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_assistant_core.sql
var queryPromptAssistantCore string

// База разрешается только по server-owned профилю того же проекта/организации.
// Ни agent payload, ни owner template не могут объявить себя помощником.
// Текущая база закрепляется только в новом snapshot; история не перечитывается.
func (repository *Repository) hydrateAssistantCoreTx(ctx context.Context, tx pgx.Tx, current scope, snapshot *entity.PromptMaterializationSnapshot) error {
	snapshot.AssistantCore = nil
	var core entity.PromptAssistantCore
	var bound bool
	err := tx.QueryRow(ctx, queryPromptAssistantCore, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "agent_ref": snapshot.ContextPin.AgentRef, "project_ref": snapshot.ProjectRef,
	}).Scan(&core.Scope, &core.Ref, &core.Revision, &core.Digest, &core.Content, &bound)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if core.Scope == "" {
		return nil
	}
	digest := sha256.Sum256([]byte(systemassistant.CorePrompt()))
	if !bound || core.Ref == "" || core.Revision != systemassistant.CorePromptRevision ||
		core.Digest != hex.EncodeToString(digest[:]) || core.Content != systemassistant.CorePrompt() {
		return errs.ErrConflict
	}
	snapshot.AssistantCore = &core
	return nil
}
