package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// testWorkspaceProfileOffRejectsMismatchedRow uses a rolled-back owner-only counterfactual to prove that an existing V2 row cannot become an empty legacy workspace.
// The original workspace and second agent are created by domain commands; the guard is re-enabled before runtime admission is inspected, and no live database is used.
// testWorkspaceProfileOffRejectsMismatchedRow использует откатываемую owner-only подмену, доказывая запрет пустого legacy-каталога при существующей V2 row.
// Исходный workspace и второй agent созданы domain commands; guard включается до проверки runtime admission, живая БД не используется.
func testWorkspaceProfileOffRejectsMismatchedRow(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, projectRef, workspaceRef string) {
	t.Helper()
	other := createLifecycleAgent(t, ctx, service, owner, projectRef, "workspace-scope-other-agent", "Другой разработчик")
	previous := repository.roleImages.WorkspaceStorageProfile
	repository.roleImages.WorkspaceStorageProfile = ""
	defer func() { repository.roleImages.WorkspaceStorageProfile = previous }()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin isolated workspace mismatch", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	var current scope
	var candidate claimableExecution
	if err := tx.QueryRow(ctx, `SELECT organization_id::text,project_id::text,session_id::text,agent_id::text
  FROM control_plane.session_workspace_storage WHERE ref=$1`, workspaceRef).Scan(&current.organizationID, &candidate.projectID, &candidate.sessionID, &candidate.agentID); err != nil {
		t.Fatal("read original workspace scope", err)
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE control_plane.session_workspace_storage DISABLE TRIGGER session_workspace_storage_guard`); err != nil {
		t.Fatal("prepare disposable workspace counterfactual", err)
	}
	changed, err := tx.Exec(ctx, `UPDATE control_plane.session_workspace_storage SET agent_id=(
  SELECT id FROM control_plane.agents WHERE organization_id=$1::uuid AND ref=$2)
  WHERE organization_id=$1::uuid AND ref=$3`, current.organizationID, other.Ref, workspaceRef)
	if err != nil || changed.RowsAffected() != 1 {
		t.Fatal("set exact disposable mismatch", err)
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE control_plane.session_workspace_storage ENABLE TRIGGER session_workspace_storage_guard`); err != nil {
		t.Fatal("restore workspace guard before admission", err)
	}
	binding, ready, err := repository.prepareSessionWorkspaceTx(ctx, tx, current, candidate)
	if binding != nil || ready || !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("mismatched existing V2 row admitted legacy work: binding=%t ready=%t conflict=%t", binding != nil, ready, errors.Is(err, errs.ErrConflict))
	}
}
