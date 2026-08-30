package platform

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

const (
	maximumAttachmentSetBytes      int64 = 512 << 20
	maximumAttachmentMutationItems       = 100
	attachmentSetCursorVersion           = "v1"
)

var attachmentSetPurposes = []string{
	"ASSISTANT_MESSAGE", "SESSION_TURN", "RUN_INPUT", "WORKFLOW_INPUT", "OWNER_GATE_MESSAGE",
}

type sealedAttachmentSet struct {
	ID, Ref, ManifestDigest, Purpose string
	ItemCount, TotalSizeBytes        int64
}

type attachmentSetRevision struct {
	ID, Ref, FamilyRef, ProjectID, ProjectRef, State, Purpose, Source, ManifestDigest string
	Revision, Version, ItemCount, TotalSizeBytes                                      int64
}

type attachmentSetItem struct {
	ArtifactID string
	runtimecontract.RunnerInputArtifact
}

func (repository *Repository) changeAttachmentSet(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AttachmentSetDraftInput)
	if !ok || len(payload.ArtifactRefs) > maximumAttachmentMutationItems {
		return commandOutcome{}, errs.ErrInvalid
	}
	if input.Kind == command.CreateAttachmentSetDraft {
		if !contains(attachmentSetPurposes, payload.Purpose) || input.Mutation.ExpectedVersion != nil ||
			(payload.ProjectRef == "" && payload.Purpose != "ASSISTANT_MESSAGE") {
			return commandOutcome{}, errs.ErrInvalid
		}
		projectID := ""
		if payload.ProjectRef != "" {
			projectID = mustProjectID(ctx, tx, current.organizationID, payload.ProjectRef)
		}
		if payload.ProjectRef != "" && projectID == "" {
			return commandOutcome{}, errs.ErrNotFound
		}
		items, total, err := repository.resolveAttachmentArtifacts(ctx, tx, current, projectID, payload.ArtifactRefs)
		if err != nil {
			return commandOutcome{}, err
		}
		familyRef, err := newRef("asetf")
		if err != nil {
			return commandOutcome{}, err
		}
		created, err := repository.insertAttachmentSetRevision(ctx, tx, current, projectID, payload.ProjectRef,
			familyRef, 1, "DRAFT", payload.Purpose, "CONTROL_CENTER", items, total, nil)
		if err != nil {
			return commandOutcome{}, err
		}
		return attachmentSetOutcome(created, projectID, payload.ProjectRef, "i18n:ATTACHMENT_SET_DRAFT_CREATED"), nil
	}
	if payload.AttachmentSetRef == "" || input.Mutation.ExpectedVersion == nil ||
		len(payload.ArtifactRefs) == 0 && input.Kind != command.FinalizeAttachmentSet {
		return commandOutcome{}, errs.ErrInvalid
	}
	base, err := repository.lockLatestAttachmentSetRevision(ctx, tx, current, payload.AttachmentSetRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if base.State != "DRAFT" || base.Version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	items, err := repository.listAttachmentSetItemsTx(ctx, tx, base.ID)
	if err != nil {
		return commandOutcome{}, err
	}
	switch input.Kind {
	case command.AddAttachmentSetItems:
		if payload.InsertAfterPosition < 0 || payload.InsertAfterPosition > int64(len(items)) {
			return commandOutcome{}, errs.ErrInvalid
		}
		addition, _, resolveErr := repository.resolveAttachmentArtifacts(ctx, tx, current, base.ProjectID, payload.ArtifactRefs)
		if resolveErr != nil {
			return commandOutcome{}, resolveErr
		}
		items, err = insertAttachmentItems(items, addition, payload.InsertAfterPosition)
	case command.RemoveAttachmentSetItems:
		items, err = removeAttachmentItems(items, payload.ArtifactRefs)
	case command.FinalizeAttachmentSet:
		if len(payload.ArtifactRefs) != 0 || payload.InsertAfterPosition != 0 || len(items) == 0 {
			return commandOutcome{}, errs.ErrInvalid
		}
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
	if err != nil {
		return commandOutcome{}, err
	}
	total, err := attachmentItemsTotal(items)
	if err != nil {
		return commandOutcome{}, err
	}
	if input.Kind == command.FinalizeAttachmentSet {
		artifacts := make([]runtimecontract.RunnerInputArtifact, 0, len(items))
		for _, item := range items {
			item.Scope = runtimecontract.AttachmentScopeInput
			artifacts = append(artifacts, item.RunnerInputArtifact)
		}
		setRef, refErr := newRef("aset")
		if refErr != nil {
			return commandOutcome{}, refErr
		}
		manifest, buildErr := runtimecontract.BuildAttachmentManifest(setRef, base.Purpose, artifacts)
		if buildErr != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		created, insertErr := repository.insertAttachmentSetRevisionWithRef(ctx, tx, current, base.ProjectID, base.ProjectRef,
			setRef, base.FamilyRef, base.Revision+1, "FINALIZED", base.Purpose, base.Source, items, total, &manifest, base.ID)
		if insertErr != nil {
			return commandOutcome{}, insertErr
		}
		return attachmentSetOutcome(created, base.ProjectID, base.ProjectRef, "i18n:ATTACHMENT_SET_FINALIZED"), nil
	}
	created, err := repository.insertAttachmentSetRevision(ctx, tx, current, base.ProjectID, base.ProjectRef,
		base.FamilyRef, base.Revision+1, "DRAFT", base.Purpose, base.Source, items, total, base.ID)
	if err != nil {
		return commandOutcome{}, err
	}
	return attachmentSetOutcome(created, base.ProjectID, base.ProjectRef, "i18n:ATTACHMENT_SET_DRAFT_REVISED"), nil
}

func attachmentSetOutcome(item entity.AttachmentSet, projectID, projectRef, summary string) commandOutcome {
	return commandOutcome{result: command.Result{AttachmentSet: &item}, projectID: projectID, projectRef: projectRef,
		resourceKind: "ATTACHMENT_SET", resourceRef: item.Ref, summary: summary}
}

func (repository *Repository) insertAttachmentSetRevision(ctx context.Context, tx pgx.Tx, current scope,
	projectID, projectRef, familyRef string, revision int64, state, purpose, source string,
	items []attachmentSetItem, total int64, previous any,
) (entity.AttachmentSet, error) {
	ref, err := newRef("aset")
	if err != nil {
		return entity.AttachmentSet{}, err
	}
	return repository.insertAttachmentSetRevisionWithRef(ctx, tx, current, projectID, projectRef, ref, familyRef,
		revision, state, purpose, source, items, total, nil, previous)
}

func (repository *Repository) insertAttachmentSetRevisionWithRef(ctx context.Context, tx pgx.Tx, current scope,
	projectID, projectRef, ref, familyRef string, revision int64, state, purpose, source string,
	items []attachmentSetItem, total int64, manifest *runtimecontract.CanonicalAttachmentManifest, previous any,
) (entity.AttachmentSet, error) {
	var manifestBytes any
	var manifestDigest any
	if manifest != nil {
		manifestBytes, manifestDigest = manifest.Bytes, manifest.Digest
	}
	var setID string
	if err := tx.QueryRow(ctx, queryAttachmentSetsInsertSet, pgx.StrictNamedArgs{
		"attachment_set_ref": ref, "family_ref": familyRef, "revision": revision,
		"previous_revision_id": previous, "organization_id": current.organizationID,
		"project_id": projectID, "state": state, "source": source, "purpose": purpose,
		"manifest": manifestBytes, "manifest_digest": manifestDigest, "item_count": len(items),
		"total_size_bytes": total, "created_by": current.actorID,
	}).Scan(&setID); err != nil {
		return entity.AttachmentSet{}, mapWriteError(err)
	}
	for index, item := range items {
		position := int64(index + 1)
		if _, err := tx.Exec(ctx, queryAttachmentSetsInsertItem, pgx.StrictNamedArgs{
			"attachment_set_id": setID, "position": position, "artifact_id": item.ArtifactID,
			"artifact_ref": item.Ref, "artifact_revision": item.Revision, "artifact_version": item.Version,
			"file_name": item.FileName, "media_type": item.MediaType, "size_bytes": item.SizeBytes,
			"digest": item.Digest, "source": item.Source,
		}); err != nil {
			return entity.AttachmentSet{}, mapWriteError(err)
		}
		items[index].Position = position
	}
	result := entity.AttachmentSet{Ref: ref, FamilyRef: familyRef, Revision: revision, Version: revision,
		ProjectRef: projectRef, State: state, Purpose: purpose, Source: source, ItemCount: int64(len(items)),
		TotalSizeBytes: total, Items: castAttachmentSetItems(items)}
	if manifest != nil {
		result.ManifestDigest = manifest.Digest
	}
	if err := tx.QueryRow(ctx, queryAttachmentSetsSelectTimestamps, setID).Scan(&result.CreatedAt, &result.FinalizedAt); err != nil {
		return entity.AttachmentSet{}, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) resolveAttachmentArtifacts(ctx context.Context, tx pgx.Tx, current scope, projectID string, refs []string) ([]attachmentSetItem, int64, error) {
	if len(refs) == 0 {
		return []attachmentSetItem{}, 0, nil
	}
	requested := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			return nil, 0, errs.ErrInvalid
		}
		if _, duplicate := requested[ref]; duplicate {
			return nil, 0, errs.ErrInvalid
		}
		requested[ref] = struct{}{}
		_, target, resolveErr := repository.resolveCommandTarget(ctx, tx, current, "artifact.bind", "ARTIFACT", ref, "")
		if resolveErr != nil || repository.requireAccess(ctx, tx, current, "artifact.bind", target) != nil {
			return nil, 0, errs.ErrNotFound
		}
	}
	rows, err := tx.Query(ctx, queryAttachmentSetsSelectArtifacts, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_id": projectID, "artifact_refs": refs,
	})
	if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]attachmentSetItem, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	var total int64
	for rows.Next() {
		var item attachmentSetItem
		if err := rows.Scan(&item.ArtifactID, &item.Ref, &item.Revision, &item.Version, &item.FileName,
			&item.MediaType, &item.SizeBytes, &item.Digest, &item.Source, &item.Position); err != nil {
			return nil, 0, errs.ErrUnavailable
		}
		item.Scope = runtimecontract.AttachmentScopeInput
		if _, duplicate := seen[item.Ref]; duplicate {
			return nil, 0, errs.ErrInvalid
		}
		seen[item.Ref] = struct{}{}
		if item.SizeBytes < 0 || total > maximumAttachmentSetBytes-item.SizeBytes {
			return nil, 0, errs.ErrInvalid
		}
		total += item.SizeBytes
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, 0, errs.ErrUnavailable
	}
	if len(items) != len(refs) {
		return nil, 0, errs.ErrConflict
	}
	return items, total, nil
}

func (repository *Repository) lockLatestAttachmentSetRevision(ctx context.Context, tx pgx.Tx, current scope, ref string) (attachmentSetRevision, error) {
	var familyRef string
	if err := tx.QueryRow(ctx, queryAttachmentSetsSelectFamily, current.organizationID, ref, current.actorID).Scan(&familyRef); err != nil {
		return attachmentSetRevision{}, errs.ErrNotFound
	}
	if _, err := tx.Exec(ctx, queryAttachmentSetsLockFamily, familyRef); err != nil {
		return attachmentSetRevision{}, errs.ErrUnavailable
	}
	var item attachmentSetRevision
	if err := tx.QueryRow(ctx, queryAttachmentSetsSelectLatest, current.organizationID, ref).Scan(
		&item.ID, &item.Ref, &item.FamilyRef, &item.ProjectID, &item.ProjectRef, &item.State,
		&item.Purpose, &item.Source, &item.ManifestDigest, &item.Revision, &item.Version,
		&item.ItemCount, &item.TotalSizeBytes,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return attachmentSetRevision{}, errs.ErrVersionMismatch
		}
		return attachmentSetRevision{}, errs.ErrUnavailable
	}
	return item, nil
}

func (repository *Repository) resolveFinalizedAttachmentSet(ctx context.Context, tx pgx.Tx, current scope,
	projectID, ref, purpose string,
) (sealedAttachmentSet, error) {
	if ref == "" {
		return sealedAttachmentSet{}, nil
	}
	var item sealedAttachmentSet
	if err := tx.QueryRow(ctx, queryAttachmentSetsResolveFinalized, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_id": projectID,
		"attachment_set_ref": ref, "purpose": purpose,
	}).Scan(&item.ID, &item.Ref, &item.ManifestDigest, &item.Purpose, &item.ItemCount, &item.TotalSizeBytes); err != nil {
		return sealedAttachmentSet{}, errs.ErrConflict
	}
	return item, nil
}

func (repository *Repository) listAttachmentSetItemsTx(ctx context.Context, tx pgx.Tx, setID string) ([]attachmentSetItem, error) {
	rows, err := tx.Query(ctx, queryAttachmentSetsListItems, setID)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := []attachmentSetItem{}
	for rows.Next() {
		var item attachmentSetItem
		if err := rows.Scan(&item.ArtifactID, &item.Ref, &item.Revision, &item.Version, &item.FileName,
			&item.MediaType, &item.SizeBytes, &item.Digest, &item.Source, &item.Position); err != nil {
			return nil, errs.ErrUnavailable
		}
		item.Scope = runtimecontract.AttachmentScopeInput
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

func insertAttachmentItems(current, additions []attachmentSetItem, after int64) ([]attachmentSetItem, error) {
	seen := make(map[string]struct{}, len(current)+len(additions))
	for _, item := range current {
		seen[item.Ref] = struct{}{}
	}
	for _, item := range additions {
		if _, duplicate := seen[item.Ref]; duplicate {
			return nil, errs.ErrConflict
		}
		seen[item.Ref] = struct{}{}
	}
	position := int(after)
	result := make([]attachmentSetItem, 0, len(current)+len(additions))
	result = append(result, current[:position]...)
	result = append(result, additions...)
	result = append(result, current[position:]...)
	return result, nil
}

func removeAttachmentItems(current []attachmentSetItem, refs []string) ([]attachmentSetItem, error) {
	remove := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if _, duplicate := remove[ref]; duplicate {
			return nil, errs.ErrInvalid
		}
		remove[ref] = struct{}{}
	}
	result := make([]attachmentSetItem, 0, len(current))
	for _, item := range current {
		if _, found := remove[item.Ref]; found {
			delete(remove, item.Ref)
			continue
		}
		result = append(result, item)
	}
	if len(remove) != 0 {
		return nil, errs.ErrConflict
	}
	return result, nil
}

func attachmentItemsTotal(items []attachmentSetItem) (int64, error) {
	var total int64
	for _, item := range items {
		if item.SizeBytes < 0 || total > maximumAttachmentSetBytes-item.SizeBytes {
			return 0, errs.ErrInvalid
		}
		total += item.SizeBytes
	}
	return total, nil
}

func castAttachmentSetItems(items []attachmentSetItem) []entity.AttachmentSetItem {
	result := make([]entity.AttachmentSetItem, 0, len(items))
	for _, item := range items {
		result = append(result, entity.AttachmentSetItem{ArtifactRef: item.Ref, ArtifactRevision: item.Revision,
			ArtifactVersion: item.Version, DisplayName: item.FileName, MediaType: item.MediaType,
			SizeBytes: item.SizeBytes, Digest: item.Digest, Source: item.Source, Position: item.Position})
	}
	return result
}

func (repository *Repository) GetAttachmentSet(ctx context.Context, principal value.Principal, ref string, page query.Page) (entity.AttachmentSet, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.AttachmentSet{}, "", err
	}
	cursor, err := decodeAttachmentSetCursor(page.Token)
	if err != nil {
		return entity.AttachmentSet{}, "", err
	}
	limit := page.Size
	if limit == 0 {
		limit = 50
	}
	var item entity.AttachmentSet
	var setID, projectID string
	if err := repository.pool.QueryRow(ctx, queryAttachmentSetsGet, current.organizationID, ref).Scan(
		&setID, &item.Ref, &item.FamilyRef, &projectID, &item.ProjectRef, &item.Revision, &item.Version,
		&item.State, &item.Purpose, &item.Source, &item.ManifestDigest, &item.ItemCount,
		&item.TotalSizeBytes, &item.CreatedAt, &item.FinalizedAt, &item.Superseded,
	); err != nil {
		return entity.AttachmentSet{}, "", errs.ErrNotFound
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.AttachmentSet{}, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if item.ProjectRef == "" {
		if repository.requireAccess(ctx, tx, current, "organization.view", resolvedAccessTarget{scope: organizationTarget(current.organizationRef)}) != nil {
			return entity.AttachmentSet{}, "", errs.ErrNotFound
		}
	} else {
		_, target, targetErr := repository.resolveCommandTarget(ctx, tx, current, "project.view", "PROJECT", item.ProjectRef, item.ProjectRef)
		if targetErr != nil || repository.requireAccess(ctx, tx, current, "project.view", target) != nil {
			return entity.AttachmentSet{}, "", errs.ErrNotFound
		}
	}
	rows, err := tx.Query(ctx, queryAttachmentSetsGetItems, setID, cursor, limit+1)
	if err != nil {
		return entity.AttachmentSet{}, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := []entity.AttachmentSetItem{}
	for rows.Next() {
		var item entity.AttachmentSetItem
		if err := rows.Scan(&item.ArtifactRef, &item.ArtifactRevision, &item.ArtifactVersion, &item.DisplayName,
			&item.MediaType, &item.SizeBytes, &item.Digest, &item.Source, &item.Position); err != nil {
			return entity.AttachmentSet{}, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return entity.AttachmentSet{}, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = encodeAttachmentSetCursor(items[len(items)-1].Position)
	}
	for _, attachment := range items {
		_, target, resolveErr := repository.resolveCommandTarget(ctx, tx, current, "artifact.view", "ARTIFACT", attachment.ArtifactRef, item.ProjectRef)
		if resolveErr != nil || repository.requireAccess(ctx, tx, current, "artifact.view", target) != nil {
			return entity.AttachmentSet{}, "", errs.ErrNotFound
		}
	}
	item.Items = items
	if err := tx.Commit(ctx); err != nil {
		return entity.AttachmentSet{}, "", errs.ErrUnavailable
	}
	return item, next, nil
}

func encodeAttachmentSetCursor(position int64) string {
	return attachmentSetCursorVersion + "." + base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(position, 10)))
}

func decodeAttachmentSetCursor(token string) (int64, error) {
	if token == "" {
		return 0, nil
	}
	version, payload, found := strings.Cut(token, ".")
	if !found || version != attachmentSetCursorVersion || len(payload) > 32 {
		return 0, errs.ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return 0, errs.ErrInvalid
	}
	position, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || position < 1 {
		return 0, errs.ErrInvalid
	}
	return position, nil
}

func (repository *Repository) bindAttachmentSet(ctx context.Context, tx pgx.Tx, current scope, projectID string,
	set sealedAttachmentSet, kind, targetID string,
) error {
	if set.ID == "" {
		return nil
	}
	if targetID == "" || !contains(attachmentSetPurposes, kind) {
		return errs.ErrInvalid
	}
	bindingRef, err := newRef("abnd")
	if err != nil {
		return err
	}
	arguments := pgx.StrictNamedArgs{
		"binding_ref": bindingRef, "organization_id": current.organizationID, "project_id": projectID,
		"attachment_set_id": set.ID, "kind": kind, "created_by": current.actorID,
		"assistant_turn_id": nil, "session_turn_id": nil, "run_id": nil, "owner_gate_id": nil,
	}
	switch kind {
	case "ASSISTANT_MESSAGE":
		arguments["assistant_turn_id"] = targetID
	case "SESSION_TURN":
		arguments["session_turn_id"] = targetID
	case "RUN_INPUT", "WORKFLOW_INPUT":
		arguments["run_id"] = targetID
	case "OWNER_GATE_MESSAGE":
		arguments["owner_gate_id"] = targetID
	}
	if _, err := tx.Exec(ctx, queryAttachmentSetsInsertBinding, arguments); err != nil {
		return mapWriteError(err)
	}
	return nil
}

func (repository *Repository) attachSetToRun(ctx context.Context, tx pgx.Tx, current scope, projectID string,
	set sealedAttachmentSet, runID, kind string,
) error {
	if set.ID == "" {
		return nil
	}
	tag, err := tx.Exec(ctx, queryAttachmentSetsBindRun, pgx.StrictNamedArgs{"attachment_set_id": set.ID, "run_id": runID})
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return repository.bindAttachmentSet(ctx, tx, current, projectID, set, kind, runID)
}

func (repository *Repository) attachSetToTurn(ctx context.Context, tx pgx.Tx, current scope, projectID string,
	set sealedAttachmentSet, turnID, kind string,
) error {
	if set.ID == "" {
		return nil
	}
	tag, err := tx.Exec(ctx, queryAttachmentSetsBindTurn, pgx.StrictNamedArgs{"attachment_set_id": set.ID, "turn_id": turnID})
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return repository.bindAttachmentSet(ctx, tx, current, projectID, set, kind, turnID)
}
