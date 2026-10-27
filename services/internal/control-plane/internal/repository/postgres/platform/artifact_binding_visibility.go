package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryArtifactVisibleBindingRefs embeds exact tenant/artifact knowledge bindings to same-project
// nonsystem agents visible through agent.view and authority-project scope. It returns only allowed
// agent refs and does not replace artifact admission or authorize binding mutations.
// queryArtifactVisibleBindingRefs встраивает exact tenant/artifact knowledge bindings к
// same-project nonsystem agents, видимым через agent.view и authority-project scope. Возвращает
// только allowed agent refs, не заменяет artifact admission и не разрешает binding mutations.
//
//go:embed sql/artifact_visible_binding_refs.sql
var queryArtifactVisibleBindingRefs string

// projectArtifactBindingRefs queries bound-agent visibility in batches of256 before replacing the
// nonnil artifact binding slice in original order, preserving duplicate visible refs. Empty input
// is unchanged; backend/iteration failure leaves the slice unfiltered and must prevent result
// delivery. It does not commit or mutate actual bindings.
// projectArtifactBindingRefs читает bound-agent visibility пакетами256 до замены binding slice
// nonnil artifact в original order с сохранением duplicate visible refs. Empty input не меняется;
// backend/iteration failure оставляет slice нефильтрованным и должна блокировать выдачу result. Не
// выполняет commit и не меняет actual bindings.
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
