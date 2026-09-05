package platform

import (
	"context"
	_ "embed"
	"errors"
	"slices"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/artifact_effective_actions.sql
var queryArtifactEffectiveActions string

//go:embed sql/artifact_effective_references.sql
var queryArtifactEffectiveReferences string

// projectArtifactResults повторно разрешает ссылки из read model и старого receipt
// в текущей транзакции. Сохранённый ответ не является источником полномочий.
func projectArtifactResults(ctx context.Context, runner queryRunner, current scope, results ...*command.Result) error {
	var references []*[]string
	var singles []*string
	var artifacts []**entity.Artifact
	for _, result := range results {
		if result == nil {
			continue
		}
		artifacts = append(artifacts, &result.Artifact)
		if result.Run != nil {
			references = append(references, &result.Run.ArtifactRefs)
		}
		if result.Graph != nil {
			for index := range result.Graph.Nodes {
				references = append(references, &result.Graph.Nodes[index].ArtifactRefs)
			}
		}
		if result.Event != nil {
			singles = append(singles, &result.Event.ArtifactRef)
			artifacts = append(artifacts, &result.Event.Delta.Artifact)
			if result.Event.Delta.Run != nil {
				references = append(references, &result.Event.Delta.Run.ArtifactRefs)
			}
			if result.Event.Delta.Node != nil {
				references = append(references, &result.Event.Delta.Node.ArtifactRefs)
			}
		}
	}
	refs := make([]string, 0)
	for _, values := range references {
		refs = append(refs, (*values)...)
	}
	for _, ref := range singles {
		if *ref != "" {
			refs = append(refs, *ref)
		}
	}
	for _, artifact := range artifacts {
		if *artifact != nil {
			refs = append(refs, (*artifact).Ref)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	slices.Sort(refs)
	refs = slices.Compact(refs)
	type eligibleArtifact struct {
		version int64
		actions []string
	}
	visible := make(map[string]eligibleArtifact, len(refs))
	// Размер одного запроса ограничен независимо от числа ссылок в истории Run.
	for start := 0; start < len(refs); start += 256 {
		rows, err := runner.Query(ctx, queryArtifactEffectiveReferences, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID,
			"artifact_refs": refs[start:min(start+256, len(refs))],
		})
		if err != nil {
			return errs.ErrUnavailable
		}
		for rows.Next() {
			var ref, lifecycle, scan string
			var item eligibleArtifact
			var permissions []string
			if err := rows.Scan(&ref, &item.version, &lifecycle, &scan, &permissions); err != nil {
				rows.Close()
				return errs.ErrUnavailable
			}
			item.actions = permittedArtifactActions(scan, lifecycle, func(permission string) bool { return contains(permissions, permission) })
			visible[ref] = item
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return errs.ErrUnavailable
		}
	}
	for _, values := range references {
		kept := make([]string, 0, len(*values))
		for _, ref := range *values {
			if _, ok := visible[ref]; ok {
				kept = append(kept, ref)
			}
		}
		*values = kept
	}
	for _, ref := range singles {
		if _, ok := visible[*ref]; !ok {
			*ref = ""
		}
	}
	for _, artifact := range artifacts {
		if *artifact == nil {
			continue
		}
		item, ok := visible[(*artifact).Ref]
		if !ok {
			*artifact = nil
			continue
		}
		(*artifact).NextActions = []string{}
		if (*artifact).Version == item.version {
			(*artifact).NextActions = slices.Clone(item.actions)
		}
	}
	return nil
}

func permittedArtifactActions(scanState, lifecycle string, allowed func(string) bool) []string {
	actions := make([]string, 0, 3)
	switch lifecycle {
	case "DELETED":
		if allowed("artifact.restore") {
			actions = append(actions, "RESTORE")
		}
		if allowed("artifact.purge") {
			actions = append(actions, "PURGE")
		}
	case "ACTIVE":
		if scanState == "CLEAN" {
			if allowed("artifact.download") {
				actions = append(actions, "DOWNLOAD")
			}
			if allowed("artifact.bind") {
				actions = append(actions, "BIND")
			}
		}
		if allowed("artifact.delete") {
			actions = append(actions, "DELETE")
		}
	}
	return actions
}

func projectArtifactEligibility(ctx context.Context, runner queryRunner, current scope, artifact *entity.Artifact) error {
	var permissions []string
	var version int64
	var lifecycle, scan string
	if err := runner.QueryRow(ctx, queryArtifactEffectiveActions, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID, "artifact_ref": artifact.Ref,
	}).Scan(&version, &lifecycle, &scan, &permissions); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	} else if err != nil {
		return errs.ErrUnavailable
	}
	artifact.NextActions = []string{}
	if artifact.Version == version {
		artifact.NextActions = permittedArtifactActions(scan, lifecycle, func(permission string) bool { return contains(permissions, permission) })
	}
	return nil
}
