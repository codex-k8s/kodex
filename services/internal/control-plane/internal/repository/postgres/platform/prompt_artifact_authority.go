package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryPromptClaimFilesOptIn embeds whether the exact tenant/project agent has active knowledge
// input or the session has any finalized attachment set. It is a file-context presence signal,
// without actor access or content admission checks.
// queryPromptClaimFilesOptIn встраивает наличие active knowledge input exact tenant/project agent
// либо любого finalized attachment set session. Это file-context presence signal без actor access
// или content admission checks.
//
//go:embed sql/prompt_claim_files_optin.sql
var queryPromptClaimFilesOptIn string

// requireRuntimeFileOptIn reads the exact file-context presence signal and returns
// CapabilityRequired when files are selected, leaving capability enforcement to the enclosing
// runtime path. Backend failure is Unavailable; an absent signal is not general artifact access or
// a grant to read files.
// requireRuntimeFileOptIn читает exact file-context presence signal и возвращает
// CapabilityRequired при selected files, оставляя capability enforcement enclosing runtime path.
// Backend failure даёт Unavailable; отсутствие signal не является general artifact access либо
// grant чтения files.
func (repository *Repository) requireRuntimeFileOptIn(ctx context.Context, tx pgx.Tx, current scope, projectID, sessionID, agentRef string) error {
	var selected bool
	if err := tx.QueryRow(ctx, queryPromptClaimFilesOptIn, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID, "session_id": sessionID, "agent_ref": agentRef}).Scan(&selected); err != nil {
		return errs.ErrUnavailable
	}
	if selected {
		return errs.ErrCapabilityRequired
	}
	return nil
}

// authorizePromptArtifactsTx bounds refs at2048, rejects empty refs and deduplicates permission
// lookups while resolving each exact tenant/project artifact target. Both artifact.view and
// artifact.download must pass; resolution/access errors are hidden as NotFound. It checks
// permissions only, without reading bytes, evaluating scan here or committing.
// authorizePromptArtifactsTx ограничивает refs числом2048, отклоняет empty refs и устраняет повтор
// permission lookups, разрешая каждый exact tenant/project artifact target. Обязательны
// artifact.view и artifact.download; resolution/access errors скрываются как NotFound. Проверяет
// только permissions без чтения bytes, проверки scan здесь либо commit.
func (repository *Repository) authorizePromptArtifactsTx(ctx context.Context, tx pgx.Tx, current scope, projectRef string, refs []string) error {
	if len(refs) > 2048 {
		return errs.ErrConflict
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref == "" {
			return errs.ErrConflict
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: projectRef, ResourceKind: "ARTIFACT", ResourceRef: ref})
		if err != nil || repository.requireAccess(ctx, tx, current, "artifact.view", target) != nil || repository.requireAccess(ctx, tx, current, "artifact.download", target) != nil {
			return errs.ErrNotFound
		}
	}
	return nil
}
