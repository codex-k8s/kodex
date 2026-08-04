package grpc

import (
	"context"
	"slices"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/integrationgatewayauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) ClaimRuntimeExecution(
	ctx context.Context,
	request *controlplanev1.ClaimRuntimeExecutionRequest,
) (*controlplanev1.ClaimRuntimeExecutionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_ClaimRuntimeExecution_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.ClaimRuntimeExecution(
		ctx, principal, request.GetIdempotencyKey(),
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.ClaimRuntimeExecutionResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func (server *Server) BindRuntimeAgentSession(
	ctx context.Context,
	request *controlplanev1.BindRuntimeAgentSessionRequest,
) (*controlplanev1.BindRuntimeAgentSessionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_BindRuntimeAgentSession_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	binding, err := server.service.BindRuntimeAgentSession(ctx, resource.RuntimeAgentSessionBindingInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		SessionID: request.GetSessionId(), ExpectedSessionVersion: request.GetExpectedSessionVersion(),
		TurnID: request.GetTurnId(), ExpectedTurnVersion: request.GetExpectedTurnVersion(),
		ExpectedAttempt: request.GetExpectedAttempt(), ExpectedInputSHA256: request.GetExpectedInputSha256(),
		RuntimeRevisionID: request.GetRuntimeRevisionId(), RuntimeRevisionVersion: request.GetRuntimeRevisionVersion(),
		RuntimeRevisionSHA256: request.GetRuntimeRevisionSha256(),
		AgentSessionKey:       request.GetAgentSessionKey(), AgentSessionID: request.GetAgentSessionId(),
		AgentSessionVersion: request.GetAgentSessionVersion(), AgentSessionTurnID: request.GetAgentSessionTurnId(),
		AgentRunID: request.GetAgentRunId(), AgentSessionTurnVersion: request.GetAgentSessionTurnVersion(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.BindRuntimeAgentSessionResponse{
		SessionId: binding.SessionID, SessionVersion: binding.SessionVersion,
		TurnId: binding.TurnID, TurnVersion: binding.TurnVersion,
		AgentSessionBindingSha256: binding.AgentSessionBindingSHA256,
		AgentTurnBindingSha256:    binding.AgentTurnBindingSHA256,
	}, nil
}

func (server *Server) GetRuntimeExecution(
	ctx context.Context,
	request *controlplanev1.GetRuntimeExecutionRequest,
) (*controlplanev1.GetRuntimeExecutionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_GetRuntimeExecution_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.GetRuntimeExecution(
		ctx, principal, request.GetExecutionId(), request.GetExpectedVersion(),
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.GetRuntimeExecutionResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func (server *Server) AdmitRuntimeExecution(
	ctx context.Context,
	request *controlplanev1.AdmitRuntimeExecutionRequest,
) (*controlplanev1.AdmitRuntimeExecutionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_AdmitRuntimeExecution_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.AdmitRuntimeExecution(ctx, resource.RuntimeExecutionInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(),
		ExpectedFence:           request.GetExpectedFence(),
		ExpectedGrantGeneration: request.GetExpectedGrantGeneration(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.AdmitRuntimeExecutionResponse{
		Execution: toProtoRuntimeExecution(result.Execution), LeaseToken: result.LeaseToken,
	}, nil
}

func (server *Server) HeartbeatRuntimeExecution(
	ctx context.Context,
	request *controlplanev1.HeartbeatRuntimeExecutionRequest,
) (*controlplanev1.HeartbeatRuntimeExecutionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_HeartbeatRuntimeExecution_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.HeartbeatRuntimeExecution(ctx, resource.RuntimeExecutionInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(),
		ExpectedFence:           request.GetExpectedFence(),
		ExpectedGrantGeneration: request.GetExpectedGrantGeneration(),
		LeaseToken:              request.GetLeaseToken(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.HeartbeatRuntimeExecutionResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func (server *Server) RecordRuntimeIncident(
	ctx context.Context,
	request *controlplanev1.RecordRuntimeIncidentRequest,
) (*controlplanev1.RecordRuntimeIncidentResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_RecordRuntimeIncident_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.RecordRuntimeIncident(ctx, resource.RecordRuntimeIncidentInput{
		RuntimeExecutionInput: resource.RuntimeExecutionInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(),
			ExpectedFence:           request.GetExpectedFence(),
			ExpectedGrantGeneration: principal.AuthorityGrantGeneration,
		},
		Kind: runtimeIncidentKind(request.GetKind()), IncidentID: request.GetIncidentId(),
		EvidenceSHA256: request.GetEvidenceSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.RecordRuntimeIncidentResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func (server *Server) CompleteRuntimeExecution(
	ctx context.Context,
	request *controlplanev1.CompleteRuntimeExecutionRequest,
) (*controlplanev1.CompleteRuntimeExecutionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_CompleteRuntimeExecution_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.CompleteRuntimeExecution(ctx, resource.CompleteRuntimeExecutionInput{
		RuntimeExecutionInput: resource.RuntimeExecutionInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(),
			ExpectedFence:           request.GetExpectedFence(),
			ExpectedGrantGeneration: request.GetExpectedGrantGeneration(),
			LeaseToken:              request.GetLeaseToken(),
		},
		Outcome:           runtimeTerminalOutcome(request.GetOutcome()),
		TerminalReference: request.GetTerminalReference(),
		TerminalSHA256:    request.GetTerminalSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.CompleteRuntimeExecutionResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func (server *Server) CancelRuntimeExecution(
	ctx context.Context,
	request *controlplanev1.CancelRuntimeExecutionRequest,
) (*controlplanev1.CancelRuntimeExecutionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_CancelRuntimeExecution_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.CancelRuntimeExecution(ctx, resource.CancelRuntimeExecutionInput{
		RuntimeExecutionInput: resource.RuntimeExecutionInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(),
			ExpectedFence: request.GetExpectedFence(),
		},
		ReasonCode: request.GetReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.CancelRuntimeExecutionResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func (server *Server) RetryRuntimeExecution(
	ctx context.Context,
	request *controlplanev1.RetryRuntimeExecutionRequest,
) (*controlplanev1.RetryRuntimeExecutionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_RetryRuntimeExecution_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.RetryRuntimeExecution(ctx, resource.RuntimeExecutionInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(),
		ExpectedFence: request.GetExpectedFence(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	turn, err := toProtoResource(result.Turn)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.RetryRuntimeExecutionResponse{
		PreviousExecution: toProtoRuntimeExecution(result.Previous), Turn: turn,
	}, nil
}

func (server *Server) RescheduleRuntimeExecution(
	ctx context.Context,
	request *controlplanev1.RescheduleRuntimeExecutionRequest,
) (*controlplanev1.RescheduleRuntimeExecutionResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_RescheduleRuntimeExecution_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.RescheduleRuntimeExecution(ctx, resource.RuntimeExecutionInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), ExecutionID: request.GetExecutionId(),
		ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	turn, err := toProtoResource(result.Turn)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.RescheduleRuntimeExecutionResponse{
		PreviousExecution: toProtoRuntimeExecution(result.Previous), Turn: turn,
	}, nil
}

func (server *Server) ExpireRuntimeExecution(
	ctx context.Context,
	request *controlplanev1.ExpireRuntimeExecutionRequest,
) (*controlplanev1.ExpireRuntimeExecutionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_ExpireRuntimeExecution_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.ExpireRuntimeExecution(
		ctx, principal, request.GetIdempotencyKey(),
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.ExpireRuntimeExecutionResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func (server *Server) RecordRuntimeArchive(
	ctx context.Context,
	request *controlplanev1.RecordRuntimeArchiveRequest,
) (*controlplanev1.RecordRuntimeArchiveResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_RecordRuntimeArchive_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	archiveRetainUntil := time.Time{}
	if request.GetArchiveRetainUntil() != nil {
		archiveRetainUntil = request.GetArchiveRetainUntil().AsTime()
	}
	execution, err := server.service.RecordRuntimeArchive(ctx, resource.RuntimeArchiveInput{
		RuntimeExecutionInput: resource.RuntimeExecutionInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(),
			ExpectedFence:           request.GetExpectedFence(),
			ExpectedGrantGeneration: principal.AuthorityGrantGeneration,
		},
		ArchiveReference: request.GetArchiveReference(), ArchiveSHA256: request.GetArchiveSha256(),
		ArchiveObjectKey: request.GetArchiveObjectKey(), ArchiveVersionID: request.GetArchiveVersionId(),
		ArchiveKMSKeyARN:        request.GetArchiveKmsKeyArn(),
		ArchiveObjectLockMode:   request.GetArchiveObjectLockMode(),
		ArchiveRetainUntil:      archiveRetainUntil,
		ArchiveProvenanceSHA256: request.GetArchiveProvenanceSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.RecordRuntimeArchiveResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func (server *Server) VerifyRuntimeRestore(
	ctx context.Context,
	request *controlplanev1.VerifyRuntimeRestoreRequest,
) (*controlplanev1.VerifyRuntimeRestoreResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_VerifyRuntimeRestore_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.VerifyRuntimeRestore(ctx, resource.RuntimeRestoreInput{
		RuntimeExecutionInput: resource.RuntimeExecutionInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(),
			ExpectedFence: request.GetExpectedFence(),
		},
		ArchiveSHA256:         request.GetArchiveSha256(),
		RestoreProofReference: request.GetRestoreProofReference(),
		RestoreProofSHA256:    request.GetRestoreProofSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.VerifyRuntimeRestoreResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func (server *Server) BindRuntimeRestoreTarget(
	ctx context.Context,
	request *controlplanev1.BindRuntimeRestoreTargetRequest,
) (*controlplanev1.BindRuntimeRestoreTargetResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_BindRuntimeRestoreTarget_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.BindRuntimeRestoreTarget(ctx, resource.RuntimeRestoreTargetInput{
		RuntimeExecutionInput: resource.RuntimeExecutionInput{Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence()},
		ExpectedAssignmentGeneration: request.GetExpectedAssignmentGeneration(), PVCName: request.GetPvcName(),
		PVCUID: request.GetPvcUid(), PVCResourceVersion: request.GetPvcResourceVersion(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.BindRuntimeRestoreTargetResponse{Execution: toProtoRuntimeExecution(execution)}, nil
}

func (server *Server) CompleteRuntimeRehydrate(
	ctx context.Context,
	request *controlplanev1.CompleteRuntimeRehydrateRequest,
) (*controlplanev1.CompleteRuntimeRehydrateResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_CompleteRuntimeRehydrate_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.CompleteRuntimeRehydrate(ctx, resource.RuntimeRehydrateInput{
		RuntimeExecutionInput: resource.RuntimeExecutionInput{Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence()},
		AssignmentGeneration: request.GetAssignmentGeneration(), PVCName: request.GetPvcName(), PVCUID: request.GetPvcUid(),
		PVCResourceVersion: request.GetPvcResourceVersion(), ProofReference: request.GetProofReference(),
		ProofSHA256: request.GetProofSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.CompleteRuntimeRehydrateResponse{Execution: toProtoRuntimeExecution(execution)}, nil
}

func (server *Server) AuthorizeRuntimeCleanup(
	ctx context.Context,
	request *controlplanev1.AuthorizeRuntimeCleanupRequest,
) (*controlplanev1.AuthorizeRuntimeCleanupResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_AuthorizeRuntimeCleanup_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.AuthorizeRuntimeCleanup(ctx, resource.RuntimeCleanupInput{
		RuntimeExecutionInput: resource.RuntimeExecutionInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(),
			ExpectedFence: request.GetExpectedFence(),
		},
		ArchiveSHA256:             request.GetArchiveSha256(),
		RestoreProofSHA256:        request.GetRestoreProofSha256(),
		ExpectedCleanupGeneration: request.GetExpectedCleanupGeneration(),
		PVCName:                   request.GetPvcName(),
		PVCUID:                    request.GetPvcUid(),
		PVCResourceVersion:        request.GetPvcResourceVersion(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.AuthorizeRuntimeCleanupResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func (server *Server) ConsumeRuntimeCleanupAuthorization(
	ctx context.Context,
	request *controlplanev1.ConsumeRuntimeCleanupAuthorizationRequest,
) (*controlplanev1.ConsumeRuntimeCleanupAuthorizationResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_ConsumeRuntimeCleanupAuthorization_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.ConsumeRuntimeCleanupAuthorization(
		ctx, resource.RuntimeCleanupAuthorizationInput{
			RuntimeExecutionInput: resource.RuntimeExecutionInput{
				Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
				ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(),
				ExpectedFence:           request.GetExpectedFence(),
				ExpectedGrantGeneration: principal.AuthorityGrantGeneration,
			},
			CleanupAuthorizationID:         request.GetCleanupAuthorizationId(),
			CleanupAuthorizationGeneration: request.GetCleanupAuthorizationGeneration(),
			ArchiveSHA256:                  request.GetArchiveSha256(),
			RestoreProofSHA256:             request.GetRestoreProofSha256(),
			PVCName:                        request.GetPvcName(),
			PVCUID:                         request.GetPvcUid(),
			PVCResourceVersion:             request.GetPvcResourceVersion(),
			ObservedNotFoundAt:             timestampOrZero(request.GetObservedNotFoundAt()),
			DeletionProofSHA256:            request.GetDeletionProofSha256(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.ConsumeRuntimeCleanupAuthorizationResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func (server *Server) ExpireRuntimeCleanupAuthorization(
	ctx context.Context,
	request *controlplanev1.ExpireRuntimeCleanupAuthorizationRequest,
) (*controlplanev1.ExpireRuntimeCleanupAuthorizationResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_ExpireRuntimeCleanupAuthorization_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	execution, err := server.service.ExpireRuntimeCleanupAuthorization(
		ctx, resource.RuntimeCleanupAuthorizationInput{
			RuntimeExecutionInput: resource.RuntimeExecutionInput{
				Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
				ExecutionID: request.GetExecutionId(), ExpectedVersion: request.GetExpectedVersion(),
				ExpectedFence: request.GetExpectedFence(),
			},
			CleanupAuthorizationID:         request.GetCleanupAuthorizationId(),
			CleanupAuthorizationGeneration: request.GetCleanupAuthorizationGeneration(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.ExpireRuntimeCleanupAuthorizationResponse{
		Execution: toProtoRuntimeExecution(execution),
	}, nil
}

func toProtoRuntimeExecution(execution resource.RuntimeExecution) *controlplanev1.RuntimeExecution {
	return &controlplanev1.RuntimeExecution{
		ExecutionId: execution.ID, OrganizationId: execution.OrganizationID,
		ProjectId: execution.ProjectID, ProcessId: execution.ProcessID,
		SessionId: execution.SessionID, ThreadId: execution.ThreadID,
		RoleId: execution.RoleID, TurnId: execution.TurnID, Attempt: execution.Attempt,
		RuntimeRevisionId:      execution.RuntimeRevisionID,
		RuntimeRevisionVersion: execution.RuntimeRevisionVersion,
		RuntimeRevisionSha256:  execution.RuntimeRevisionSHA256,
		ImmutableInputSha256:   execution.ImmutableInputSHA256,
		ResourceClass:          toProtoRuntimeResourceClass(execution.ResourceClass),
		ClusterAccessProfile:   toProtoClusterAccessProfile(execution.ClusterAccessProfile),
		WorkloadId:             execution.WorkloadID, WorkloadSpiffeId: execution.WorkloadSPIFFEID,
		GrantGeneration: execution.GrantGeneration, Version: execution.Version,
		Fence: execution.Fence, State: toProtoRuntimeExecutionState(execution.State),
		LeaseId: execution.LeaseID, LeaseExpiresAt: optionalTimestamp(execution.LeaseExpiresAt),
		TerminalOutcome:   toProtoRuntimeTerminalOutcome(execution.TerminalOutcome),
		TerminalReference: execution.TerminalReference,
		TerminalSha256:    execution.TerminalSHA256,
		ArchiveReference:  execution.ArchiveReference, ArchiveSha256: execution.ArchiveSHA256,
		ArchiveObjectKey: execution.ArchiveObjectKey, ArchiveVersionId: execution.ArchiveVersionID,
		ArchiveKmsKeyArn:                    execution.ArchiveKMSKeyARN,
		ArchiveObjectLockMode:               execution.ArchiveObjectLockMode,
		ArchiveProvenanceSha256:             execution.ArchiveProvenanceSHA256,
		RestoreProofReference:               execution.RestoreProofReference,
		RestoreProofSha256:                  execution.RestoreProofSHA256,
		RestoreVerifierWorkloadId:           execution.RestoreVerifierWorkload,
		RestoreVerifierSpiffeId:             execution.RestoreVerifierSPIFFEID,
		RestoreVerifierGeneration:           execution.RestoreVerifierGeneration,
		CleanupAuthorizationId:              execution.CleanupAuthorizationID,
		CleanupAuthorizationExpiresAt:       optionalTimestamp(execution.CleanupAuthorizationExpiresAt),
		CleanupAuthorizationState:           toProtoRuntimeCleanupAuthorizationState(execution.CleanupAuthorizationState),
		CleanupAuthorizationGeneration:      execution.CleanupAuthorizationGeneration,
		CleanupConsumedAt:                   optionalTimestamp(execution.CleanupConsumedAt),
		CleanupPvcName:                      execution.CleanupPVCName,
		CleanupPvcUid:                       execution.CleanupPVCUID,
		CleanupPvcResourceVersion:           execution.CleanupPVCResourceVersion,
		CleanupClaimedAt:                    optionalTimestamp(execution.CleanupClaimedAt),
		CleanupEligibleAt:                   optionalTimestamp(execution.CleanupEligibleAt),
		CleanupNotFoundAt:                   optionalTimestamp(execution.CleanupNotFoundAt),
		CleanupDeletionProofSha256:          execution.CleanupDeletionProofSHA256,
		RestoreSourceExecutionId:            execution.RestoreSourceExecutionID,
		RestoreSourceArchiveReference:       execution.RestoreSourceArchiveReference,
		RestoreSourceArchiveSha256:          execution.RestoreSourceArchiveSHA256,
		RestoreSourceRuntimeRevisionSha256:  execution.RestoreSourceRuntimeRevisionSHA256,
		RestoreSourceImmutableInputSha256:   execution.RestoreSourceImmutableInputSHA256,
		RestoreSourceProofSha256:            execution.RestoreSourceProofSHA256,
		RestoreSourceVersion:                execution.RestoreSourceVersion,
		RestoreSourceArchiveObjectKey:       execution.RestoreSourceArchiveObjectKey,
		RestoreSourceArchiveVersionId:       execution.RestoreSourceArchiveVersionID,
		RestoreSourceArchiveKmsKeyArn:       execution.RestoreSourceArchiveKMSKeyARN,
		RestoreSourceArchiveObjectLockMode:  execution.RestoreSourceArchiveObjectLockMode,
		RestoreSourceArchiveRetainUntil:     optionalTimestamp(execution.RestoreSourceArchiveRetainUntil),
		RestoreSourceRetentionPolicyId:      execution.RestoreSourceRetentionPolicyID,
		RestoreSourceRetentionPolicyVersion: execution.RestoreSourceRetentionPolicyVersion,
		RestoreSourceProvenanceSha256:       execution.RestoreSourceProvenanceSHA256,
		EffectiveRuntimeSha256:              execution.EffectiveRuntimeSHA256,
		AgentSessionKey:                     execution.AgentSessionKey,
		AgentSessionId:                      execution.AgentSessionID,
		AgentSessionTurnId:                  execution.AgentSessionTurnID,
		AgentRunId:                          execution.AgentRunID,
		AgentBindingSha256:                  execution.AgentBindingSHA256,
		RetentionPolicyId:                   execution.RetentionPolicyID,
		RetentionPolicyVersion:              execution.RetentionPolicyVersion,
		PvcRetention:                        durationpb.New(time.Duration(execution.PVCRetentionSeconds) * time.Second),
		ArchiveRetention:                    durationpb.New(time.Duration(execution.ArchiveRetentionSeconds) * time.Second),
		ArchiveRetainUntil:                  optionalTimestamp(execution.ArchiveRetainUntil),
		PvcCleanupEligibleAt:                timestamppb.New(execution.PVCCleanupEligibleAt),
		CapacityObservationExpiresAt:        timestamppb.New(execution.CapacityObservationExpiresAt),
		RescheduleAfter:                     timestamppb.New(execution.RescheduleAfter),
		RestoreAssignmentState:              execution.RestoreAssignmentState,
		RestoreAssignmentGeneration:         execution.RestoreAssignmentGeneration,
		RestoreTargetPvcName:                execution.RestoreTargetPVCName,
		RestoreTargetPvcUid:                 execution.RestoreTargetPVCUID,
		RestoreTargetPvcResourceVersion:     execution.RestoreTargetPVCResourceVersion,
		RehydrateProofReference:             execution.RehydrateProofReference,
		RehydrateProofSha256:                execution.RehydrateProofSHA256,
		CredentialSnapshotSha256:            execution.CredentialSnapshotSHA256,
		WorkloadTicketSha256:                execution.WorkloadTicketSHA256,
		WorkloadTicket:                      execution.WorkloadTicket,
		ArchiveWorkloadTicket:               execution.ArchiveWorkloadTicket,
		RestoreWorkloadTicket:               execution.RestoreWorkloadTicket,
		CreatedAt:                           timestamppb.New(execution.CreatedAt), UpdatedAt: timestamppb.New(execution.UpdatedAt),
	}
}

func runtimeIncidentKind(kind controlplanev1.RuntimeIncidentKind) string {
	return map[controlplanev1.RuntimeIncidentKind]string{
		controlplanev1.RuntimeIncidentKind_RUNTIME_INCIDENT_KIND_HEARTBEAT_MISSED:     "HEARTBEAT_MISSED",
		controlplanev1.RuntimeIncidentKind_RUNTIME_INCIDENT_KIND_RECONCILE_FAILED:     "RECONCILE_FAILED",
		controlplanev1.RuntimeIncidentKind_RUNTIME_INCIDENT_KIND_WORKLOAD_UNAVAILABLE: "WORKLOAD_UNAVAILABLE",
	}[kind]
}

func runtimeTerminalOutcome(outcome controlplanev1.RuntimeTerminalOutcome) string {
	return map[controlplanev1.RuntimeTerminalOutcome]string{
		controlplanev1.RuntimeTerminalOutcome_RUNTIME_TERMINAL_OUTCOME_SUCCEEDED: "SUCCEEDED",
		controlplanev1.RuntimeTerminalOutcome_RUNTIME_TERMINAL_OUTCOME_FAILED:    "FAILED",
		controlplanev1.RuntimeTerminalOutcome_RUNTIME_TERMINAL_OUTCOME_BLOCKED:   "BLOCKED",
	}[outcome]
}

func toProtoRuntimeResourceClass(value string) controlplanev1.RuntimeResourceClass {
	return map[string]controlplanev1.RuntimeResourceClass{
		"STANDARD":    controlplanev1.RuntimeResourceClass_RUNTIME_RESOURCE_CLASS_STANDARD,
		"HIGH_MEMORY": controlplanev1.RuntimeResourceClass_RUNTIME_RESOURCE_CLASS_HIGH_MEMORY,
		"ACCELERATED": controlplanev1.RuntimeResourceClass_RUNTIME_RESOURCE_CLASS_ACCELERATED,
	}[value]
}

func toProtoClusterAccessProfile(value string) controlplanev1.ClusterAccessProfile {
	return map[string]controlplanev1.ClusterAccessProfile{
		"NONE":              controlplanev1.ClusterAccessProfile_CLUSTER_ACCESS_PROFILE_NONE,
		"PROJECT_READ_ONLY": controlplanev1.ClusterAccessProfile_CLUSTER_ACCESS_PROFILE_PROJECT_READ_ONLY,
		"CLUSTER_ADMIN":     controlplanev1.ClusterAccessProfile_CLUSTER_ACCESS_PROFILE_CLUSTER_ADMIN,
	}[value]
}

func toProtoRuntimeExecutionState(value string) controlplanev1.RuntimeExecutionState {
	return map[string]controlplanev1.RuntimeExecutionState{
		"PENDING":   controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_PENDING,
		"ADMITTED":  controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_ADMITTED,
		"RUNNING":   controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_RUNNING,
		"SUCCEEDED": controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_SUCCEEDED,
		"FAILED":    controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_FAILED,
		"CANCELLED": controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_CANCELLED,
		"EXPIRED":   controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_EXPIRED,
		"RETRIED":   controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_RETRIED,
		"SUSPENDED": controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_SUSPENDED,
	}[value]
}

func toProtoRuntimeTerminalOutcome(value string) controlplanev1.RuntimeTerminalOutcome {
	return map[string]controlplanev1.RuntimeTerminalOutcome{
		"SUCCEEDED": controlplanev1.RuntimeTerminalOutcome_RUNTIME_TERMINAL_OUTCOME_SUCCEEDED,
		"FAILED":    controlplanev1.RuntimeTerminalOutcome_RUNTIME_TERMINAL_OUTCOME_FAILED,
		"SUSPENDED": controlplanev1.RuntimeTerminalOutcome_RUNTIME_TERMINAL_OUTCOME_SUSPENDED,
		"CANCELLED": controlplanev1.RuntimeTerminalOutcome_RUNTIME_TERMINAL_OUTCOME_CANCELLED,
		"EXPIRED":   controlplanev1.RuntimeTerminalOutcome_RUNTIME_TERMINAL_OUTCOME_EXPIRED,
		"BLOCKED":   controlplanev1.RuntimeTerminalOutcome_RUNTIME_TERMINAL_OUTCOME_BLOCKED,
	}[value]
}

func toProtoRuntimeCleanupAuthorizationState(
	value string,
) controlplanev1.RuntimeCleanupAuthorizationState {
	return map[string]controlplanev1.RuntimeCleanupAuthorizationState{
		"NONE":     controlplanev1.RuntimeCleanupAuthorizationState_RUNTIME_CLEANUP_AUTHORIZATION_STATE_NONE,
		"ACTIVE":   controlplanev1.RuntimeCleanupAuthorizationState_RUNTIME_CLEANUP_AUTHORIZATION_STATE_ACTIVE,
		"CONSUMED": controlplanev1.RuntimeCleanupAuthorizationState_RUNTIME_CLEANUP_AUTHORIZATION_STATE_CONSUMED,
		"EXPIRED":  controlplanev1.RuntimeCleanupAuthorizationState_RUNTIME_CLEANUP_AUTHORIZATION_STATE_EXPIRED,
	}[value]
}

func (server *Server) ResolveIntegrationSession(
	ctx context.Context,
	_ *controlplanev1.ResolveIntegrationSessionRequest,
) (*controlplanev1.ResolveIntegrationSessionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_ResolveIntegrationSession_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	resolvedContext, err := server.service.ResolveIntegrationSession(ctx, principal)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.ResolveIntegrationSessionResponse{
		Context: toProtoIntegrationSessionContext(resolvedContext),
	}, nil
}

func (server *Server) SuspendForIntegrationApproval(
	ctx context.Context,
	request *controlplanev1.SuspendForIntegrationApprovalRequest,
) (*controlplanev1.SuspendForIntegrationApprovalResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_SuspendForIntegrationApproval_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	expiresAt, err := requiredTime(request.GetApprovalExpiresAt())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	selected := request.GetSelectedBinding()
	if selected == nil || selected.GetIntegration() == nil ||
		selected.GetIntegration().GetResourceId() != request.GetIntegrationId() {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	credentialBindings := make(
		[]resource.PinnedIntegrationResource,
		0,
		len(selected.GetCredentialBindings()),
	)
	for _, binding := range selected.GetCredentialBindings() {
		credentialBindings = append(
			credentialBindings,
			fromProtoPinnedIntegrationResource(binding),
		)
	}
	continuation, err := server.service.SuspendForIntegrationApproval(
		ctx, resource.SuspendIntegrationInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			InvocationID: request.GetInvocationId(), ApprovalID: request.GetApprovalId(),
			IntegrationID:      request.GetIntegrationId(),
			IntegrationVersion: selected.GetIntegration().GetVersion(),
			IntegrationSHA256:  selected.GetIntegration().GetProjectionSha256(),
			CredentialBindings: credentialBindings,
			RequestSHA256:      request.GetRequestSha256(),
			ApprovalExpiresAt:  expiresAt,
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := server.toProtoIntegrationContinuation(ctx, principal, continuation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.SuspendForIntegrationApprovalResponse{
		Continuation: encoded,
	}, nil
}

func (server *Server) ApproveIntegrationInvocation(
	ctx context.Context,
	request *controlplanev1.ApproveIntegrationInvocationRequest,
) (*controlplanev1.ApproveIntegrationInvocationResponse, error) {
	continuation, principal, err := server.integrationDecision(
		ctx, request.GetDecision(), "approve",
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := server.toProtoIntegrationContinuation(ctx, principal, continuation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ApproveIntegrationInvocationResponse{
		Continuation: encoded,
	}, nil
}

func (server *Server) RejectIntegrationInvocation(
	ctx context.Context,
	request *controlplanev1.RejectIntegrationInvocationRequest,
) (*controlplanev1.RejectIntegrationInvocationResponse, error) {
	continuation, principal, err := server.integrationDecision(
		ctx, request.GetDecision(), "reject",
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := server.toProtoIntegrationContinuation(ctx, principal, continuation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.RejectIntegrationInvocationResponse{
		Continuation: encoded,
	}, nil
}

func (server *Server) CancelIntegrationInvocation(
	ctx context.Context,
	request *controlplanev1.CancelIntegrationInvocationRequest,
) (*controlplanev1.CancelIntegrationInvocationResponse, error) {
	continuation, principal, err := server.integrationDecision(
		ctx, request.GetDecision(), "cancel",
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := server.toProtoIntegrationContinuation(ctx, principal, continuation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.CancelIntegrationInvocationResponse{
		Continuation: encoded,
	}, nil
}

func (server *Server) integrationDecision(
	ctx context.Context,
	decision *controlplanev1.IntegrationDecisionReference,
	action string,
) (resource.IntegrationContinuation, value.Principal, error) {
	fullMethod := controlplanev1.ControlPlaneService_ApproveIntegrationInvocation_FullMethodName
	if action == "reject" {
		fullMethod = controlplanev1.ControlPlaneService_RejectIntegrationInvocation_FullMethodName
	} else if action == "cancel" {
		fullMethod = controlplanev1.ControlPlaneService_CancelIntegrationInvocation_FullMethodName
	}
	principal, err := authorization.Principal(ctx, fullMethod)
	if err != nil {
		return resource.IntegrationContinuation{}, value.Principal{}, errs.ErrUnauthenticated
	}
	if decision == nil {
		return resource.IntegrationContinuation{}, principal, errs.ErrInvalidInput
	}
	input := resource.IntegrationDecisionInput{
		Principal: principal, IdempotencyKey: decision.GetIdempotencyKey(),
		ContinuationID:  decision.GetContinuationId(),
		ExpectedVersion: decision.GetExpectedVersion(), ExpectedFence: decision.GetExpectedFence(),
		InvocationID: decision.GetInvocationId(), ApprovalID: decision.GetApprovalId(),
		RequestSHA256:     decision.GetRequestSha256(),
		DecisionReference: decision.GetDecisionReference(),
		DecisionSHA256:    decision.GetDecisionSha256(),
	}
	var continuation resource.IntegrationContinuation
	if action == "approve" {
		continuation, err = server.service.ApproveIntegrationInvocation(ctx, input)
	} else if action == "reject" {
		continuation, err = server.service.RejectIntegrationInvocation(ctx, input)
	} else {
		continuation, err = server.service.CancelIntegrationInvocation(ctx, input)
	}
	return continuation, principal, err
}

func (server *Server) ExpireIntegrationInvocation(
	ctx context.Context,
	request *controlplanev1.ExpireIntegrationInvocationRequest,
) (*controlplanev1.ExpireIntegrationInvocationResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_ExpireIntegrationInvocation_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	continuation, err := server.service.ExpireIntegrationInvocation(
		ctx, principal, request.GetIdempotencyKey(),
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := server.toProtoIntegrationContinuation(ctx, principal, continuation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ExpireIntegrationInvocationResponse{
		Continuation: encoded,
	}, nil
}

func (server *Server) BeginIntegrationExecution(
	ctx context.Context,
	request *controlplanev1.BeginIntegrationExecutionRequest,
) (*controlplanev1.BeginIntegrationExecutionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_BeginIntegrationExecution_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	continuation, err := server.service.BeginIntegrationExecution(
		ctx, resource.IntegrationExecutionInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ContinuationID:  request.GetContinuationId(),
			ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence(),
			InvocationID: request.GetInvocationId(), RequestSHA256: request.GetRequestSha256(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := server.toProtoIntegrationContinuation(ctx, principal, continuation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.BeginIntegrationExecutionResponse{
		Continuation: encoded,
	}, nil
}

func (server *Server) CompleteIntegrationExecution(
	ctx context.Context,
	request *controlplanev1.CompleteIntegrationExecutionRequest,
) (*controlplanev1.CompleteIntegrationExecutionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_CompleteIntegrationExecution_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	continuation, err := server.service.CompleteIntegrationExecution(
		ctx, resource.IntegrationExecutionInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ContinuationID:  request.GetContinuationId(),
			ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence(),
			InvocationID: request.GetInvocationId(), RequestSHA256: request.GetRequestSha256(),
			ResultReference: request.GetResultReference(), ResultSHA256: request.GetResultSha256(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := server.toProtoIntegrationContinuation(ctx, principal, continuation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.CompleteIntegrationExecutionResponse{
		Continuation: encoded,
	}, nil
}

func (server *Server) FailIntegrationExecution(
	ctx context.Context,
	request *controlplanev1.FailIntegrationExecutionRequest,
) (*controlplanev1.FailIntegrationExecutionResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_FailIntegrationExecution_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	continuation, err := server.service.FailIntegrationExecution(
		ctx, resource.IntegrationExecutionInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ContinuationID:  request.GetContinuationId(),
			ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence(),
			InvocationID: request.GetInvocationId(), RequestSHA256: request.GetRequestSha256(),
			ErrorCode: request.GetErrorCode(), ErrorReference: request.GetErrorReference(),
			ErrorSHA256: request.GetErrorSha256(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := server.toProtoIntegrationContinuation(ctx, principal, continuation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.FailIntegrationExecutionResponse{
		Continuation: encoded,
	}, nil
}

func (server *Server) GetIntegrationContinuation(
	ctx context.Context,
	_ *controlplanev1.GetIntegrationContinuationRequest,
) (*controlplanev1.GetIntegrationContinuationResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_GetIntegrationContinuation_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	continuation, err := server.service.GetIntegrationContinuation(
		ctx, resource.GetIntegrationContinuationInput{
			Principal: principal,
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := server.toProtoIntegrationContinuation(ctx, principal, continuation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.GetIntegrationContinuationResponse{
		Continuation: encoded,
	}, nil
}

func (server *Server) AcknowledgeIntegrationContinuation(
	ctx context.Context,
	request *controlplanev1.AcknowledgeIntegrationContinuationRequest,
) (*controlplanev1.AcknowledgeIntegrationContinuationResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_AcknowledgeIntegrationContinuation_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	continuation, err := server.service.AcknowledgeIntegrationContinuation(
		ctx, resource.AcknowledgeIntegrationContinuationInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ExpectedVersion: request.GetExpectedVersion(), ExpectedFence: request.GetExpectedFence(),
			ExpectedInputSHA256: request.GetExpectedInputSha256(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := server.toProtoIntegrationContinuation(ctx, principal, continuation)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.AcknowledgeIntegrationContinuationResponse{
		Continuation: encoded,
	}, nil
}

func (server *Server) ValidateIntegrationResultAccess(
	ctx context.Context,
	_ *controlplanev1.ValidateIntegrationResultAccessRequest,
) (*controlplanev1.ValidateIntegrationResultAccessResponse, error) {
	transportPrincipal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_ValidateIntegrationResultAccess_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, rpcError(transportPrincipal.CorrelationID, errs.ErrUnauthenticated)
	}
	values := incoming.Get(integrationgatewayauth.ResultAccessGrantMetadata)
	if len(values) != 1 || values[0] == "" || strings.TrimSpace(values[0]) != values[0] {
		return nil, rpcError(transportPrincipal.CorrelationID, errs.ErrUnauthenticated)
	}
	claims, err := server.resultVerifier.Verify(ctx, values[0])
	if err != nil || claims.Purpose != integrationgatewayauth.PurposeResultAccess ||
		!slices.Contains(claims.AllowedOperationIDs, "integration.result.resolve") ||
		!slices.Contains(claims.AllowedOperationIDs, "integration.result.acknowledge") {
		return nil, rpcError(transportPrincipal.CorrelationID, errs.ErrPermissionDenied)
	}
	boundPrincipal := value.Principal{
		ActorID: claims.Subject, OrganizationID: claims.OrganizationID, ProjectID: claims.ProjectID,
		Permission: "controlplane.integration_continuation.read", CorrelationID: transportPrincipal.CorrelationID,
		PolicyRevision: transportPrincipal.PolicyRevision, AuthorityGeneration: claims.SignerGeneration,
		CallerWorkload: claims.WorkloadID, CallerSPIFFEID: claims.CallerSPIFFEID,
		AuthoritySource: "INTEGRATION_CONTINUATION", AuthorityReference: claims.ContinuationID,
		AuthorityRevision: claims.ContinuationVersion, AuthorityDigest: claims.InputSHA256,
		AuthorityGrantGeneration: claims.GrantGeneration,
	}
	continuation, err := server.service.GetIntegrationContinuation(ctx, resource.GetIntegrationContinuationInput{Principal: boundPrincipal})
	if err != nil {
		return nil, rpcError(transportPrincipal.CorrelationID, err)
	}
	revisionSHA256, err := server.service.IntegrationContinuationRuntimeRevisionSHA256(ctx, boundPrincipal, continuation)
	if err != nil {
		return nil, rpcError(transportPrincipal.CorrelationID, err)
	}
	outcome, reference, referenceSHA256 := "", "", ""
	if continuation.ExecutionState == "SUCCEEDED" {
		outcome, reference, referenceSHA256 = "SUCCEEDED", continuation.ResultReference, continuation.ResultSHA256
	} else if continuation.ExecutionState == "FAILED" {
		outcome, reference, referenceSHA256 = "FAILED", continuation.ErrorReference, continuation.ErrorSHA256
		if continuation.ErrorCode == "PROVIDER_OUTCOME_UNKNOWN" {
			outcome = "UNKNOWN"
		}
	}
	if continuation.ID != claims.ContinuationID || continuation.Version != claims.ContinuationVersion ||
		continuation.Fence != claims.ContinuationFence || continuation.InvocationID != claims.InvocationID ||
		continuation.SessionID != claims.SessionID || continuation.ContinuationTurnID != claims.TurnID ||
		continuation.ContinuationAttempt != claims.Attempt ||
		continuation.ContinuationRuntimeRevisionID != claims.RuntimeRevisionID ||
		continuation.ContinuationRuntimeRevisionVersion != claims.RuntimeRevisionVersion ||
		revisionSHA256 != claims.RuntimeRevisionSHA256 ||
		continuation.ContinuationInputSHA256 != claims.InputSHA256 ||
		continuation.GrantGeneration != claims.GrantGeneration || outcome != claims.Outcome ||
		reference != claims.Reference || referenceSHA256 != claims.ReferenceSHA256 || reference == "" {
		return nil, rpcError(transportPrincipal.CorrelationID, errs.ErrStateConflict)
	}
	encoded := toProtoIntegrationContinuation(continuation)
	return &controlplanev1.ValidateIntegrationResultAccessResponse{
		Continuation: encoded, Outcome: outcome, Reference: reference,
		ReferenceSha256: referenceSHA256, ResultAttemptId: claims.ResultAttemptID,
		SignerGeneration: claims.SignerGeneration,
	}, nil
}

func toProtoIntegrationExecutionBinding(
	binding resource.IntegrationExecutionBinding,
) *controlplanev1.IntegrationExecutionBinding {
	credentialBindings := make(
		[]*controlplanev1.PinnedIntegrationResource,
		0,
		len(binding.CredentialBindings),
	)
	for _, credential := range binding.CredentialBindings {
		credentialBindings = append(
			credentialBindings,
			toProtoPinnedIntegrationResource(credential),
		)
	}
	return &controlplanev1.IntegrationExecutionBinding{
		OrganizationId: binding.OrganizationID, ProjectId: binding.ProjectID,
		ProcessId: binding.ProcessID, SessionId: binding.SessionID,
		SessionVersion: binding.SessionVersion, ThreadId: binding.ThreadID,
		RoleId: binding.RoleID, TurnId: binding.TurnID, TurnVersion: binding.TurnVersion,
		Attempt: binding.Attempt, RuntimeRevisionId: binding.RuntimeRevisionID,
		RuntimeRevisionVersion: binding.RuntimeRevisionVersion,
		RuntimeRevisionSha256:  binding.RuntimeRevisionSHA256,
		ImmutableInputSha256:   binding.ImmutableInputSHA256,
		GrantGeneration:        binding.GrantGeneration, Fence: binding.Fence,
		IntegrationBinding: &controlplanev1.IntegrationApprovalBinding{
			Integration:        toProtoPinnedIntegrationResource(binding.Integration),
			CredentialBindings: credentialBindings,
		},
	}
}

func fromProtoPinnedIntegrationResource(
	input *controlplanev1.PinnedIntegrationResource,
) resource.PinnedIntegrationResource {
	if input == nil {
		return resource.PinnedIntegrationResource{}
	}
	return resource.PinnedIntegrationResource{
		ResourceID: input.GetResourceId(), Version: input.GetVersion(),
		ProjectionSHA256: input.GetProjectionSha256(),
	}
}

func toProtoPinnedIntegrationResource(
	item resource.PinnedIntegrationResource,
) *controlplanev1.PinnedIntegrationResource {
	return &controlplanev1.PinnedIntegrationResource{
		ResourceId: item.ResourceID, Version: item.Version,
		ProjectionSha256: item.ProjectionSHA256,
	}
}

func toProtoIntegrationSessionContext(
	context resource.IntegrationSessionContext,
) *controlplanev1.IntegrationSessionContext {
	integrations := make([]*controlplanev1.IntegrationSessionBinding, 0, len(context.Integrations))
	for _, integration := range context.Integrations {
		credentials := make(
			[]*controlplanev1.IntegrationCredentialBinding,
			0,
			len(integration.CredentialBindings),
		)
		for _, credential := range integration.CredentialBindings {
			credentials = append(credentials, &controlplanev1.IntegrationCredentialBinding{
				CredentialBindingId:      credential.CredentialBindingID,
				CredentialBindingVersion: credential.CredentialBindingVersion,
				ProjectionSha256:         credential.ProjectionSHA256,
				Purpose:                  credential.Purpose, SecretRef: credential.SecretRef,
				PrincipalRef:       credential.PrincipalRef,
				CredentialRevision: credential.CredentialRevision,
				ExpiresAt:          optionalTimestamp(credential.ExpiresAt),
			})
		}
		integrations = append(integrations, &controlplanev1.IntegrationSessionBinding{
			IntegrationId:      integration.IntegrationID,
			IntegrationVersion: integration.IntegrationVersion,
			ProjectionSha256:   integration.ProjectionSHA256,
			DefinitionRef:      integration.DefinitionRef,
			DefinitionVersion:  integration.DefinitionVersion,
			Capabilities:       integration.Capabilities, EndpointRef: integration.EndpointRef,
			CredentialBindings: credentials,
		})
	}
	return &controlplanev1.IntegrationSessionContext{
		OrganizationId: context.OrganizationID, ProjectId: context.ProjectID,
		OwnerActorId: context.OwnerActorID, ProcessId: context.ProcessID,
		SessionId: context.SessionID, SessionVersion: context.SessionVersion,
		ThreadId: context.ThreadID, TurnId: context.TurnID,
		TurnVersion: context.TurnVersion, Attempt: context.Attempt,
		InputSha256:            context.InputSHA256,
		RuntimeRevisionId:      context.RuntimeRevisionID,
		RuntimeRevisionVersion: context.RuntimeRevisionVersion,
		RuntimeRevisionSha256:  context.RuntimeRevisionSHA256,
		RuntimeManifestSha256:  context.RuntimeManifestSHA256,
		RoleId:                 context.RoleID, RoleVersion: context.RoleVersion,
		RoleCapabilities: context.RoleCapabilities,
		GrantGeneration:  context.GrantGeneration, Integrations: integrations,
	}
}

func toProtoIntegrationContinuation(
	continuation resource.IntegrationContinuation,
) *controlplanev1.IntegrationContinuation {
	binding := resource.IntegrationExecutionBinding{
		OrganizationID: continuation.OrganizationID, ProjectID: continuation.ProjectID,
		ProcessID: continuation.ProcessID, SessionID: continuation.SessionID,
		SessionVersion: continuation.SessionVersion, ThreadID: continuation.ThreadID,
		RoleID: continuation.RoleID, TurnID: continuation.TurnID,
		TurnVersion: continuation.TurnVersion, Attempt: continuation.Attempt,
		RuntimeRevisionID:      continuation.RuntimeRevisionID,
		RuntimeRevisionVersion: continuation.RuntimeRevisionVersion,
		RuntimeRevisionSHA256:  continuation.RuntimeRevisionSHA256,
		ImmutableInputSHA256:   continuation.ImmutableInputSHA256,
		GrantGeneration:        continuation.GrantGeneration, Fence: continuation.Fence,
		Integration: resource.PinnedIntegrationResource{
			ResourceID:       continuation.IntegrationID,
			Version:          continuation.IntegrationVersion,
			ProjectionSHA256: continuation.IntegrationSHA256,
		},
		CredentialBindings: continuation.CredentialBindings,
	}
	return &controlplanev1.IntegrationContinuation{
		ContinuationId: continuation.ID,
		Binding:        toProtoIntegrationExecutionBinding(binding),
		InvocationId:   continuation.InvocationID, ApprovalId: continuation.ApprovalID,
		IntegrationId: continuation.IntegrationID, RequestSha256: continuation.RequestSHA256,
		ApprovalState:     toProtoIntegrationApprovalState(continuation.ApprovalState),
		ExecutionState:    toProtoIntegrationExecutionState(continuation.ExecutionState),
		ContinuationState: toProtoIntegrationContinuationState(continuation.ContinuationState),
		Version:           continuation.Version, Fence: continuation.Fence,
		ApprovalExpiresAt: timestamppb.New(continuation.ApprovalExpiresAt),
		DecisionReference: continuation.DecisionReference,
		DecisionSha256:    continuation.DecisionSHA256,
		ResultReference:   continuation.ResultReference, ResultSha256: continuation.ResultSHA256,
		ErrorCode: continuation.ErrorCode, ErrorReference: continuation.ErrorReference,
		ErrorSha256:                        continuation.ErrorSHA256,
		ContinuationTurnId:                 continuation.ContinuationTurnID,
		ContinuationTurnVersion:            continuation.ContinuationTurnVersion,
		ContinuationAttempt:                continuation.ContinuationAttempt,
		ContinuationRuntimeRevisionId:      continuation.ContinuationRuntimeRevisionID,
		ContinuationRuntimeRevisionVersion: continuation.ContinuationRuntimeRevisionVersion,
		ContinuationInputSha256:            continuation.ContinuationInputSHA256,
		CreatedAt:                          timestamppb.New(continuation.CreatedAt),
		UpdatedAt:                          timestamppb.New(continuation.UpdatedAt),
	}
}

func (server *Server) toProtoIntegrationContinuation(
	ctx context.Context,
	principal value.Principal,
	continuation resource.IntegrationContinuation,
) (*controlplanev1.IntegrationContinuation, error) {
	encoded := toProtoIntegrationContinuation(continuation)
	now := time.Now().UTC().Truncate(time.Second)
	base := integrationgatewayauth.Claims{
		Version:  1,
		Issuer:   "https://control-plane.mattercodex-system.svc.cluster.local/authority/integration-continuation",
		Audience: "urn:mattercodex:integration-continuation",
		Purpose:  integrationgatewayauth.PurposeTransition,
		Subject:  principal.ActorID, OrganizationID: continuation.OrganizationID,
		ProjectID: continuation.ProjectID, WorkloadID: "integration-gateway",
		CallerSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway",
		SessionID:      continuation.SessionID, TurnID: continuation.TurnID, Attempt: continuation.Attempt,
		InputSHA256:            continuation.ImmutableInputSHA256,
		RuntimeRevisionID:      continuation.RuntimeRevisionID,
		RuntimeRevisionVersion: continuation.RuntimeRevisionVersion,
		RuntimeRevisionSHA256:  continuation.RuntimeRevisionSHA256,
		GrantGeneration:        continuation.GrantGeneration,
		ContinuationID:         continuation.ID, ContinuationVersion: continuation.Version,
		ContinuationFence: continuation.Fence, InvocationID: continuation.InvocationID,
		JTI: uuid.NewString(), IssuedAt: now.Unix(), NotBefore: now.Unix(),
		ExpiresAt: now.Add(continuationGrantTTL).Unix(),
	}
	switch {
	case continuation.ApprovalState == "PENDING" && continuation.ExecutionState == "NOT_STARTED" && continuation.ContinuationState == "SUSPENDED":
		base.AllowedOperationIDs = []string{
			"control.integration-invocation.approve", "control.integration-invocation.reject",
			"control.integration-invocation.cancel", "control.integration-invocation.expire",
		}
		if deadline := continuation.ApprovalExpiresAt.Add(15 * time.Minute); deadline.Before(time.Unix(base.ExpiresAt, 0)) {
			base.ExpiresAt = deadline.Unix()
		}
	case continuation.ApprovalState == "APPROVED" && continuation.ExecutionState == "NOT_STARTED" && continuation.ContinuationState == "SUSPENDED":
		base.AllowedOperationIDs = []string{"control.integration-execution.begin", "control.integration-invocation.cancel"}
	case continuation.ApprovalState == "APPROVED" && continuation.ExecutionState == "EXECUTING" && continuation.ContinuationState == "SUSPENDED":
		base.AllowedOperationIDs = []string{"control.integration-execution.complete", "control.integration-execution.fail"}
	}
	if len(base.AllowedOperationIDs) > 0 {
		compact, err := server.transitionSigner.Sign(ctx, base)
		if err != nil {
			return nil, err
		}
		encoded.TransitionGrant = compact
		encoded.TransitionGrantExpiresAt = timestamppb.New(time.Unix(base.ExpiresAt, 0).UTC())
	}
	resultReference, resultDigest, outcome := continuation.ResultReference, continuation.ResultSHA256, "SUCCEEDED"
	if continuation.ExecutionState == "FAILED" {
		resultReference, resultDigest, outcome = continuation.ErrorReference, continuation.ErrorSHA256, "FAILED"
		if continuation.ErrorCode == "PROVIDER_OUTCOME_UNKNOWN" {
			outcome = "UNKNOWN"
		}
	}
	if continuation.ContinuationState == "READY" && resultReference != "" && resultDigest != "" &&
		(outcome == "SUCCEEDED" || outcome == "FAILED" || outcome == "UNKNOWN") {
		const resultPrefix = "integration-gateway://invocations/"
		reference, found := strings.CutPrefix(resultReference, resultPrefix)
		parts := strings.Split(reference, "/results/")
		if !found || len(parts) != 2 || parts[0] != continuation.InvocationID ||
			value.ValidateID(parts[1]) != nil {
			return nil, errs.ErrInternal
		}
		revisionSHA256, err := server.service.IntegrationContinuationRuntimeRevisionSHA256(
			ctx, principal, continuation,
		)
		if err != nil {
			return nil, err
		}
		base.Purpose = integrationgatewayauth.PurposeResultAccess
		base.Audience = "urn:mattercodex:integration-result-access"
		base.WorkloadID = "agent-runner"
		base.CallerSPIFFEID = "spiffe://mattercodex.local/ns/mattercodex-system/sa/agent-runner"
		base.TurnID = continuation.ContinuationTurnID
		base.Attempt = continuation.ContinuationAttempt
		base.InputSHA256 = continuation.ContinuationInputSHA256
		base.RuntimeRevisionID = continuation.ContinuationRuntimeRevisionID
		base.RuntimeRevisionVersion = continuation.ContinuationRuntimeRevisionVersion
		base.RuntimeRevisionSHA256 = revisionSHA256
		base.ResultAttemptID = parts[1]
		base.Outcome = outcome
		base.Reference = resultReference
		base.ReferenceSHA256 = resultDigest
		base.AllowedOperationIDs = []string{"integration.result.resolve", "integration.result.acknowledge"}
		base.JTI = uuid.NewString()
		base.ExpiresAt = now.Add(continuationGrantTTL).Unix()
		compact, err := server.resultSigner.Sign(ctx, base)
		if err != nil {
			return nil, err
		}
		encoded.ResultAccessGrant = compact
		encoded.ResultAccessGrantExpiresAt = timestamppb.New(time.Unix(base.ExpiresAt, 0).UTC())
	}
	return encoded, nil
}

func toProtoIntegrationApprovalState(value string) controlplanev1.IntegrationApprovalState {
	return map[string]controlplanev1.IntegrationApprovalState{
		"PENDING":   controlplanev1.IntegrationApprovalState_INTEGRATION_APPROVAL_STATE_PENDING,
		"APPROVED":  controlplanev1.IntegrationApprovalState_INTEGRATION_APPROVAL_STATE_APPROVED,
		"REJECTED":  controlplanev1.IntegrationApprovalState_INTEGRATION_APPROVAL_STATE_REJECTED,
		"EXPIRED":   controlplanev1.IntegrationApprovalState_INTEGRATION_APPROVAL_STATE_EXPIRED,
		"CANCELLED": controlplanev1.IntegrationApprovalState_INTEGRATION_APPROVAL_STATE_CANCELLED,
	}[value]
}

func toProtoIntegrationExecutionState(value string) controlplanev1.IntegrationExecutionState {
	return map[string]controlplanev1.IntegrationExecutionState{
		"NOT_STARTED":    controlplanev1.IntegrationExecutionState_INTEGRATION_EXECUTION_STATE_NOT_STARTED,
		"EXECUTING":      controlplanev1.IntegrationExecutionState_INTEGRATION_EXECUTION_STATE_EXECUTING,
		"SUCCEEDED":      controlplanev1.IntegrationExecutionState_INTEGRATION_EXECUTION_STATE_SUCCEEDED,
		"FAILED":         controlplanev1.IntegrationExecutionState_INTEGRATION_EXECUTION_STATE_FAILED,
		"NOT_APPLICABLE": controlplanev1.IntegrationExecutionState_INTEGRATION_EXECUTION_STATE_NOT_APPLICABLE,
	}[value]
}

func toProtoIntegrationContinuationState(value string) controlplanev1.IntegrationContinuationState {
	return map[string]controlplanev1.IntegrationContinuationState{
		"SUSPENDED": controlplanev1.IntegrationContinuationState_INTEGRATION_CONTINUATION_STATE_SUSPENDED,
		"READY":     controlplanev1.IntegrationContinuationState_INTEGRATION_CONTINUATION_STATE_READY,
		"REJOINED":  controlplanev1.IntegrationContinuationState_INTEGRATION_CONTINUATION_STATE_REJOINED,
	}[value]
}
