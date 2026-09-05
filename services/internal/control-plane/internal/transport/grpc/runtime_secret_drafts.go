package grpc

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func castSecretDraft(d entity.RuntimeSecretDraft) *cp.RuntimeSecretDraft {
	return &cp.RuntimeSecretDraft{Ref: d.Ref, Version: d.Version, Generation: d.Generation, ProjectRef: d.ProjectRef, SecretRef: d.SecretRef, Name: d.Name, Description: d.Description, ValueType: runtimeSecretValueType(d.ValueType), State: cp.RuntimeSecretDraftState(cp.RuntimeSecretDraftState_value["RUNTIME_SECRET_DRAFT_STATE_"+d.State]), PublishedRevision: d.PublishedRevision, CreatedAt: timestamp(d.CreatedAt), UpdatedAt: timestamp(d.UpdatedAt), ExpiresAt: timestamp(d.ExpiresAt)}
}
func castDraftEncrypted(d *entity.RuntimeSecretDraftEncryptedDescriptor) *cp.RuntimeSecretDraftEncryptedDescriptor {
	if d == nil {
		return nil
	}
	return &cp.RuntimeSecretDraftEncryptedDescriptor{Namespace: d.Namespace, SecretName: d.SecretName, SecretKey: d.SecretKey, SecretUid: d.SecretUID, SecretResourceVersion: d.SecretResourceVersion, CiphertextSha256: d.CiphertextSHA256, EncryptionKeyId: d.EncryptionKeyID, EncryptionKeyGeneration: d.EncryptionKeyGeneration}
}
func draftEncrypted(d *cp.RuntimeSecretDraftEncryptedDescriptor) *entity.RuntimeSecretDraftEncryptedDescriptor {
	if d == nil {
		return nil
	}
	return &entity.RuntimeSecretDraftEncryptedDescriptor{Namespace: d.GetNamespace(), SecretName: d.GetSecretName(), SecretKey: d.GetSecretKey(), SecretUID: d.GetSecretUid(), SecretResourceVersion: d.GetSecretResourceVersion(), CiphertextSHA256: d.GetCiphertextSha256(), EncryptionKeyID: d.GetEncryptionKeyId(), EncryptionKeyGeneration: d.GetEncryptionKeyGeneration()}
}
func castDraftMaterialization(d *entity.RuntimeSecretMaterialization) *cp.RuntimeSecretMaterialization {
	if d == nil {
		return nil
	}
	return &cp.RuntimeSecretMaterialization{Namespace: d.Namespace, SecretName: d.SecretName, SecretKey: d.SecretKey, SecretUid: d.SecretUID, SecretResourceVersion: d.SecretResourceVersion, ContentSha256: d.ContentSHA256}
}
func castDraftWork(w entity.RuntimeSecretDraftWork) *cp.RuntimeSecretDraftWork {
	return &cp.RuntimeSecretDraftWork{OperationRef: w.OperationRef, Kind: cp.RuntimeSecretDraftOperationKind(cp.RuntimeSecretDraftOperationKind_value["RUNTIME_SECRET_DRAFT_OPERATION_KIND_"+w.Kind]), Draft: castSecretDraft(w.Draft), ExpectedContentSha256: w.ExpectedContentSHA256, Namespace: w.Namespace, StagedNamespace: w.StagedNamespace, StagedSecretName: w.StagedSecretName, StagedSecretKey: w.StagedSecretKey, TargetRevision: w.TargetRevision, Encrypted: castDraftEncrypted(w.Encrypted), ClaimantId: w.ClaimantID, ClaimGeneration: w.ClaimGeneration, LeaseDeadline: draftOptionalTime(w.LeaseDeadline), ExpiresAt: timestamp(w.ExpiresAt), RecoveryEncrypted: castDraftEncrypted(w.RecoveryEncrypted), RecoveryMaterialization: castDraftMaterialization(w.RecoveryMaterialization)}
}
func (server *Server) GetRuntimeSecretDraft(ctx context.Context, request *cp.GetRuntimeSecretDraftRequest) (*cp.GetRuntimeSecretDraftResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_GetRuntimeSecretDraft_FullMethodName)
	if err != nil {
		return nil, err
	}
	d, err := server.service.GetRuntimeSecretDraft(ctx, p, request.GetDraftRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.GetRuntimeSecretDraftResponse{Draft: castSecretDraft(d)}, nil
}
func (server *Server) prepareSecretDraft(ctx context.Context, method string, input port.RuntimeSecretDraftPrepareInput) (*cp.RuntimeSecretDraftOperationReceipt, error) {
	p, err := principal(ctx, method)
	if err != nil {
		return nil, err
	}
	result, err := server.service.PrepareRuntimeSecretDraft(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.RuntimeSecretDraftOperationReceipt{OperationGrant: result.OperationGrant, OperationRef: result.OperationRef, State: runtimeSecretOperationState(result.State), ExpiresAt: draftOptionalTime(result.ExpiresAt), Draft: castSecretDraft(result.Draft), FailureCode: runtimeSecretFailureCode(result.FailureCode)}
	if result.TerminalSecret != nil {
		response.TerminalSecret = castRuntimeSecret(*result.TerminalSecret)
	}
	return response, nil
}
func (server *Server) PrepareSaveRuntimeSecretDraft(ctx context.Context, request *cp.PrepareSaveRuntimeSecretDraftRequest) (*cp.PrepareSaveRuntimeSecretDraftResponse, error) {
	operation, err := server.prepareSecretDraft(ctx, cp.PlatformCommandService_PrepareSaveRuntimeSecretDraft_FullMethodName, port.RuntimeSecretDraftPrepareInput{Kind: "SAVE", Mutation: mutation(request.GetMutation()), SecretRef: request.GetSecretRef(), ProjectRef: request.GetProjectRef(), Name: request.GetName(), Description: request.GetDescription(), ValueType: runtimeSecretValueTypeName(request.GetValueType()), ExpectedContentSHA256: request.GetExpectedContentSha256()})
	if err != nil {
		return nil, err
	}
	return &cp.PrepareSaveRuntimeSecretDraftResponse{Operation: operation}, nil
}
func (server *Server) PrepareValidateRuntimeSecretDraft(ctx context.Context, request *cp.PrepareValidateRuntimeSecretDraftRequest) (*cp.PrepareValidateRuntimeSecretDraftResponse, error) {
	operation, err := server.prepareSecretDraft(ctx, cp.PlatformCommandService_PrepareValidateRuntimeSecretDraft_FullMethodName, port.RuntimeSecretDraftPrepareInput{Kind: "VALIDATE", Mutation: mutation(request.GetMutation()), DraftRef: request.GetDraftRef()})
	if err != nil {
		return nil, err
	}
	return &cp.PrepareValidateRuntimeSecretDraftResponse{Operation: operation}, nil
}
func (server *Server) PreparePublishRuntimeSecretDraft(ctx context.Context, request *cp.PreparePublishRuntimeSecretDraftRequest) (*cp.PreparePublishRuntimeSecretDraftResponse, error) {
	operation, err := server.prepareSecretDraft(ctx, cp.PlatformCommandService_PreparePublishRuntimeSecretDraft_FullMethodName, port.RuntimeSecretDraftPrepareInput{Kind: "PUBLISH", Mutation: mutation(request.GetMutation()), DraftRef: request.GetDraftRef(), ExpectedSecretVersion: request.GetExpectedSecretVersion()})
	if err != nil {
		return nil, err
	}
	return &cp.PreparePublishRuntimeSecretDraftResponse{Operation: operation}, nil
}
func (server *Server) PrepareDiscardRuntimeSecretDraft(ctx context.Context, request *cp.PrepareDiscardRuntimeSecretDraftRequest) (*cp.PrepareDiscardRuntimeSecretDraftResponse, error) {
	operation, err := server.prepareSecretDraft(ctx, cp.PlatformCommandService_PrepareDiscardRuntimeSecretDraft_FullMethodName, port.RuntimeSecretDraftPrepareInput{Kind: "DISCARD", Mutation: mutation(request.GetMutation()), DraftRef: request.GetDraftRef()})
	if err != nil {
		return nil, err
	}
	return &cp.PrepareDiscardRuntimeSecretDraftResponse{Operation: operation}, nil
}
func (server *Server) CheckRuntimeSecretDraftWorkReadiness(ctx context.Context, _ *cp.CheckRuntimeSecretDraftWorkReadinessRequest) (*cp.CheckRuntimeSecretDraftWorkReadinessResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_CheckRuntimeSecretDraftWorkReadiness_FullMethodName)
	if err != nil {
		return nil, err
	}
	if err = server.service.CheckRuntimeSecretDraftWork(ctx, p); err != nil {
		return nil, transportError(err)
	}
	return &cp.CheckRuntimeSecretDraftWorkReadinessResponse{Ready: true}, nil
}
func (server *Server) ConsumeRuntimeSecretDraftOperation(ctx context.Context, request *cp.ConsumeRuntimeSecretDraftOperationRequest) (*cp.ConsumeRuntimeSecretDraftOperationResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_ConsumeRuntimeSecretDraftOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	work, err := server.service.ConsumeRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{OperationGrant: request.GetOperationGrant(), ClaimantID: request.GetClaimantId()})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.ConsumeRuntimeSecretDraftOperationResponse{Work: castDraftWork(work)}, nil
}
func (server *Server) ListRuntimeSecretDraftRecoveryWork(ctx context.Context, request *cp.ListRuntimeSecretDraftRecoveryWorkRequest) (*cp.ListRuntimeSecretDraftRecoveryWorkResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_ListRuntimeSecretDraftRecoveryWork_FullMethodName)
	if err != nil {
		return nil, err
	}
	works, next, err := server.service.ListRuntimeSecretDraftRecovery(ctx, p, page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.ListRuntimeSecretDraftRecoveryWorkResponse{Page: &cp.PageInfo{NextPageToken: next}}
	for _, work := range works {
		response.Operations = append(response.Operations, castDraftWork(work))
	}
	return response, nil
}
func (server *Server) CompleteRuntimeSecretDraftOperation(ctx context.Context, request *cp.CompleteRuntimeSecretDraftOperationRequest) (*cp.CompleteRuntimeSecretDraftOperationResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_CompleteRuntimeSecretDraftOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FinishRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{Action: "COMPLETE", OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(), Encrypted: draftEncrypted(request.GetEncrypted()), Materialization: runtimeSecretMaterialization(request.GetMaterialization())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.CompleteRuntimeSecretDraftOperationResponse{Draft: castSecretDraft(result.Draft)}
	if result.Secret != nil {
		response.Secret = castRuntimeSecret(*result.Secret)
	}
	return response, nil
}
func (server *Server) FailRuntimeSecretDraftOperation(ctx context.Context, request *cp.FailRuntimeSecretDraftOperationRequest) (*cp.FailRuntimeSecretDraftOperationResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_FailRuntimeSecretDraftOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FinishRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{Action: "FAIL", OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(), FailureCode: enumSuffix(request.GetFailureCode(), "RUNTIME_SECRET_FAILURE_CODE_")})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.FailRuntimeSecretDraftOperationResponse{Draft: castSecretDraft(result.Draft), State: runtimeSecretOperationState(result.State)}, nil
}
func (server *Server) RecoverRuntimeSecretDraftMaterialization(ctx context.Context, request *cp.RecoverRuntimeSecretDraftMaterializationRequest) (*cp.RecoverRuntimeSecretDraftMaterializationResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_RecoverRuntimeSecretDraftMaterialization_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FinishRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{Action: "RECOVER", OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(), Encrypted: draftEncrypted(request.GetEncrypted()), Materialization: runtimeSecretMaterialization(request.GetMaterialization())})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.RecoverRuntimeSecretDraftMaterializationResponse{Draft: castSecretDraft(result.Draft), OperationState: runtimeSecretOperationState(result.State), EncryptedAction: cp.RuntimeSecretRecoveryAction(cp.RuntimeSecretRecoveryAction_value["RUNTIME_SECRET_RECOVERY_ACTION_"+result.EncryptedAction]), MaterializationAction: cp.RuntimeSecretRecoveryAction(cp.RuntimeSecretRecoveryAction_value["RUNTIME_SECRET_RECOVERY_ACTION_"+result.MaterializationAction])}, nil
}
func (server *Server) CompleteRuntimeSecretDraftCleanup(ctx context.Context, request *cp.CompleteRuntimeSecretDraftCleanupRequest) (*cp.CompleteRuntimeSecretDraftCleanupResponse, error) {
	p, err := principal(ctx, cp.RuntimeSecretDraftWorkService_CompleteRuntimeSecretDraftCleanup_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FinishRuntimeSecretDraft(ctx, p, port.RuntimeSecretDraftWorkInput{Action: "CLEANUP", OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(), Encrypted: draftEncrypted(request.GetEncrypted()), Materialization: runtimeSecretMaterialization(request.GetMaterialization())})
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.CompleteRuntimeSecretDraftCleanupResponse{Completed: result.Completed}, nil
}

func draftOptionalTime(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamp(value)
}
