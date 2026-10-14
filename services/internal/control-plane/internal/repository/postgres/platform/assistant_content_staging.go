package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/assistant_prepared_content_source.sql
var queryAssistantPreparedContentSource string

// stageAssistantPlanProposal проверяет весь typed plan ДО внешней записи.
// После записи в owner command поступают только locator и safe commitment.
func (repository *Repository) stageAssistantPlanProposal(ctx context.Context, current scope, input *command.Command) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.authorizeCommand(ctx, tx, current, *input); err != nil {
		return err
	}
	var oldDigest string
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		current.organizationID, current.actorID, input.Mutation.Operation, input.Mutation.IdempotencyKey).Scan(&oldDigest, new([]byte))
	if err == nil {
		if oldDigest != input.Mutation.IntentDigest {
			return errs.ErrIdempotencyReuse
		}
		return nil
	}
	if err != pgx.ErrNoRows {
		return errs.ErrUnavailable
	}
	proposal, err := repository.prepareAssistantPlanProposalTx(ctx, tx, current, *input)
	if err != nil {
		return err
	}
	type fileStage struct {
		operation entity.AssistantPlanOperation
		request   preparedContentRequest
	}
	files := []fileStage{}
	for _, operation := range proposal.operations {
		if !assistantFileOperation(operation.Type) {
			continue
		}
		planned, err := assistantOperationCommand(operation)
		if err != nil {
			return err
		}
		binding := preparedContentBinding{ActorID: proposal.actorScope.actorID, OrganizationID: current.organizationID,
			OrganizationRef: proposal.actorScope.organizationRef, ProjectID: proposal.projectID, ProjectRef: proposal.projectRef,
			SourceLeaseRef: proposal.payload.LeaseRef, SourceLeaseGeneration: proposal.payload.Generation,
			SourceRunRef: stringMap(proposal.lease, "runRef"), OperationKey: operation.Key,
			IntentOperation: input.Mutation.Operation, IdempotencyKey: input.Mutation.IdempotencyKey, IntentDigest: input.Mutation.IntentDigest}
		fence := sha256.Sum256([]byte(proposal.payload.Fence))
		binding.SourceFenceDigest = fmt.Sprintf("%x", fence[:])
		if err := tx.QueryRow(ctx, queryAssistantPreparedContentSource, current.organizationID, proposal.lease["runtimeRevisionID"], proposal.lease["leaseID"], proposal.actorScope.actorID, operation.Type == createProjectFileRevision).
			Scan(&binding.SourceProfile, &binding.SourceProfileRef, &binding.SourceProfileVersion, &binding.SourceContextDigest); err != nil {
			return errs.ErrForbidden
		}
		var body []byte
		var fileName, mediaType, digest string
		switch p := planned.Payload.(type) {
		case command.ProjectFileInput:
			body, fileName, mediaType, digest = p.Content, p.FileName, p.MediaType, "sha256:"+p.SHA256
		case command.ProjectFileRevisionInput:
			body, mediaType, digest = p.Content, p.MediaType, p.Digest
			fileName = assistantString(operation.Before, "fileName")
			binding.TargetArtifactRef = p.ArtifactRef
			binding.SourceRevisionRef = p.SourceRevisionRef
			binding.ExpectedArtifactVersion = *operation.ExpectedVersion
			if err := tx.QueryRow(ctx, queryArtifactRevisionCreateLock, current.organizationID, p.ArtifactRef).Scan(&binding.TargetArtifactID, new(string), new(int64)); err != nil {
				return errs.ErrNotFound
			}
		default:
			return errs.ErrInvalid
		}
		if body == nil {
			return errs.ErrInvalid
		}
		verdict := artifactpolicy.Inspect(fileName, mediaType, body)
		if verdict.ScanState != artifactpolicy.ScanClean {
			return errs.ErrInvalid
		}
		files = append(files, fileStage{operation: operation, request: preparedContentRequest{Binding: binding, FileName: fileName,
			MediaType: verdict.MediaType, Digest: digest, SizeBytes: int64(len(body)), ScanState: verdict.ScanState, PreviewState: verdict.PreviewState, Body: bytes.NewReader(body)}})
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrConflict
	}
	payload := proposal.payload
	payload.PreparedContent = map[string]string{}
	for _, file := range files {
		staged, err := repository.stagePreparedContent(ctx, file.request)
		if err != nil {
			return err
		}
		for index, operation := range payload.Operations {
			if operation.Key != file.operation.Key {
				continue
			}
			operation = file.operation
			parameters := cloneAssistantFields(operation.Parameters)
			delete(parameters, "content")
			parameters["contentRef"], parameters["digest"], parameters["sizeBytes"] = staged.ContentRef, staged.Prepared.Digest, staged.Prepared.SizeBytes
			if assistantString(parameters, "contentEncoding") == "" {
				parameters["contentEncoding"] = "UTF8"
			}
			operation.Parameters = parameters
			operation.Input = nil
			if operation.Type == createProjectFileRevision {
				operation.After = assistantRevisionAfter(operation)
			} else {
				operation.After = cloneAssistantFields(parameters)
			}
			payload.Operations[index] = operation
			payload.PreparedContent[operation.Key] = staged.LedgerID
		}
	}
	input.Payload = payload
	return nil
}

func filePreparedDigest(operation entity.AssistantPlanOperation) string {
	return strings.TrimPrefix(assistantString(operation.Parameters, "digest"), "sha256:")
}

func (repository *Repository) stageAssistantPlanDraft(ctx context.Context, current scope, input *command.Command) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.authorizeCommand(ctx, tx, current, *input); err != nil {
		return err
	}
	var oldDigest string
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		current.organizationID, current.actorID, input.Mutation.Operation, input.Mutation.IdempotencyKey).Scan(&oldDigest, new([]byte))
	if err == nil {
		if oldDigest != input.Mutation.IntentDigest {
			return errs.ErrIdempotencyReuse
		}
		return nil
	}
	if err != pgx.ErrNoRows {
		return errs.ErrUnavailable
	}
	originalPayload, ok := input.Payload.(command.AssistantPlanDraftInput)
	if !ok {
		return errs.ErrInvalid
	}
	preflightInput := *input
	preflightPayload := originalPayload
	preflightPayload.Operations = append([]entity.AssistantPlanOperation(nil), originalPayload.Operations...)
	preflightInput.Payload = preflightPayload
	draft, err := repository.prepareAssistantPlanDraftTx(ctx, tx, current, preflightInput)
	if err != nil {
		return err
	}
	type draftFile struct {
		key     string
		staged  preparedContent
		request *preparedContentRequest
	}
	files := []draftFile{}
	for _, operation := range draft.operations {
		if !assistantFileOperation(operation.Type) {
			continue
		}
		original := draft.originals[operation.Key]
		oldSize, ok := assistantInt64(original.Parameters, "sizeBytes")
		if !ok {
			return errs.ErrConflict
		}
		oldContentRef, oldDigest := assistantString(original.Parameters, "contentRef"), assistantString(original.Parameters, "digest")
		binding, err := repository.readPreparedContentBindingTx(ctx, tx, draft.scope, draft.planID, draft.revision, operation.Key, oldContentRef, oldDigest, oldSize)
		if err != nil {
			return err
		}
		if _, hasBody := operation.Parameters["content"]; !hasBody {
			content, err := repository.resolvePreparedContentTx(ctx, tx, draft.scope, draft.planID, draft.revision, operation.Key, oldContentRef, oldDigest, oldSize)
			if err != nil {
				return err
			}
			files = append(files, draftFile{key: operation.Key, staged: content})
			continue
		}
		planned, err := assistantOperationCommand(operation)
		if err != nil {
			return err
		}
		var body []byte
		var fileName, mediaType, digest string
		switch file := planned.Payload.(type) {
		case command.ProjectFileInput:
			body, fileName, mediaType, digest = file.Content, file.FileName, file.MediaType, "sha256:"+file.SHA256
		case command.ProjectFileRevisionInput:
			body, fileName, mediaType, digest = file.Content, assistantString(operation.Before, "fileName"), file.MediaType, file.Digest
		default:
			return errs.ErrInvalid
		}
		verdict := artifactpolicy.Inspect(fileName, mediaType, body)
		if verdict.ScanState != artifactpolicy.ScanClean {
			return errs.ErrInvalid
		}
		binding.IntentOperation, binding.IdempotencyKey, binding.IntentDigest = input.Mutation.Operation, input.Mutation.IdempotencyKey, input.Mutation.IntentDigest
		request := preparedContentRequest{Binding: binding, FileName: fileName, MediaType: verdict.MediaType, Digest: digest, SizeBytes: int64(len(body)),
			ScanState: verdict.ScanState, PreviewState: verdict.PreviewState, Body: bytes.NewReader(body)}
		files = append(files, draftFile{key: operation.Key, request: &request})
	}
	if err := tx.Commit(ctx); err != nil {
		return errs.ErrConflict
	}
	payload := originalPayload
	payload.Operations = append([]entity.AssistantPlanOperation(nil), originalPayload.Operations...)
	payload.PreparedContent = map[string]string{}
	for _, file := range files {
		staged := file.staged
		if file.request != nil {
			staged, err = repository.stagePreparedContent(ctx, *file.request)
			if err != nil {
				return err
			}
		}
		payload.PreparedContent[file.key] = staged.LedgerID
		for index, operation := range payload.Operations {
			if operation.Key != file.key {
				continue
			}
			operation = draft.operations[index]
			parameters := cloneAssistantFields(operation.Parameters)
			delete(parameters, "content")
			parameters["contentRef"], parameters["digest"], parameters["sizeBytes"] = staged.ContentRef, staged.Prepared.Digest, staged.Prepared.SizeBytes
			if assistantString(parameters, "contentEncoding") == "" {
				parameters["contentEncoding"] = "UTF8"
			}
			operation.Parameters = parameters
			operation.Input = nil
			if operation.Type == createProjectFileRevision {
				operation.After = assistantRevisionAfter(operation)
			} else {
				operation.After = cloneAssistantFields(parameters)
			}
			payload.Operations[index] = operation
		}
	}
	input.Payload = payload
	return nil
}

func (repository *Repository) resolveAssistantFileOperationTx(ctx context.Context, tx pgx.Tx, current scope, planID string, revision int64, operation entity.AssistantPlanOperation, planned command.Command) (command.Command, error) {
	size, ok := assistantInt64(operation.Parameters, "sizeBytes")
	if !ok || !assistantProjectFileContentReady(operation) {
		return command.Command{}, errs.ErrConflict
	}
	prepared, err := repository.resolvePreparedContentTx(ctx, tx, current, planID, revision, operation.Key,
		assistantString(operation.Parameters, "contentRef"), assistantString(operation.Parameters, "digest"), size)
	if err != nil {
		return command.Command{}, err
	}
	switch file := planned.Payload.(type) {
	case command.ProjectFileInput:
		if file.FileName != prepared.FileName || file.MediaType != prepared.Prepared.MediaType || "sha256:"+file.SHA256 != prepared.Prepared.Digest || file.SizeBytes != prepared.Prepared.SizeBytes {
			return command.Command{}, errs.ErrConflict
		}
		file.Prepared = &prepared.Prepared
		file.PreparedLedgerID = prepared.LedgerID
		file.Content = nil
		planned.Payload = file
	case command.ProjectFileRevisionInput:
		if assistantString(operation.Before, "fileName") != prepared.FileName || file.MediaType != prepared.Prepared.MediaType || file.Digest != prepared.Prepared.Digest || file.SizeBytes != prepared.Prepared.SizeBytes || file.ArtifactRef != prepared.ArtifactRef {
			return command.Command{}, errs.ErrConflict
		}
		file.Prepared = &prepared.Prepared
		file.PreparedLedgerID = prepared.LedgerID
		file.PreparedRevisionRef = prepared.RevisionRef
		file.Content = nil
		planned.Payload = file
	default:
		return command.Command{}, errs.ErrInvalid
	}
	return planned, nil
}
