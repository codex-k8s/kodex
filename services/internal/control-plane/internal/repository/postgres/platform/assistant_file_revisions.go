package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

const createProjectFileRevision = "CREATE_PROJECT_FILE_REVISION"

// Применённые исторические строки и их digest остаются неизменными. Публичная
// история и будущая проекция prompt не возвращают историческое содержимое файла.
func redactAssistantFilePlanOperations(operations []entity.AssistantPlanOperation) {
	for index, operation := range operations {
		if !assistantFileOperation(operation.Type) {
			continue
		}
		operation.Parameters = cloneAssistantFields(operation.Parameters)
		delete(operation.Parameters, "content")
		operation.After = cloneAssistantFields(operation.After)
		delete(operation.After, "content")
		operation.Input = nil
		operations[index] = operation
	}
}

func assistantFileOperation(kind string) bool {
	return kind == "CREATE_PROJECT_FILE" || kind == createProjectFileRevision
}

// Граница проверяется до первого SQL write: даже незавершённая транзакция
// не должна передавать тело файла в PostgreSQL.
func assistantFilePersistenceReady(operations []entity.AssistantPlanOperation, prepared map[string]string) error {
	for _, operation := range operations {
		if !assistantFileOperation(operation.Type) {
			continue
		}
		if !assistantProjectFileContentReady(operation) || prepared[operation.Key] == "" {
			return errs.ErrConflict
		}
		if _, exists := operation.Input["content"]; exists {
			return errs.ErrConflict
		}
		if _, exists := operation.After["content"]; exists {
			return errs.ErrConflict
		}
	}
	return nil
}

func rehydrateEditedAssistantFile(original, edited entity.AssistantPlanOperation, serverPrepared bool) (entity.AssistantPlanOperation, error) {
	if !assistantJSONEqual(original.Target, edited.Target) || !assistantJSONEqual(original.Before, edited.Before) ||
		!assistantJSONEqual(original.ExpectedVersion, edited.ExpectedVersion) {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	_, hasBody := edited.Parameters["content"]
	if serverPrepared {
		if hasBody || assistantString(edited.Parameters, "contentRef") == "" || !objectDigestValid(assistantString(edited.Parameters, "digest")) {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		if original.Type == createProjectFileRevision {
			if !assistantJSONEqual(edited.After, assistantRevisionAfter(edited)) || assistantString(edited.Parameters, "artifactRef") != original.Target.Ref {
				return entity.AssistantPlanOperation{}, errs.ErrForbidden
			}
		} else if !assistantJSONEqual(edited.After, edited.Parameters) {
			return entity.AssistantPlanOperation{}, errs.ErrForbidden
		}
	} else {
		if !assistantJSONEqual(original.After, edited.After) {
			return entity.AssistantPlanOperation{}, errs.ErrForbidden
		}
		if !hasBody {
			if !assistantJSONEqual(original.Parameters, edited.Parameters) {
				return entity.AssistantPlanOperation{}, errs.ErrForbidden
			}
		} else {
			for _, field := range []string{"contentRef", "digest", "sizeBytes"} {
				if _, ok := edited.Parameters[field]; ok {
					return entity.AssistantPlanOperation{}, errs.ErrInvalid
				}
			}
			if original.Type == createProjectFileRevision {
				if assistantString(edited.Parameters, "artifactRef") != original.Target.Ref {
					return entity.AssistantPlanOperation{}, errs.ErrForbidden
				}
				edited.After = assistantRevisionAfter(edited)
			} else {
				if assistantString(edited.Parameters, "projectRef") != assistantString(original.Parameters, "projectRef") {
					return entity.AssistantPlanOperation{}, errs.ErrForbidden
				}
				edited.After = cloneAssistantFields(edited.Parameters)
				delete(edited.After, "content")
			}
		}
	}
	edited.Input = nil
	return edited, nil
}

func (repository *Repository) assistantFileRevisionSnapshotMatches(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (bool, error) {
	item, err := repository.artifactRevisionReadHead(ctx, tx, current, operation.Target.Ref)
	if err != nil {
		return false, err
	}
	return operation.ExpectedVersion != nil && item.Version == *operation.ExpectedVersion && assistantJSONEqual(assistantRevisionBefore(item), operation.Before), nil
}

//go:embed sql/artifact_revision_create_lock.sql
var queryArtifactRevisionCreateLock string

//go:embed sql/artifact_revision_create.sql
var queryArtifactRevisionCreate string

//go:embed sql/artifact_revision_create_content.sql
var queryArtifactRevisionCreateContent string

//go:embed sql/artifact_revision_advance_head.sql
var queryArtifactRevisionAdvanceHead string

//go:embed sql/artifact_revision_current_id.sql
var queryArtifactRevisionCurrentID string

func assistantRevisionBefore(item entity.Artifact) map[string]any {
	return map[string]any{"artifactRef": item.Ref, "currentRevisionRef": item.CurrentRevisionRef,
		"revision": item.Revision, "version": item.Version, "digest": item.Digest,
		"fileName": item.FileName, "mediaType": item.MediaType, "sizeBytes": item.SizeBytes,
		"scanState": item.ScanState, "lifecycleState": item.LifecycleState}
}

func assistantRevisionAfter(operation entity.AssistantPlanOperation) map[string]any {
	return map[string]any{"artifactRef": assistantString(operation.Parameters, "artifactRef"),
		"fileName": assistantString(operation.Before, "fileName"), "mediaType": assistantString(operation.Parameters, "mediaType"),
		"digest": assistantString(operation.Parameters, "digest"), "sizeBytes": operation.Parameters["sizeBytes"],
		"contentRef":          assistantString(operation.Parameters, "contentRef"),
		"previousRevisionRef": assistantString(operation.Before, "currentRevisionRef"), "createsImmutableRevision": true}
}

func (repository *Repository) hydrateAssistantFileRevision(ctx context.Context, tx pgx.Tx, current scope, projectRef string, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	ref := assistantString(operation.Parameters, "artifactRef")
	item, err := repository.artifactRevisionReadHead(ctx, tx, current, ref)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if projectRef == "" || item.ProjectRef != projectRef || item.ScanState != "CLEAN" {
		return entity.AssistantPlanOperation{}, errs.ErrNotFound
	}
	for _, permission := range []string{"artifact.download", "artifact.revision.create"} {
		_, target, err := repository.resolveCommandTarget(ctx, tx, current, permission, "ARTIFACT", ref, projectRef)
		if err != nil {
			return entity.AssistantPlanOperation{}, err
		}
		if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
			return entity.AssistantPlanOperation{}, errs.ErrNotFound
		}
	}
	version := item.Version
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "ARTIFACT", Ref: item.Ref, Name: item.FileName, Version: &version}
	operation.Before = assistantRevisionBefore(item)
	operation.ExpectedVersion = &version
	operation.After = assistantRevisionAfter(operation)
	operation.Selected = true
	return operation, nil
}

func assistantFileRevisionCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	p := operation.Input
	if !onlyAssistantFields(p, "artifactRef", "mediaType", "contentEncoding", "content", "contentRef", "digest", "sizeBytes", "expectedVersion") ||
		!hasAssistantFields(p, "artifactRef", "mediaType", "expectedVersion") || !contains([]string{"text/plain", "text/markdown", "text/csv", "application/json"}, assistantString(p, "mediaType")) {
		return command.Command{}, errs.ErrInvalid
	}
	encoding := assistantString(p, "contentEncoding")
	if encoding != "" && encoding != "UTF8" {
		return command.Command{}, errs.ErrInvalid
	}
	expected, ok := assistantInt64(p, "expectedVersion")
	if !ok || expected < 1 {
		return command.Command{}, errs.ErrInvalid
	}
	input := command.ProjectFileRevisionInput{ArtifactRef: assistantString(p, "artifactRef"), MediaType: assistantString(p, "mediaType"),
		ContentRef: assistantString(p, "contentRef"), Digest: assistantString(p, "digest"), SourceRevisionRef: assistantString(operation.Before, "currentRevisionRef")}
	if content, supplied := p["content"]; supplied {
		body, ok := content.(string)
		if !ok || len(body) > 1<<20 || !utf8.ValidString(body) || strings.ContainsRune(body, 0) || input.ContentRef != "" || input.Digest != "" || p["sizeBytes"] != nil {
			return command.Command{}, errs.ErrInvalid
		}
		input.Content = []byte(body)
		input.SizeBytes = int64(len(input.Content))
		digest := sha256.Sum256(input.Content)
		input.Digest = fmt.Sprintf("sha256:%x", digest[:])
	} else {
		input.SizeBytes, ok = assistantInt64(p, "sizeBytes")
		if !ok || input.SizeBytes < 0 || input.SizeBytes > 1<<20 || !strings.HasPrefix(input.ContentRef, "pfcnt_") || !objectDigestValid(input.Digest) {
			return command.Command{}, errs.ErrInvalid
		}
	}
	if input.ArtifactRef == "" || input.SourceRevisionRef == "" {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: command.CreateProjectFileRevision, Payload: input, Mutation: value.Mutation{ExpectedVersion: &expected}}, nil
}

func objectDigestValid(digest string) bool {
	if len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		return false
	}
	for _, r := range digest[7:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (repository *Repository) createProjectFileRevision(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ProjectFileRevisionInput)
	if !ok || payload.Content != nil || input.Mutation.ExpectedVersion == nil || payload.Prepared == nil || payload.PreparedLedgerID == "" || payload.PreparedRevisionRef == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	// Authority повторяется до lock/OCC; cached plan, locator и replay не grants.
	if err := repository.authorizeCommand(ctx, tx, current, input); err != nil {
		return commandOutcome{}, err
	}
	item, err := repository.artifactRevisionReadHead(ctx, tx, current, payload.ArtifactRef)
	if err != nil {
		return commandOutcome{}, err
	}
	var artifactID, projectID string
	var version int64
	if err := tx.QueryRow(ctx, queryArtifactRevisionCreateLock, current.organizationID, payload.ArtifactRef).Scan(&artifactID, &projectID, &version); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion || item.Version != version || item.CurrentRevisionRef != payload.SourceRevisionRef {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	p := payload.Prepared
	if p.Digest != payload.Digest || p.SizeBytes != payload.SizeBytes || p.MediaType != payload.MediaType || p.ScanState != "CLEAN" {
		return commandOutcome{}, errs.ErrConflict
	}
	receiptRef, err := newRef("obj")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var revisionID string
	if err := tx.QueryRow(ctx, queryArtifactRevisionCreate, artifactID, payload.PreparedRevisionRef, item.Revision+1, item.FileName, p.MediaType, p.SizeBytes, p.Digest,
		item.Source, p.ScanState, receiptRef, p.PreviewState, current.actorID).Scan(&revisionID); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryArtifactRevisionCreateContent, revisionID, p.ObjectKey, p.ObjectVersion, p.ObjectETag, p.Digest, p.SizeBytes); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryArtifactRevisionAdvanceHead, artifactID, revisionID, version)
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if err := repository.adoptPreparedContentTx(ctx, tx, current, payload.PreparedLedgerID, revisionID); err != nil {
		return commandOutcome{}, err
	}
	updated, err := repository.artifactRevisionReadHead(ctx, tx, current, payload.ArtifactRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Artifact: &updated}, projectID: projectID, projectRef: updated.ProjectRef, resourceKind: "ARTIFACT", resourceRef: updated.Ref,
		summary: "i18n:ARTIFACT_REVISION_CREATED", platformEvent: "ARTIFACT_CHANGED"}, nil
}
