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

// queryAssistantPreparedContentSource embeds the exact active assistant conversation/runtime
// revision/live lease lineage, deadline and profile snapshot check, including artifact.manage for
// file revision creation. It returns source identity/version/context digest, never file bytes or
// independent authority.
// queryAssistantPreparedContentSource встраивает точную проверку активного разговора ассистента,
// runtime-ревизии, действующей аренды, срока исполнения и снимка профиля, включая artifact.manage
// для новой ревизии файла. Возвращает идентичность, версию и дайджест исходного контекста, но не
// содержимое файла и не самостоятельные полномочия.
//
//go:embed sql/assistant_prepared_content_source.sql
var queryAssistantPreparedContentSource string

// stageAssistantPlanProposal authorizes and validates the entire typed proposal in a ReadCommitted
// preflight transaction, honoring existing idempotency receipts before external storage. It binds
// each inspected CLEAN file to actor/project/lease/fence/intent metadata, commits preflight, then
// stages files and replaces inline content with locators and commitments. A later staging error
// can leave earlier external objects; the final owner command must revalidate and own atomic plan
// writes.
// stageAssistantPlanProposal проверяет доступ и весь типизированный план в предварительной
// ReadCommitted транзакции, учитывая существующие квитанции идемпотентности до внешнего хранения.
// Связывает каждый проверенный CLEAN файл с субъектом, проектом, арендой, fence и намерением,
// завершает предварительную транзакцию, затем сохраняет файлы и заменяет inline-содержимое
// ссылками и обязательствами. Поздняя ошибка может оставить ранее созданные внешние объекты;
// итоговая команда владельца повторно проверяет состояние и атомарно записывает план.
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
	// fileStage pairs one hydrated file operation with its exact prepared-content request until all
	// proposal validation completes. It is local staging data; constructing it performs no external
	// write.
	// fileStage связывает подготовленную файловую операцию с точным запросом хранения до завершения
	// проверки всего предложения. Это локальные данные подготовки; их создание не выполняет внешнюю
	// запись.
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

// filePreparedDigest returns the operation digest with one leading sha256: prefix removed. It does
// not validate hexadecimal form, length, provenance or content equality.
// filePreparedDigest возвращает дайджест операции без одного начального префикса sha256:. Не
// проверяет шестнадцатеричный формат, длину, происхождение и соответствие содержимому.
func filePreparedDigest(operation entity.AssistantPlanOperation) string {
	return strings.TrimPrefix(assistantString(operation.Parameters, "digest"), "sha256:")
}

// stageAssistantPlanDraft preflights an edited plan in a ReadCommitted transaction using a copied
// operation slice, exact prior prepared-content bindings and authorized draft snapshots. Unchanged
// content is resolved from its ledger; changed content is inspected before external staging after
// commit. It builds a copied payload with locators and no inline content; partial external staging
// can survive an error and requires the existing ledger/cleanup protocol.
// stageAssistantPlanDraft предварительно проверяет изменённый план в ReadCommitted транзакции с
// копией среза операций, точными прежними привязками подготовленного содержимого и проверенными
// снимками черновика. Неизменное содержимое разрешается через реестр; изменённое проверяется до
// внешнего сохранения после Commit. Собирает копию payload со ссылками без inline-содержимого;
// частичные внешние записи могут сохраниться при ошибке и требуют существующего протокола реестра
// и очистки.
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
	// draftFile carries either already resolved prepared content or a new staging request for one
	// operation key. A nil request means reuse; this local union does not itself authorize or write
	// content.
	// draftFile хранит уже разрешённое подготовленное содержимое либо новый запрос хранения для ключа
	// операции. Nil запрос означает повторное использование; эта локальная структура сама не проверяет
	// полномочия и не записывает содержимое.
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

// resolveAssistantFileOperationTx resolves exact plan/revision/operation content pins in the
// caller transaction, then checks filename, media type, digest, size and revision target as
// applicable. It installs prepared descriptors and ledger references into a copied typed command
// and clears raw Content; no external file write or commit occurs here.
// resolveAssistantFileOperationTx разрешает точные привязки содержимого к плану, ревизии и
// операции в переданной транзакции, затем сверяет имя, тип, дайджест, размер и цель ревизии, где
// применимо. Добавляет подготовленные дескрипторы и ссылки реестра в копию типизированной команды
// и очищает Content; внешней записи файла и завершения транзакции здесь нет.
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
