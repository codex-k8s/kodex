package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) ListBackups(
	ctx context.Context,
	request *controlplanev1.ListBackupsRequest,
) (*controlplanev1.ListBackupsResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_ListBackups_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	limit := int(request.GetPageSize())
	if limit == 0 {
		limit = 50
	}
	backups, err := server.service.ListBackups(ctx, resource.ListBackupsInput{
		Principal: principal, AfterID: request.GetPageToken(), Limit: limit,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListBackupsResponse{
		Backups: make([]*controlplanev1.Backup, 0, len(backups)),
	}
	for _, backup := range backups {
		projected, projectErr := backupToProto(backup)
		if projectErr != nil {
			return nil, rpcError(principal.CorrelationID, projectErr)
		}
		response.Backups = append(response.Backups, projected)
	}
	if len(backups) == limit {
		response.NextPageToken = backups[len(backups)-1].ID
	}
	return response, nil
}

func (server *Server) GetBackup(
	ctx context.Context,
	request *controlplanev1.GetBackupRequest,
) (*controlplanev1.GetBackupResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_GetBackup_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	backup, err := server.service.GetBackup(ctx, resource.GetBackupInput{
		Principal: principal, BackupID: request.GetBackupId(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	projected, err := backupToProto(backup)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.GetBackupResponse{Backup: projected}, nil
}

func (server *Server) RestoreBackup(
	ctx context.Context,
	request *controlplanev1.RestoreBackupRequest,
) (*controlplanev1.RestoreBackupResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_RestoreBackup_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	operation, err := server.service.RestoreBackup(ctx, resource.RestoreBackupInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		BackupID: request.GetBackupId(), ExpectedSourceVersion: request.GetExpectedSourceVersion(),
		ArchiveSHA256: request.GetArchiveSha256(), ProvenanceSHA256: request.GetProvenanceSha256(),
		Scope: request.GetScope(), ScopeID: request.GetScopeId(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	projected, err := restoreOperationToProto(operation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.RestoreBackupResponse{Operation: projected}, nil
}

func (server *Server) GetRestoreOperation(
	ctx context.Context,
	request *controlplanev1.GetRestoreOperationRequest,
) (*controlplanev1.GetRestoreOperationResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_GetRestoreOperation_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	operation, err := server.service.GetRestoreOperation(ctx, resource.GetRestoreOperationInput{
		Principal: principal, OperationID: request.GetRestoreOperationId(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	projected, err := restoreOperationToProto(operation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.GetRestoreOperationResponse{Operation: projected}, nil
}

func (server *Server) ListRestoreOperations(
	ctx context.Context,
	request *controlplanev1.ListRestoreOperationsRequest,
) (*controlplanev1.ListRestoreOperationsResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_ListRestoreOperations_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	limit := int(request.GetPageSize())
	if limit == 0 {
		limit = 50
	}
	operations, err := server.service.ListRestoreOperations(ctx, resource.ListRestoreOperationsInput{
		Principal: principal, BackupID: request.GetBackupId(), AfterID: request.GetPageToken(), Limit: limit,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListRestoreOperationsResponse{
		Operations: make([]*controlplanev1.RestoreOperation, 0, len(operations)),
	}
	for _, operation := range operations {
		projected, projectErr := restoreOperationToProto(operation)
		if projectErr != nil {
			return nil, rpcError(principal.CorrelationID, projectErr)
		}
		response.Operations = append(response.Operations, projected)
	}
	if len(operations) == limit {
		response.NextPageToken = operations[len(operations)-1].ID
	}
	return response, nil
}

func backupToProto(backup domainrepo.Backup) (*controlplanev1.Backup, error) {
	states := map[string]controlplanev1.BackupState{
		"VERIFYING":         controlplanev1.BackupState_BACKUP_STATE_VERIFYING,
		"RETENTION_PENDING": controlplanev1.BackupState_BACKUP_STATE_RETENTION_PENDING,
		"AVAILABLE":         controlplanev1.BackupState_BACKUP_STATE_AVAILABLE,
		"RESTORING":         controlplanev1.BackupState_BACKUP_STATE_RESTORING,
		"RESTORED":          controlplanev1.BackupState_BACKUP_STATE_RESTORED,
		"RESTORE_FAILED":    controlplanev1.BackupState_BACKUP_STATE_RESTORE_FAILED,
		"RESTORE_CANCELLED": controlplanev1.BackupState_BACKUP_STATE_RESTORE_CANCELLED,
		"RESTORE_EXPIRED":   controlplanev1.BackupState_BACKUP_STATE_RESTORE_EXPIRED,
		"EXPIRED":           controlplanev1.BackupState_BACKUP_STATE_EXPIRED,
		"UNAVAILABLE":       controlplanev1.BackupState_BACKUP_STATE_UNAVAILABLE,
	}
	state := states[backup.State]
	if state == controlplanev1.BackupState_BACKUP_STATE_UNSPECIFIED ||
		backup.ID == "" || backup.SourceVersion == 0 || backup.SessionID == "" ||
		backup.Restorable != (backup.State == "AVAILABLE") ||
		!validPublicSHA256(backup.SourceRuntimeRevisionSHA256) ||
		!validPublicSHA256(backup.SourceImmutableInputSHA256) ||
		!validPublicSHA256(backup.ArchiveSHA256) ||
		!validPublicSHA256(backup.ProvenanceSHA256) {
		return nil, errs.ErrInternal
	}
	result := &controlplanev1.Backup{
		BackupId: backup.ID, SourceVersion: backup.SourceVersion,
		SourceRuntimeRevisionSha256: backup.SourceRuntimeRevisionSHA256,
		SourceImmutableInputSha256:  backup.SourceImmutableInputSHA256,
		ArchiveSha256:               backup.ArchiveSHA256, ProvenanceSha256: backup.ProvenanceSHA256,
		State: state, Scope: "SESSION", ScopeId: backup.SessionID, Restorable: backup.Restorable,
		CreatedAt: timestamppb.New(backup.CreatedAt), UpdatedAt: timestamppb.New(backup.UpdatedAt),
		RestoreOperationId: backup.RestoreOperationID,
	}
	result.AvailableAt = optionalTimestamp(backup.AvailableAt)
	result.RetainUntil = optionalTimestamp(backup.RetainUntil)
	if !result.CreatedAt.IsValid() || !result.UpdatedAt.IsValid() ||
		(result.AvailableAt != nil && !result.AvailableAt.IsValid()) ||
		(result.RetainUntil != nil && !result.RetainUntil.IsValid()) {
		return nil, errs.ErrInternal
	}
	return result, nil
}

func restoreOperationToProto(
	operation domainrepo.RuntimeRestoreOperation,
) (*controlplanev1.RestoreOperation, error) {
	const versionWindow uint64 = 1 << 32
	state, errorCode := restoreOperationState(operation)
	if state == controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_UNSPECIFIED ||
		operation.ID == "" || operation.SourceVersion == 0 || operation.SessionID == "" ||
		operation.TargetTurnID == "" || operation.TargetAttempt == 0 || operation.Generation == 0 ||
		operation.Generation > 100 || operation.TargetExecutionVersion >= versionWindow-1 ||
		!validPublicSHA256(operation.ArchiveSHA256) ||
		!validPublicSHA256(operation.ProvenanceSHA256) {
		return nil, errs.ErrInternal
	}
	// Generation занимает непересекающееся окно: successor никогда не
	// уменьшает public version, а lifecycle target остаётся versioned внутри окна.
	version := (operation.Generation-1)*versionWindow + operation.TargetExecutionVersion + 1
	partial := (operation.TargetRestoreAssignmentState == "BOUND" || operation.TargetRestoreAssignmentState == "CONSUMED") &&
		(operation.TargetExecutionState == "FAILED" || operation.TargetExecutionState == "CANCELLED" ||
			operation.TargetExecutionState == "EXPIRED")
	result := &controlplanev1.RestoreOperation{
		RestoreOperationId: operation.ID, Version: version, State: state,
		BackupId: operation.BackupID, SourceVersion: operation.SourceVersion,
		ArchiveSha256: operation.ArchiveSHA256, ProvenanceSha256: operation.ProvenanceSHA256,
		Scope: "SESSION", ScopeId: operation.SessionID, TargetTurnId: operation.TargetTurnID,
		TargetAttempt: operation.TargetAttempt, ErrorCode: errorCode,
		Generation: operation.Generation, Partial: partial, NextAction: restoreOperationNextAction(operation, state),
		CreatedAt: timestamppb.New(operation.CreatedAt), UpdatedAt: timestamppb.New(operation.UpdatedAt),
	}
	if !result.CreatedAt.IsValid() || !result.UpdatedAt.IsValid() {
		return nil, errs.ErrInternal
	}
	return result, nil
}

func validPublicSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func restoreOperationState(
	operation domainrepo.RuntimeRestoreOperation,
) (controlplanev1.RestoreOperationState, string) {
	switch operation.TargetExecutionState {
	case "":
		switch operation.TargetTurnState {
		case "FAILED":
			return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_FAILED, "RESTORE_TARGET_FAILED_BEFORE_CLAIM"
		case "CANCELLED":
			return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_CANCELLED, ""
		case "EXPIRED":
			return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_EXPIRED, ""
		default:
			if operation.Generation > 1 {
				return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_RETRYING, ""
			}
			return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_QUEUED, ""
		}
	case "PENDING":
		switch operation.TargetRestoreAssignmentState {
		case "ASSIGNED":
			return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_ASSIGNED, ""
		case "BOUND":
			return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_MATERIALIZING, ""
		case "CONSUMED":
			return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_READY, ""
		}
	case "ADMITTED":
		return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_READY, ""
	case "RUNNING":
		return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_RUNNING, ""
	case "SUCCEEDED":
		return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_SUCCEEDED, ""
	case "FAILED":
		return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_FAILED, "RESTORE_EXECUTION_FAILED"
	case "CANCELLED":
		return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_CANCELLED, ""
	case "EXPIRED":
		return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_EXPIRED, ""
	case "RETRIED":
		return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_RETRYING, ""
	}
	return controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_UNSPECIFIED, ""
}

func restoreOperationNextAction(
	operation domainrepo.RuntimeRestoreOperation,
	state controlplanev1.RestoreOperationState,
) controlplanev1.RestoreOperationNextAction {
	switch state {
	case controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_QUEUED,
		controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_ASSIGNED,
		controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_MATERIALIZING,
		controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_READY,
		controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_RUNNING,
		controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_RETRYING:
		return controlplanev1.RestoreOperationNextAction_RESTORE_OPERATION_NEXT_ACTION_WAIT
	case controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_FAILED,
		controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_CANCELLED,
		controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_EXPIRED:
		if operation.TargetExecutionID == "" || operation.TargetExecutionState == "" {
			return controlplanev1.RestoreOperationNextAction_RESTORE_OPERATION_NEXT_ACTION_NONE
		}
		return controlplanev1.RestoreOperationNextAction_RESTORE_OPERATION_NEXT_ACTION_RETRY_RUNTIME
	case controlplanev1.RestoreOperationState_RESTORE_OPERATION_STATE_SUCCEEDED:
		return controlplanev1.RestoreOperationNextAction_RESTORE_OPERATION_NEXT_ACTION_READ_BACKUP
	default:
		return controlplanev1.RestoreOperationNextAction_RESTORE_OPERATION_NEXT_ACTION_NONE
	}
}
