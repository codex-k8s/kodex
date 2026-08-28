package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

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

type sessionArchiveTaskResult struct {
	taskRef, state, archiveRef string
	retryScheduled             bool
}

func (server *Server) executeSessionArchive(ctx context.Context, method string, kind command.Kind, mutation *controlplanev1.MutationContext, payload command.SessionArchiveTaskInput) (sessionArchiveTaskResult, error) {
	result, err := execute(ctx, server.service, method, kind, mutation, payload)
	if err != nil {
		return sessionArchiveTaskResult{}, err
	}
	return sessionArchiveTaskResult{taskRef: mapString(result.Runtime, "taskRef"), state: mapString(result.Runtime, "state"),
		archiveRef: mapString(result.Runtime, "archiveRef"), retryScheduled: mapBool(result.Runtime, "retryScheduled")}, nil
}

func sessionArchiveLeaseInput(taskRef, leaseRef, fence string, generation int64) command.SessionArchiveTaskInput {
	return command.SessionArchiveTaskInput{TaskRef: taskRef, LeaseRef: leaseRef, Fence: fence, Generation: generation}
}

func mapBool(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

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
