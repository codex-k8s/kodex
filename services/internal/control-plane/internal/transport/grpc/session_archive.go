package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// ClaimSessionArchiveTasks Resolves the principal for the exact claim RPC, asks the owner service for bounded workload tasks and casts every returned task into protobuf. Claim authority and leases are assigned by the service, not by request identifiers.
// ClaimSessionArchiveTasks Разрешает principal точного RPC получения задач, запрашивает ограниченный список задач workload у сервиса-владельца и переводит их в protobuf. Полномочия и leases назначает сервис, а не идентификаторы запроса.
func (server *Server) ClaimSessionArchiveTasks(ctx context.Context, request *controlplanev1.ClaimSessionArchiveTasksRequest) (*controlplanev1.ClaimSessionArchiveTasksResponse, error) {
	p, err := principal(ctx, controlplanev1.SessionArchiveWorkService_ClaimSessionArchiveTasks_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ClaimSessionArchiveTasks(ctx, p, request.GetWorkloadInstance(), request.GetLimit())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ClaimSessionArchiveTasksResponse{Tasks: make([]*controlplanev1.SessionArchiveTask, 0, len(items))}
	for _, item := range items {
		response.Tasks = append(response.Tasks, castSessionArchiveTask(item))
	}
	return response, nil
}

// RenewSessionArchiveTask Authorizes the exact renewal method and passes task, lease, fence and generation unchanged to the service. A successful response contains the renewed lease; service errors use the common transport mapping.
// RenewSessionArchiveTask Авторизует точный метод продления и передаёт сервису неизменённые task, lease, fence и generation. Успешный ответ содержит продлённый lease; ошибки сервиса проходят общий транспортный mapper.
func (server *Server) RenewSessionArchiveTask(ctx context.Context, request *controlplanev1.RenewSessionArchiveTaskRequest) (*controlplanev1.RenewSessionArchiveTaskResponse, error) {
	p, err := principal(ctx, controlplanev1.SessionArchiveWorkService_RenewSessionArchiveTask_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.RenewSessionArchiveTask(ctx, p, command.SessionArchiveTaskInput{
		TaskRef: request.GetTaskRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.RenewSessionArchiveTaskResponse{Lease: castLease(result)}, nil
}

// CompleteSessionSnapshot Builds the snapshot completion input from exact lease pins and object format, identity, digest and size metadata. The specialized command commits the result; this transport does not upload bytes or verify storage itself.
// CompleteSessionSnapshot Собирает завершение снимка из точных lease pins и метаданных формата, идентичности, digest и размеров объекта. Результат фиксирует отдельная команда; транспорт сам не загружает байты и не проверяет хранилище.
func (server *Server) CompleteSessionSnapshot(ctx context.Context, request *controlplanev1.CompleteSessionSnapshotRequest) (*controlplanev1.CompleteSessionSnapshotResponse, error) {
	payload := sessionArchiveLeaseInput(request.GetTaskRef(), request.GetLeaseRef(), request.GetFence(), request.GetGeneration())
	payload.FormatVersion, payload.ObjectKey = request.GetFormatVersion(), request.GetObjectKey()
	payload.ObjectVersion, payload.ObjectETag, payload.ObjectDigest = request.GetObjectVersion(), request.GetObjectEtag(), request.GetObjectDigest()
	payload.ObjectSizeBytes, payload.SourceSizeBytes = request.GetObjectSizeBytes(), request.GetSourceSizeBytes()
	result, err := server.executeSessionArchive(ctx, controlplanev1.SessionArchiveWorkService_CompleteSessionSnapshot_FullMethodName,
		command.CompleteSessionSnapshot, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CompleteSessionSnapshotResponse{TaskRef: result.taskRef, State: result.state,
		ArchiveRef: result.archiveRef, RetryScheduled: result.retryScheduled}, nil
}

// CompleteSessionRestore Carries the object identity and restored source hash/size into the dedicated restore completion command under the exact RPC principal and mutation context. It returns the owner state and retry flag without reading the restored filesystem.
// CompleteSessionRestore Передаёт идентичность объекта и hash/размер восстановленного источника отдельной команде завершения восстановления с principal точного RPC и контекстом изменения. Возвращает состояние владельца и признак повтора, не читая восстановленную файловую систему.
func (server *Server) CompleteSessionRestore(ctx context.Context, request *controlplanev1.CompleteSessionRestoreRequest) (*controlplanev1.CompleteSessionRestoreResponse, error) {
	payload := sessionArchiveLeaseInput(request.GetTaskRef(), request.GetLeaseRef(), request.GetFence(), request.GetGeneration())
	payload.FormatVersion, payload.ObjectKey = request.GetFormatVersion(), request.GetObjectKey()
	payload.ObjectVersion, payload.ObjectETag, payload.ObjectDigest = request.GetObjectVersion(), request.GetObjectEtag(), request.GetObjectDigest()
	payload.ObjectSizeBytes, payload.RestoredSourceSHA256 = request.GetObjectSizeBytes(), request.GetRestoredSourceSha256()
	payload.SourceSizeBytes = request.GetRestoredSourceSizeBytes()
	result, err := server.executeSessionArchive(ctx, controlplanev1.SessionArchiveWorkService_CompleteSessionRestore_FullMethodName,
		command.CompleteSessionRestore, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CompleteSessionRestoreResponse{TaskRef: result.taskRef, State: result.state,
		ArchiveRef: result.archiveRef, RetryScheduled: result.retryScheduled}, nil
}

// CompleteSessionPVCDeletion Reports deletion of the named PVC with task lease/fence/generation to the dedicated owner command. The PVC name is evidence input, not authority to delete an arbitrary volume; no Kubernetes action occurs in this adapter.
// CompleteSessionPVCDeletion Сообщает отдельной команде владельца об удалении указанного PVC с task lease/fence/generation. Имя PVC является входным свидетельством, а не правом удалить любой том; адаптер не выполняет действий Kubernetes.
func (server *Server) CompleteSessionPVCDeletion(ctx context.Context, request *controlplanev1.CompleteSessionPVCDeletionRequest) (*controlplanev1.CompleteSessionPVCDeletionResponse, error) {
	payload := sessionArchiveLeaseInput(request.GetTaskRef(), request.GetLeaseRef(), request.GetFence(), request.GetGeneration())
	payload.PVCName = request.GetPvcName()
	result, err := server.executeSessionArchive(ctx, controlplanev1.SessionArchiveWorkService_CompleteSessionPVCDeletion_FullMethodName,
		command.CompleteSessionPVCDeletion, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CompleteSessionPVCDeletionResponse{TaskRef: result.taskRef, State: result.state,
		ArchiveRef: result.archiveRef, RetryScheduled: result.retryScheduled}, nil
}

// CompleteSessionObjectDeletion Reports the exact object key and version under the archive-task lease to the owner deletion completion. It neither deletes S3 data nor treats the payload key as ownership proof.
// CompleteSessionObjectDeletion Сообщает о точных key и version объекта под lease задачи отдельной команде завершения удаления. Сам не удаляет данные S3 и не считает key из payload доказательством владения.
func (server *Server) CompleteSessionObjectDeletion(ctx context.Context, request *controlplanev1.CompleteSessionObjectDeletionRequest) (*controlplanev1.CompleteSessionObjectDeletionResponse, error) {
	payload := sessionArchiveLeaseInput(request.GetTaskRef(), request.GetLeaseRef(), request.GetFence(), request.GetGeneration())
	payload.ObjectKey, payload.ObjectVersion = request.GetObjectKey(), request.GetObjectVersion()
	result, err := server.executeSessionArchive(ctx, controlplanev1.SessionArchiveWorkService_CompleteSessionObjectDeletion_FullMethodName,
		command.CompleteSessionObjectDeletion, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CompleteSessionObjectDeletionResponse{TaskRef: result.taskRef, State: result.state,
		ArchiveRef: result.archiveRef, RetryScheduled: result.retryScheduled}, nil
}

// FailSessionArchiveTask Forwards a safe failure code and exact task lease pins to the owner failure transition. Retry scheduling and terminal state come from the command result and are never decided by the transport.
// FailSessionArchiveTask Передаёт безопасный код ошибки и точные lease pins задачи переходу отказа владельца. Планирование повтора и итоговое состояние берутся из результата команды и не определяются транспортом.
func (server *Server) FailSessionArchiveTask(ctx context.Context, request *controlplanev1.FailSessionArchiveTaskRequest) (*controlplanev1.FailSessionArchiveTaskResponse, error) {
	payload := sessionArchiveLeaseInput(request.GetTaskRef(), request.GetLeaseRef(), request.GetFence(), request.GetGeneration())
	payload.SafeErrorCode = request.GetSafeErrorCode()
	result, err := server.executeSessionArchive(ctx, controlplanev1.SessionArchiveWorkService_FailSessionArchiveTask_FullMethodName,
		command.FailSessionArchiveTask, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.FailSessionArchiveTaskResponse{TaskRef: result.taskRef, State: result.state,
		ArchiveRef: result.archiveRef, RetryScheduled: result.retryScheduled}, nil
}

// sessionArchiveTaskResult holds the narrow owner command projection shared by distinct archive completion responses; it is not a stored task or a lease grant.
// sessionArchiveTaskResult хранит узкую проекцию команды владельца для разных ответов завершения архивирования; это не хранимая задача и не выдача lease.
type sessionArchiveTaskResult struct {
	taskRef, state, archiveRef string
	retryScheduled             bool
}

// executeSessionArchive Runs one explicit archive command through the common authenticated executor and projects only task, state, archive and retry fields from its runtime result. Missing or wrongly typed fields keep the existing zero-value behavior.
// executeSessionArchive Выполняет одну явную архивную команду через общий авторизованный executor и извлекает из runtime-результата только task, state, archive и retry. Отсутствующие поля и неверные типы сохраняют прежнее поведение нулевого значения.
func (server *Server) executeSessionArchive(ctx context.Context, method string, kind command.Kind, mutation *controlplanev1.MutationContext, payload command.SessionArchiveTaskInput) (sessionArchiveTaskResult, error) {
	result, err := execute(ctx, server.service, method, kind, mutation, payload)
	if err != nil {
		return sessionArchiveTaskResult{}, err
	}
	return sessionArchiveTaskResult{taskRef: mapString(result.Runtime, "taskRef"), state: mapString(result.Runtime, "state"),
		archiveRef: mapString(result.Runtime, "archiveRef"), retryScheduled: mapBool(result.Runtime, "retryScheduled")}, nil
}

// sessionArchiveLeaseInput Copies the exact task, lease, fence and monotonic generation into the shared command input. It performs no lookup or validation and does not derive authority from these caller-supplied identifiers.
// sessionArchiveLeaseInput Копирует точные task, lease, fence и монотонное поколение в общий input команды. Не делает lookup и проверок и не выводит полномочия из переданных вызывающим идентификаторов.
func sessionArchiveLeaseInput(taskRef, leaseRef, fence string, generation int64) command.SessionArchiveTaskInput {
	return command.SessionArchiveTaskInput{TaskRef: taskRef, LeaseRef: leaseRef, Fence: fence, Generation: generation}
}

// mapBool Returns a boolean map member or false for a missing key or another type. It performs no string or numeric coercion and does not mutate the result map.
// mapBool Возвращает булево поле map либо false при отсутствии ключа или другом типе. Не приводит строки и числа и не меняет map результата.
func mapBool(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

// castSessionArchiveTask Projects the closed task-kind mapping, tenant/session/runtime pins, source metadata and lease into protobuf. An archive binding is included only for a nested map with a nonempty archiveRef; absent or unknown values keep the established zero defaults.
// castSessionArchiveTask Переводит закрытое сопоставление вида задачи, tenant/session/runtime pins, метаданные источника и lease в protobuf. Архивная привязка включается только для вложенной map с непустым archiveRef; отсутствующие и неизвестные значения сохраняют прежние нули.
func castSessionArchiveTask(values map[string]any) *controlplanev1.SessionArchiveTask {
	kinds := map[string]controlplanev1.SessionArchiveTaskKind{
		"SNAPSHOT":      controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_SNAPSHOT,
		"RESTORE":       controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_RESTORE,
		"DELETE_PVC":    controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_DELETE_PVC,
		"DELETE_OBJECT": controlplanev1.SessionArchiveTaskKind_SESSION_ARCHIVE_TASK_KIND_DELETE_OBJECT,
	}
	result := &controlplanev1.SessionArchiveTask{TaskRef: mapString(values, "taskRef"), Kind: kinds[mapString(values, "kind")],
		OrganizationRef: mapString(values, "organizationRef"), ProjectRef: mapString(values, "projectRef"),
		SessionRef: mapString(values, "sessionRef"), ProviderAccountRef: mapString(values, "providerAccountRef"),
		RuntimeRevisionRef: mapString(values, "runtimeRevisionRef"), RuntimeRevisionVersion: mapInt64(values, "runtimeRevisionVersion"),
		RuntimeRevisionDigest: mapString(values, "runtimeRevisionDigest"), CodexSessionId: mapString(values, "codexSessionID"),
		ContentGeneration: mapInt64(values, "contentGeneration"), PvcName: mapString(values, "pvcName"),
		InputDigest: mapString(values, "inputDigest"), SourceRelativePath: mapString(values, "sourceRelativePath"),
		SourceSha256: mapString(values, "sourceSHA256"), SourceSizeBytes: mapInt64(values, "sourceSizeBytes"),
		TargetObjectKey: mapString(values, "objectKey"), TargetObjectVersion: mapString(values, "objectVersion"),
		Attempt: int32(mapInt64(values, "attempt")), Lease: castLease(values)}
	if archive, ok := values["archive"].(map[string]any); ok && mapString(archive, "archiveRef") != "" {
		result.Archive = &controlplanev1.SessionArchiveBinding{ArchiveRef: mapString(archive, "archiveRef"),
			FormatVersion: uint32(mapInt64(archive, "formatVersion")), ObjectKey: mapString(archive, "objectKey"),
			ObjectVersion: mapString(archive, "objectVersion"), ObjectEtag: mapString(archive, "objectETag"),
			ObjectDigest: mapString(archive, "objectDigest"), ObjectSizeBytes: mapInt64(archive, "objectSizeBytes"),
			SourceRelativePath: mapString(archive, "sourceRelativePath"), SourceSha256: mapString(archive, "sourceSHA256"),
			SourceSizeBytes: mapInt64(archive, "sourceSizeBytes")}
	}
	return result
}
