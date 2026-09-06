package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/prompt_preview_knowledge.sql
var queryPromptPreviewKnowledge string

//go:embed sql/prompt_preview_session_sets.sql
var queryPromptPreviewSessionSets string

func (repository *Repository) promptPreviewSessionFilesTx(ctx context.Context, tx pgx.Tx, current scope, sessionID, projectID, currentSetRef string, snapshot *entity.PromptMaterializationSnapshot) error {
	rows, err := tx.Query(ctx, queryPromptPreviewSessionSets, pgx.StrictNamedArgs{"organization_id": current.organizationID, "session_id": sessionID, "project_id": projectID, "current_set_ref": currentSetRef})
	if err != nil {
		return errs.ErrUnavailable
	}
	var sets []sealedAttachmentSet
	for rows.Next() {
		var set sealedAttachmentSet
		if rows.Scan(&set.ID, &set.Ref, &set.ManifestDigest, &set.Purpose, &set.ItemCount, &set.TotalSizeBytes) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		sets = append(sets, set)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errs.ErrUnavailable
	}
	if len(sets) > 512 {
		return errs.ErrConflict
	}
	for _, set := range sets {
		if err := repository.promptPreviewAttachmentSetTx(ctx, tx, current, projectID, snapshot, set, true); err != nil {
			return err
		}
	}
	return refreshPromptFileScopes(snapshot)
}

func (repository *Repository) promptPreviewKnowledgeTx(ctx context.Context, tx pgx.Tx, current scope, snapshot *entity.PromptMaterializationSnapshot) error {
	if !capabilityEnabled(snapshot.AgentCapabilities, runtimecontract.ArtifactCapability) {
		return refreshPromptFileScopes(snapshot)
	}
	rows, err := tx.Query(ctx, queryPromptPreviewKnowledge, pgx.StrictNamedArgs{"organization_id": current.organizationID, "agent_ref": snapshot.ContextPin.AgentRef})
	if err != nil {
		return errs.ErrUnavailable
	}
	refs := []string{}
	for rows.Next() {
		item := runtimecontract.RunnerInputArtifact{Scope: runtimecontract.AttachmentScopeKnowledge, AttachmentPurpose: "PROJECT_KNOWLEDGE", Provenance: "PROJECT_BINDING"}
		if err := rows.Scan(&item.Ref, &item.FileName, &item.MediaType, &item.Digest, &item.SizeBytes, &item.Revision, &item.Version, &item.Source, &item.Position); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		snapshot.Artifacts = append(snapshot.Artifacts, item)
		refs = append(refs, item.Ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errs.ErrUnavailable
	}
	if err := repository.authorizePromptArtifactsTx(ctx, tx, current, snapshot.ProjectRef, refs); err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			disablePromptFileScopes(snapshot, "PERMISSION_REQUIRED")
			return refreshPromptFileScopes(snapshot)
		}
		return err
	}
	return refreshPromptFileScopes(snapshot)
}

func refreshPromptFileScopes(snapshot *entity.PromptMaterializationSnapshot) error {
	if !capabilityEnabled(snapshot.AgentCapabilities, runtimecontract.ArtifactCapability) {
		disablePromptFileScopes(snapshot, "CAPABILITY_REQUIRED")
	} else if snapshot.UnavailableVariables["input.files"] == "PERMISSION_REQUIRED" {
		snapshot.Artifacts = nil
	}
	items := make([]map[string]any, 0, len(snapshot.Artifacts))
	for _, item := range snapshot.Artifacts {
		for _, number := range []int64{item.SizeBytes, item.Revision, item.Version, item.Position} {
			if number < 0 || number > 9007199254740991 {
				return errs.ErrConflict
			}
		}
		items = append(items, map[string]any{"ref": item.Ref, "fileName": item.FileName, "mediaType": item.MediaType, "digest": item.Digest, "sizeBytes": float64(item.SizeBytes),
			"revision": float64(item.Revision), "version": float64(item.Version), "source": item.Source, "position": float64(item.Position), "scope": item.Scope, "attachmentSetRef": item.AttachmentSetRef, "attachmentPurpose": item.AttachmentPurpose, "provenance": item.Provenance})
	}
	structured, err := promptStructuredVariables(items, nil, runtimecontract.RuntimeEnvironmentImage{}, snapshot.ContextPin.EnvironmentVersionRef, snapshot.ContextPin.AttachmentSetRef, snapshot.ContextPin.WorkflowRef)
	if err != nil {
		return errs.ErrConflict
	}
	for _, scope := range []string{"input", "run", "session", "workflow", "gate", "project"} {
		current, ok := snapshot.StructuredVariables[scope].(map[string]any)
		if !ok {
			current = map[string]any{}
			snapshot.StructuredVariables[scope] = current
		}
		for key, value := range structured[scope].(map[string]any) {
			current[key] = value
		}
	}
	return nil
}

func disablePromptFileScopes(snapshot *entity.PromptMaterializationSnapshot, reason string) {
	if snapshot.UnavailableVariables == nil {
		snapshot.UnavailableVariables = map[string]string{}
	}
	for _, scope := range []string{"input", "run", "session", "workflow", "gate", "project"} {
		for _, field := range []string{"files", "files_count", "files_dir", "manifest_path"} {
			snapshot.UnavailableVariables[scope+"."+field] = reason
		}
	}
	snapshot.Artifacts = nil
}
