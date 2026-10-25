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

// queryPromptAssistantCore embeds server-owned SYSTEM/PROJECT assistant-core selection under
// organization/agent and exact project rules. SYSTEM remains organization-owned even with an
// active same-tenant project screen context. The projection includes current runtime core pins,
// published core content and its active instruction binding; ordinary agents return an empty core
// scope.
// queryPromptAssistantCore встраивает server-owned выбор SYSTEM/PROJECT assistant core внутри
// organization/agent и точных project rules. SYSTEM остаётся собственностью организации даже при
// активном project screen context того же tenant. Проекция содержит текущие runtime core pins,
// опубликованный core content и его active instruction binding; обычные агенты возвращают пустой
// core scope.
//
//go:embed sql/prompt_assistant_core.sql
var queryPromptAssistantCore string

// hydrateAssistantCoreTx clears any existing AssistantCore before reading the server-owned
// projection in the caller transaction. Missing agent is NotFound, SQL failure is Unavailable and
// an ordinary agent needs no core. Assistant cores require an active binding, nonempty ref and
// exact compiled revision/content/SHA-256, otherwise Conflict. Only the new in-memory snapshot
// receives the core; historical snapshots and caller-supplied templates cannot mint an assistant
// profile.
// hydrateAssistantCoreTx очищает прежний AssistantCore перед чтением server-owned projection в
// транзакции вызывающего. Отсутствующий agent даёт NotFound, SQL-отказ — Unavailable, обычному
// agent core не нужен. Для assistant core требует active binding, непустой ref и точные compiled
// revision/content/SHA-256, иначе Conflict. Core получает только новый in-memory snapshot;
// исторические snapshots и templates вызывающего не создают assistant profile.
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
