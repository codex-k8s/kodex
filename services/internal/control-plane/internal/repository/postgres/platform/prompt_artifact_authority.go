package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_claim_files_optin.sql
var queryPromptClaimFilesOptIn string

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
