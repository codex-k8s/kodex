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

// queryArtifactEffectiveActions embeds exact tenant/project-scoped artifact.view eligibility and
// the current version/lifecycle/scan plus individually allowed action permissions. It is an
// authoritative action projection, not permission to use historical bytes without revision checks.
// queryArtifactEffectiveActions встраивает exact tenant/project-scoped artifact.view eligibility и
// current version/lifecycle/scan с отдельно допустимыми action permissions. Это authoritative
// action projection, а не permission использовать historical bytes без revision checks.
//
//go:embed sql/artifact_effective_actions.sql
var queryArtifactEffectiveActions string

// queryArtifactEffectiveReferences embeds batch artifact.view eligibility and current
// action/version data for exact requested refs inside tenant/authority-project scope. The caller
// preserves original reference order and separately validates immutable revision pins.
// queryArtifactEffectiveReferences встраивает batch artifact.view eligibility и current
// action/version data для exact requested refs внутри tenant/authority-project scope. Caller
// сохраняет original reference order и отдельно проверяет immutable revision pins.
//
//go:embed sql/artifact_effective_references.sql
var queryArtifactEffectiveReferences string

// projectArtifactResults collects artifact pointers/refs from result, graph and event projections,
// deduplicates lookup in batches of256 and rechecks current visibility before mutating outputs.
// Invisible references are removed, actions require matching aggregate version, and each artifact
// rechecks revision/binding eligibility. A later error can leave partial mutations, so callers
// must discard the whole result; receipts never carry authority.
// projectArtifactResults собирает artifact pointers/refs из result, graph и event projections,
// устраняет повтор lookup пакетами256 и заново проверяет current visibility до изменения outputs.
// Invisible refs удаляются, actions требуют matching aggregate version, каждый artifact
// перепроверяет revision/binding eligibility. Поздняя ошибка может оставить partial mutations,
// поэтому caller отбрасывает whole result; receipts не переносят authority.
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
	// eligibleArtifact holds current aggregate version and allowed action names for a visible batch
	// reference. It is a local lookup projection; immutable content admission and bound-agent
	// visibility are checked later.
	// eligibleArtifact хранит current aggregate version и allowed action names видимой batch
	// reference. Это local lookup projection; immutable content admission и bound-agent visibility
	// проверяются далее.
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
		if err := projectArtifactRevisionEligibility(ctx, runner, current, *artifact); err != nil {
			return err
		}
		if err := projectArtifactBindingRefs(ctx, runner, current, *artifact); err != nil {
			return err
		}
	}
	return nil
}

// permittedArtifactActions returns ordered RESTORE/PURGE for deleted or DOWNLOAD/BIND/DELETE for
// active artifacts according to the supplied permission predicate; byte actions additionally
// require CLEAN. Unknown lifecycle yields empty actions. The returned hints do not replace fresh
// command authorization.
// permittedArtifactActions возвращает ordered RESTORE/PURGE для deleted либо DOWNLOAD/BIND/DELETE
// для active artifacts по переданному permission predicate; byte actions также требуют CLEAN.
// Unknown lifecycle даёт empty actions. Подсказки не заменяют fresh command authorization.
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

// projectArtifactEligibility applies current actions, exact revision scan/preview and visible
// binding refs in that order to a nonnil artifact. It stops on the first error without restoring
// earlier mutations and performs no storage I/O or transaction commit.
// projectArtifactEligibility последовательно применяет current actions, exact revision
// scan/preview и visible binding refs к nonnil artifact. Останавливается на первой ошибке без
// отката прежних mutations, не выполняет storage I/O либо transaction commit.
func projectArtifactEligibility(ctx context.Context, runner queryRunner, current scope, artifact *entity.Artifact) error {
	if err := projectArtifactActions(ctx, runner, current, artifact); err != nil {
		return err
	}
	if err := projectArtifactRevisionEligibility(ctx, runner, current, artifact); err != nil {
		return err
	}
	return projectArtifactBindingRefs(ctx, runner, current, artifact)
}

// projectArtifactActions reads the current visible artifact row in tenant/project scope, returning
// NotFound for an ineligible/missing ref and Unavailable for backend failure. It clears actions
// and repopulates only when projected aggregate version matches; partial entities are not promoted
// to immutable revision proof.
// projectArtifactActions читает current visible artifact row в tenant/project scope, возвращая
// NotFound для ineligible/missing ref и Unavailable для backend failure. Очищает actions и
// восстанавливает только при matching aggregate version; partial entities не становятся immutable
// revision proof.
func projectArtifactActions(ctx context.Context, runner queryRunner, current scope, artifact *entity.Artifact) error {
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

// projectArtifactRevisionEligibility defaults scan/preview to QUARANTINED/BLOCKED, then adopts
// effective revision scan only for exact ref/revision/digest/size/name/media pins. Missing or
// mismatched proof stays blocked without inventing CLEAN; other errors propagate. Deferred
// filtering removes DOWNLOAD/BIND whenever the final scan is not CLEAN, including on failure.
// projectArtifactRevisionEligibility сначала ставит scan/preview QUARANTINED/BLOCKED, затем
// принимает effective revision scan только для exact ref/revision/digest/size/name/media pins.
// Missing либо mismatched proof остаётся blocked без выдуманного CLEAN; прочие errors передаются
// далее. Deferred filtering удаляет DOWNLOAD/BIND при final scan не CLEAN, включая отказ.
func projectArtifactRevisionEligibility(ctx context.Context, runner queryRunner, current scope, artifact *entity.Artifact) error {
	defer func() {
		if artifact.ScanState != "CLEAN" {
			artifact.NextActions = slices.DeleteFunc(artifact.NextActions, func(action string) bool { return action == "DOWNLOAD" || action == "BIND" })
		}
	}()
	artifact.ScanState, artifact.PreviewState = "QUARANTINED", "BLOCKED"
	if artifact.CurrentRevisionRef == "" || !objectDigestValid(artifact.Digest) {
		return nil
	}
	revision, err := artifactRevisionRow(runner.QueryRow(ctx, queryArtifactRevisionGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "artifact_ref": artifact.Ref, "revision_ref": artifact.CurrentRevisionRef,
	}))
	if errors.Is(err, errs.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if revision.Revision != artifact.Revision || revision.Digest != artifact.Digest || revision.SizeBytes != artifact.SizeBytes ||
		revision.FileName != artifact.FileName || revision.MediaType != artifact.MediaType {
		return nil
	}
	artifact.ScanState = revision.ScanState
	if revision.ScanState == "CLEAN" {
		artifact.PreviewState = "UNAVAILABLE"
		if revision.PreviewAvailable {
			artifact.PreviewState = "AVAILABLE"
		}
	}
	return nil
}
