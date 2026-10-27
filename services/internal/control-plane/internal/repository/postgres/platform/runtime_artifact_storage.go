package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// prepareCommandObjects routes assistant plan staging separately and preflights typed execution
// artifacts under current authorization, idempotency and exact lease/write/deadline checks.
// Expired work validates bytes without uploading; live work commits preflight, then
// validates/scans/uploads at most16 bounded artifacts and replaces content with prepared
// descriptors. Later errors can leave partial payload mutations; cleanup is best effort and the
// outer owner transaction must revalidate and commit metadata.
// prepareCommandObjects отдельно направляет staging планов ассистента и предварительно проверяет
// типизированные результаты исполнения по текущим полномочиям, идемпотентности, точным
// lease/write/deadline. Просроченная работа проверяет байты без загрузки; действующая завершает
// preflight, затем проверяет, сканирует и загружает максимум16 ограниченных артефактов, заменяя
// содержимое дескрипторами. Поздние ошибки могут оставить частично изменённый payload; очистка не
// гарантирована, внешняя owner-транзакция повторно проверяет и фиксирует метаданные.
func (repository *Repository) prepareCommandObjects(ctx context.Context, scope scope, input *command.Command) ([]objectstorage.Receipt, error) {
	if input == nil {
		return nil, nil
	}
	if input.Kind == command.ProposeAssistantPlan {
		return nil, repository.stageAssistantPlanProposal(ctx, scope, input)
	}
	if input.Kind == command.UpdateAssistantPlan {
		return nil, repository.stageAssistantPlanDraft(ctx, scope, input)
	}
	if input.Kind == command.ApplyAssistantPlan {
		return nil, nil
	}
	if input.Kind != command.CompleteExecution {
		return nil, nil
	}
	payload, ok := input.Payload.(command.CompleteExecutionInput)
	if !ok || len(payload.Artifacts) == 0 {
		return nil, nil
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.authorizeCommand(ctx, tx, scope, *input); err != nil {
		return nil, err
	}
	var storedDigest string
	var storedPayload []byte
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		scope.organizationID, scope.actorID, input.Mutation.Operation, input.Mutation.IdempotencyKey).
		Scan(&storedDigest, &storedPayload)
	if err == nil {
		if storedDigest != input.Mutation.IntentDigest {
			return nil, errs.ErrIdempotencyReuse
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, errs.ErrConflict
		}
		return nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrUnavailable
	}
	lease, err := repository.lease(ctx, tx, scope, command.LeaseInput{
		LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation,
	}, true)
	if err != nil {
		return nil, err
	}
	if err := repository.requireRuntimeArtifactWrite(ctx, tx, scope, lease); err != nil {
		return nil, err
	}
	_, expired, err := readRuntimeExecutionDeadline(ctx, tx, scope.organizationID, stringMap(lease, "runID"))
	if err != nil {
		return nil, err
	}
	if expired {
		// Expiry будет committed основной owner-транзакцией. До неё нельзя
		// публиковать новые objects; исходная валидация bytes сохраняется.
		var total int64
		for _, artifact := range payload.Artifacts {
			if len(payload.Artifacts) > 16 || artifact.Prepared != nil || artifact.FileName == "" || safeFileName(artifact.FileName) != artifact.FileName ||
				artifact.SizeBytes != int64(len(artifact.Content)) || artifact.SizeBytes < 0 || artifact.SizeBytes > 1<<20 {
				return nil, errs.ErrInvalid
			}
			total += artifact.SizeBytes
			digest := sha256.Sum256(artifact.Content)
			if total > maximumArtifactBytes || !strings.EqualFold(strings.TrimSpace(artifact.SHA256), hex.EncodeToString(digest[:])) ||
				artifactpolicy.Inspect(artifact.FileName, artifact.MediaType, artifact.Content).ScanState != artifactpolicy.ScanClean {
				return nil, errs.ErrInvalid
			}
		}
		return nil, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}

	prepared := make([]objectstorage.Receipt, 0, len(payload.Artifacts))
	var totalBytes int64
	for index := range payload.Artifacts {
		artifact := &payload.Artifacts[index]
		if len(payload.Artifacts) > 16 || artifact.Prepared != nil || artifact.FileName == "" ||
			safeFileName(artifact.FileName) != artifact.FileName || artifact.SizeBytes != int64(len(artifact.Content)) ||
			artifact.SizeBytes < 0 || artifact.SizeBytes > 1<<20 {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, errs.ErrInvalid
		}
		totalBytes += artifact.SizeBytes
		if totalBytes > maximumArtifactBytes {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, errs.ErrInvalid
		}
		digest := sha256.Sum256(artifact.Content)
		digestHex := hex.EncodeToString(digest[:])
		if !strings.EqualFold(strings.TrimSpace(artifact.SHA256), digestHex) {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, errs.ErrInvalid
		}
		verdict := artifactpolicy.Inspect(artifact.FileName, artifact.MediaType, artifact.Content)
		if verdict.ScanState != artifactpolicy.ScanClean {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, errs.ErrInvalid
		}
		ref, refErr := newRef("art")
		if refErr != nil {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, errs.ErrUnavailable
		}
		digestValue := "sha256:" + digestHex
		scan, scanErr := repository.scanArtifact(ctx, bytes.NewReader(artifact.Content), digestValue, artifact.SizeBytes)
		if scanErr != nil || scan.Infected {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			if scanErr != nil {
				return nil, scanErr
			}
			return nil, errs.ErrInvalid
		}
		key := artifactObjectKey(scope.organizationRef, scope.actorRef, stringMap(lease, "projectRef"), ref, digestValue)
		receipt, putErr := repository.objects.Put(ctx, objectstorage.PutInput{
			Key: key, MediaType: verdict.MediaType, Digest: digestValue,
			SizeBytes: artifact.SizeBytes, Body: bytes.NewReader(artifact.Content),
		})
		if putErr != nil {
			repository.cleanupPreparedObjects(ctx, prepared, false)
			return nil, mapObjectStorageError(putErr)
		}
		prepared = append(prepared, receipt)
		artifact.Prepared = &command.PreparedArtifact{
			Ref: ref, ObjectKey: receipt.Key, ObjectVersion: receipt.VersionID,
			ObjectETag: receipt.ETag, MediaType: verdict.MediaType,
			Digest: receipt.Digest, SizeBytes: receipt.SizeBytes,
			ScanState: verdict.ScanState, PreviewState: verdict.PreviewState,
			Antivirus: scan,
		}
		artifact.Content = nil
	}
	input.Payload = payload
	return prepared, nil
}

// requireRuntimeArtifactWrite requires the artifact capability in both current agent and pinned
// runtime revision, resolves the active root actor and checks upload access to the lease project.
// Missing/denied cases are Forbidden; query errors are Unavailable. The actor substitution affects
// only the local scope copy, not caller identity.
// requireRuntimeArtifactWrite требует artifact capability у текущего агента и закреплённой
// runtime-ревизии, разрешает активного root-субъекта и проверяет upload-доступ к проекту аренды.
// Отсутствие либо отказ дают Forbidden, ошибка запроса — Unavailable. Замена субъекта затрагивает
// только локальную копию scope, а не идентичность caller.
func (repository *Repository) requireRuntimeArtifactWrite(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any) error {
	var allowed bool
	var actorRef string
	if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectAgentCapability, current.organizationID,
		runtimecontract.ArtifactCapability, lease["nodeID"], lease["leaseID"]).Scan(&allowed, &actorRef); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	} else if err != nil {
		return errs.ErrUnavailable
	}
	if !allowed {
		return errs.ErrForbidden
	}
	current.actorRef = actorRef
	if err := repository.requireArtifactUploadAccess(ctx, tx, current, stringMap(lease, "projectRef")); err != nil {
		return errs.ErrForbidden
	}
	return nil
}

// cleanupPreparedObjects keeps receipts when requested; otherwise attempts version-specific
// deletes under one independent five-second cleanup context derived without cancellation. Delete
// errors are ignored and the shared budget may expire before all objects are removed; this helper
// proves neither cleanup completion nor metadata rollback.
// cleanupPreparedObjects сохраняет объекты при keep; иначе пытается удалить точные версии по
// квитанциям в одном независимом пятисекундном контексте без унаследованной отмены. Ошибки Delete
// игнорируются, общий бюджет может закончиться до очистки всех объектов; helper не доказывает
// завершение очистки и rollback метаданных.
func (repository *Repository) cleanupPreparedObjects(ctx context.Context, prepared []objectstorage.Receipt, keep bool) {
	if keep {
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, receipt := range prepared {
		_ = repository.objects.Delete(cleanupContext, receipt.Key, receipt.VersionID)
	}
}

// resultContainsPreparedObjects checks receipt-count capacity and membership of every server-ref
// path segment in result.CreatedRefs before cleanup is suppressed. Empty receipts succeed;
// malformed keys or missing refs fail. It is a containment guard, not an object-store readback,
// transaction receipt or cryptographic proof.
// resultContainsPreparedObjects проверяет вместимость CreatedRefs и наличие в нём каждого
// серверного ref из пути квитанции перед подавлением очистки. Пустой список квитанций успешен;
// повреждённые пути и отсутствующие refs дают отказ. Это проверка включения, а не readback object
// storage, квитанция транзакции или криптографическое доказательство.
func resultContainsPreparedObjects(result command.Result, prepared []objectstorage.Receipt) bool {
	if len(prepared) == 0 {
		return true
	}
	if len(result.CreatedRefs) < len(prepared) {
		return false
	}
	// Prepared keys содержат назначенный сервером artifact ref. Перед подавлением
	// очистки результат должен содержать каждый из этих refs.
	for _, receipt := range prepared {
		parts := strings.Split(receipt.Key, "/")
		if len(parts) < 2 {
			return false
		}
		ref := parts[len(parts)-2]
		found := false
		for _, created := range result.CreatedRefs {
			if created == ref {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// preparedArtifact requires a prepared descriptor without inline content and validates
// key/digest/size/media, CLEAN policy and matching noninfected antivirus metadata. It returns a
// descriptor copy or Invalid; object version/ETag and actual stored bytes are not independently
// read or verified here.
// preparedArtifact требует подготовленный дескриптор без inline-содержимого и проверяет
// key/digest/size/media, CLEAN policy и совпадающие незаражённые AV-метаданные. Возвращает копию
// дескриптора либо Invalid; версия объекта, ETag и реальные байты здесь независимо не читаются и
// не проверяются.
func preparedArtifact(artifact command.CompletedArtifact) (command.PreparedArtifact, error) {
	if artifact.Prepared == nil || artifact.Content != nil ||
		artifact.Prepared.Ref == "" || !objectstorage.ValidKey(artifact.Prepared.ObjectKey) ||
		!objectstorage.ValidDigest(artifact.Prepared.Digest) || artifact.Prepared.SizeBytes != artifact.SizeBytes ||
		artifact.Prepared.Digest != "sha256:"+strings.ToLower(strings.TrimSpace(artifact.SHA256)) ||
		artifact.Prepared.MediaType == "" || artifact.Prepared.ScanState != artifactpolicy.ScanClean ||
		!validArtifactAntivirus(artifact.Prepared.Antivirus) || artifact.Prepared.Antivirus.Infected ||
		artifact.Prepared.Antivirus.Digest != artifact.Prepared.Digest || artifact.Prepared.Antivirus.SizeBytes != artifact.SizeBytes {
		return command.PreparedArtifact{}, errs.ErrInvalid
	}
	return *artifact.Prepared, nil
}

// mapPreparedObjectError preserves object storage Conflict as the domain Conflict sentinel and
// delegates all other failures to the shared storage mapper. It performs no retry, cleanup or
// outcome reconciliation.
// mapPreparedObjectError сохраняет object storage Conflict как доменный Conflict, остальные ошибки
// передаёт общему mapper хранилища. Не выполняет повтор, очистку и установление исхода операции.
func mapPreparedObjectError(err error) error {
	if errors.Is(err, objectstorage.ErrConflict) {
		return errs.ErrConflict
	}
	return mapObjectStorageError(err)
}
