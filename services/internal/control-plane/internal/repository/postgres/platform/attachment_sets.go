package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

const maximumAttachmentSetBytes int64 = 512 << 20

type sealedAttachmentSet struct {
	ID, Ref, ManifestDigest string
	ArtifactRefs            []string
	ItemCount               int64
	TotalSizeBytes          int64
}

type attachmentSetItem struct {
	ArtifactID string
	runtimecontract.RunnerInputArtifact
}

func (repository *Repository) sealAttachmentSet(
	ctx context.Context,
	tx pgx.Tx,
	scope scope,
	projectID string,
	artifactRefs []string,
	contextKind string,
) (sealedAttachmentSet, error) {
	if len(artifactRefs) == 0 {
		return sealedAttachmentSet{}, nil
	}
	if !contains([]string{"ASSISTANT_MESSAGE", "SESSION_TURN", "RUN_INPUT", "WORKFLOW_INPUT", "OWNER_GATE_MESSAGE"}, contextKind) {
		return sealedAttachmentSet{}, errs.ErrInvalid
	}
	rows, err := tx.Query(ctx, queryAttachmentSetsSelectArtifacts, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"project_id":      projectID,
		"created_by":      scope.actorID,
		"artifact_refs":   artifactRefs,
	})
	if err != nil {
		return sealedAttachmentSet{}, errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]attachmentSetItem, 0, len(artifactRefs))
	seen := make(map[string]struct{}, len(artifactRefs))
	var total int64
	for rows.Next() {
		var item attachmentSetItem
		if err := rows.Scan(&item.ArtifactID, &item.Ref, &item.Revision,
			&item.Version, &item.FileName, &item.MediaType, &item.SizeBytes,
			&item.Digest, &item.Source, &item.Position); err != nil {
			return sealedAttachmentSet{}, errs.ErrUnavailable
		}
		item.Scope = runtimecontract.AttachmentScopeInput
		if _, duplicate := seen[item.Ref]; duplicate {
			return sealedAttachmentSet{}, errs.ErrInvalid
		}
		seen[item.Ref] = struct{}{}
		if item.SizeBytes < 0 || total > maximumAttachmentSetBytes-item.SizeBytes {
			return sealedAttachmentSet{}, errs.ErrInvalid
		}
		total += item.SizeBytes
		items = append(items, item)
	}
	if rows.Err() != nil {
		return sealedAttachmentSet{}, errs.ErrUnavailable
	}
	if len(items) != len(artifactRefs) || total > maximumAttachmentSetBytes {
		return sealedAttachmentSet{}, errs.ErrConflict
	}
	ref, err := newRef("aset")
	if err != nil {
		return sealedAttachmentSet{}, err
	}
	artifacts := make([]runtimecontract.RunnerInputArtifact, 0, len(items))
	for _, item := range items {
		artifacts = append(artifacts, item.RunnerInputArtifact)
	}
	manifest, err := runtimecontract.BuildAttachmentManifest(ref, contextKind, artifacts)
	if err != nil {
		return sealedAttachmentSet{}, errs.ErrInvalid
	}
	var setID string
	if err := tx.QueryRow(ctx, queryAttachmentSetsInsertSet, pgx.StrictNamedArgs{
		"attachment_set_ref": ref,
		"organization_id":    scope.organizationID,
		"project_id":         projectID,
		"context_kind":       contextKind,
		"manifest":           manifest.Bytes,
		"manifest_digest":    manifest.Digest,
		"item_count":         len(items),
		"total_size_bytes":   total,
		"created_by":         scope.actorID,
	}).Scan(&setID); err != nil {
		return sealedAttachmentSet{}, errs.ErrUnavailable
	}
	for _, item := range items {
		if _, err := tx.Exec(ctx, queryAttachmentSetsInsertItem, pgx.StrictNamedArgs{
			"attachment_set_id": setID,
			"position":          item.Position,
			"artifact_id":       item.ArtifactID,
			"artifact_ref":      item.Ref,
			"artifact_revision": item.Revision,
			"artifact_version":  item.Version,
			"file_name":         item.FileName,
			"media_type":        item.MediaType,
			"size_bytes":        item.SizeBytes,
			"digest":            item.Digest,
		}); err != nil {
			return sealedAttachmentSet{}, errs.ErrUnavailable
		}
	}
	return sealedAttachmentSet{ID: setID, Ref: ref, ManifestDigest: manifest.Digest,
		ArtifactRefs: append([]string(nil), artifactRefs...), ItemCount: int64(len(items)), TotalSizeBytes: total}, nil
}

func (repository *Repository) bindAttachmentSet(
	ctx context.Context,
	tx pgx.Tx,
	scope scope,
	projectID string,
	set sealedAttachmentSet,
	targetKind string,
	targetRef string,
) error {
	if set.ID == "" {
		return nil
	}
	if targetRef == "" || !contains([]string{"ASSISTANT_MESSAGE", "SESSION_TURN", "RUN_INPUT", "WORKFLOW_INPUT", "OWNER_GATE_MESSAGE"}, targetKind) {
		return errs.ErrInvalid
	}
	bindingRef, err := newRef("abnd")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryAttachmentSetsInsertBinding, pgx.StrictNamedArgs{
		"binding_ref":       bindingRef,
		"organization_id":   scope.organizationID,
		"project_id":        projectID,
		"attachment_set_id": set.ID,
		"target_kind":       targetKind,
		"target_ref":        targetRef,
		"created_by":        scope.actorID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrConflict
		}
		return errs.ErrUnavailable
	}
	return nil
}

func (repository *Repository) attachSetToRun(
	ctx context.Context,
	tx pgx.Tx,
	scope scope,
	projectID string,
	set sealedAttachmentSet,
	runID string,
	runRef string,
	targetKind string,
) error {
	if set.ID == "" {
		return nil
	}
	tag, err := tx.Exec(ctx, queryAttachmentSetsBindRun, pgx.StrictNamedArgs{
		"attachment_set_id": set.ID,
		"artifact_refs":     set.ArtifactRefs,
		"run_id":            runID,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return repository.bindAttachmentSet(ctx, tx, scope, projectID, set, targetKind, runRef)
}

func (repository *Repository) attachSetToTurn(
	ctx context.Context,
	tx pgx.Tx,
	scope scope,
	projectID string,
	set sealedAttachmentSet,
	turnID string,
	turnRef string,
	targetKind string,
) error {
	if set.ID == "" {
		return nil
	}
	tag, err := tx.Exec(ctx, queryAttachmentSetsBindTurn, pgx.StrictNamedArgs{
		"attachment_set_id": set.ID,
		"artifact_refs":     set.ArtifactRefs,
		"turn_id":           turnID,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return repository.bindAttachmentSet(ctx, tx, scope, projectID, set, targetKind, turnRef)
}
