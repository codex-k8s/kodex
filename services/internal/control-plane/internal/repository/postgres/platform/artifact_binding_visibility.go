package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/artifact_visible_binding_refs.sql
var queryArtifactVisibleBindingRefs string

func projectArtifactBindingRefs(ctx context.Context, runner queryRunner, current scope, artifact *entity.Artifact) error {
	if len(artifact.Bindings) == 0 {
		return nil
	}
	visible := map[string]bool{}
	for start := 0; start < len(artifact.Bindings); start += 256 {
		rows, err := runner.Query(ctx, queryArtifactVisibleBindingRefs, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "actor_id": current.actorID,
			"authority_project_id": current.authorityProjectID, "artifact_ref": artifact.Ref,
			"agent_refs": artifact.Bindings[start:min(start+256, len(artifact.Bindings))],
		})
		if err != nil {
			return errs.ErrUnavailable
		}
		for rows.Next() {
			var ref string
			if rows.Scan(&ref) != nil {
				rows.Close()
				return errs.ErrUnavailable
			}
			visible[ref] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return errs.ErrUnavailable
		}
	}
	kept := make([]string, 0, len(visible))
	for _, ref := range artifact.Bindings {
		if visible[ref] {
			kept = append(kept, ref)
		}
	}
	artifact.Bindings = kept
	return nil
}
