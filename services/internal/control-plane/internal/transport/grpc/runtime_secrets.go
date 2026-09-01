package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) CheckRuntimeSecretWorkReadiness(ctx context.Context, _ *controlplanev1.CheckRuntimeSecretWorkReadinessRequest) (*controlplanev1.CheckRuntimeSecretWorkReadinessResponse, error) {
	if _, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_CheckRuntimeSecretWorkReadiness_FullMethodName); err != nil {
		return nil, err
	}
	return &controlplanev1.CheckRuntimeSecretWorkReadinessResponse{Ready: true}, nil
}

func (server *Server) ListRuntimeSecretRecoveryWork(ctx context.Context, request *controlplanev1.ListRuntimeSecretRecoveryWorkRequest) (*controlplanev1.ListRuntimeSecretRecoveryWorkResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_ListRuntimeSecretRecoveryWork_FullMethodName)
	if err != nil {
		return nil, err
	}
	requestedPage := request.GetPage()
	page := platformrepo.RuntimeSecretRecoveryPage{}
	if requestedPage != nil {
		page.Size = requestedPage.GetPageSize()
		page.Token = requestedPage.GetPageToken()
	}
	items, next, err := server.service.ListRuntimeSecretRecoveryWork(ctx, p, page)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRuntimeSecretRecoveryWorkResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Operations = append(response.Operations, &controlplanev1.RuntimeSecretRecoveryWork{
			OperationRef: item.OperationRef, Kind: runtimeSecretOperationKind(item.Kind),
			ClaimantId: item.ClaimantID, ClaimGeneration: item.ClaimGeneration,
			Namespace: item.Namespace, SecretRef: item.SecretRef, TargetRevision: item.TargetRevision,
			SecretKey: item.SecretKey, ExpectedContentSha256: item.ExpectedContentSHA256,
		})
	}
	return response, nil
}

func (server *Server) ListRuntimeSecrets(ctx context.Context, request *controlplanev1.ListRuntimeSecretsRequest) (*controlplanev1.ListRuntimeSecretsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListRuntimeSecrets_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListRuntimeSecrets(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRuntimeSecretsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Secrets = append(response.Secrets, castRuntimeSecret(item))
	}
	return response, nil
}

func (server *Server) GetRuntimeSecret(ctx context.Context, request *controlplanev1.GetRuntimeSecretRequest) (*controlplanev1.GetRuntimeSecretResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRuntimeSecret_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetRuntimeSecret(ctx, p, request.GetSecretRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetRuntimeSecretResponse{Secret: castRuntimeSecret(item)}, nil
}

func (server *Server) PrepareCreateRuntimeSecret(ctx context.Context, request *controlplanev1.PrepareCreateRuntimeSecretRequest) (*controlplanev1.PrepareCreateRuntimeSecretResponse, error) {
	operation, err := server.prepareRuntimeSecret(ctx, controlplanev1.PlatformCommandService_PrepareCreateRuntimeSecret_FullMethodName, platformrepo.RuntimeSecretPrepareInput{
		Kind: "CREATE", ProjectRef: request.GetProjectRef(), Name: request.GetName(), Description: request.GetDescription(),
		ValueType: runtimeSecretValueTypeName(request.GetValueType()), ExpectedContentSHA256: request.GetExpectedContentSha256(), Mutation: mutation(request.GetMutation()),
	})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PrepareCreateRuntimeSecretResponse{Operation: operation}, err
}
func (server *Server) PrepareRotateRuntimeSecret(ctx context.Context, request *controlplanev1.PrepareRotateRuntimeSecretRequest) (*controlplanev1.PrepareRotateRuntimeSecretResponse, error) {
	operation, err := server.prepareRuntimeSecret(ctx, controlplanev1.PlatformCommandService_PrepareRotateRuntimeSecret_FullMethodName, platformrepo.RuntimeSecretPrepareInput{
		Kind: "ROTATE", SecretRef: request.GetSecretRef(), ValueType: runtimeSecretValueTypeName(request.GetValueType()),
		ExpectedContentSHA256: request.GetExpectedContentSha256(), Mutation: mutation(request.GetMutation()),
	})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PrepareRotateRuntimeSecretResponse{Operation: operation}, err
}
func (server *Server) PrepareRevealRuntimeSecret(ctx context.Context, request *controlplanev1.PrepareRevealRuntimeSecretRequest) (*controlplanev1.PrepareRevealRuntimeSecretResponse, error) {
	operation, err := server.prepareRuntimeSecret(ctx, controlplanev1.PlatformCommandService_PrepareRevealRuntimeSecret_FullMethodName, platformrepo.RuntimeSecretPrepareInput{
		Kind: "REVEAL", SecretRef: request.GetSecretRef(), Mutation: mutation(request.GetMutation()),
	})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PrepareRevealRuntimeSecretResponse{Operation: operation}, err
}
func (server *Server) PrepareRevokeRuntimeSecret(ctx context.Context, request *controlplanev1.PrepareRevokeRuntimeSecretRequest) (*controlplanev1.PrepareRevokeRuntimeSecretResponse, error) {
	operation, err := server.prepareRuntimeSecret(ctx, controlplanev1.PlatformCommandService_PrepareRevokeRuntimeSecret_FullMethodName, platformrepo.RuntimeSecretPrepareInput{
		Kind: "REVOKE", SecretRef: request.GetSecretRef(), Mutation: mutation(request.GetMutation()),
	})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PrepareRevokeRuntimeSecretResponse{Operation: operation}, err
}

func (server *Server) prepareRuntimeSecret(ctx context.Context, method string, input platformrepo.RuntimeSecretPrepareInput) (*controlplanev1.RuntimeSecretOperationReceipt, error) {
	p, err := principal(ctx, method)
	if err != nil {
		return nil, err
	}
	result, err := server.service.PrepareRuntimeSecretOperation(ctx, p, input)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.RuntimeSecretOperationReceipt{
		OperationRef: result.OperationRef, OperationGrant: result.OperationGrant, ExpiresAt: timestamp(result.ExpiresAt),
		ValueType: runtimeSecretValueType(result.ValueType), State: runtimeSecretOperationState(result.State),
		FailureCode: runtimeSecretFailureCode(result.FailureCode),
	}
	if result.TerminalSecret != nil {
		response.TerminalSecret = castRuntimeSecret(*result.TerminalSecret)
	}
	return response, nil
}

func (server *Server) ConsumeRuntimeSecretOperation(ctx context.Context, request *controlplanev1.ConsumeRuntimeSecretOperationRequest) (*controlplanev1.ConsumeRuntimeSecretOperationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_ConsumeRuntimeSecretOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.ConsumeRuntimeSecretOperation(ctx, p, platformrepo.RuntimeSecretConsumeInput{
		OperationGrant: request.GetOperationGrant(), ClaimantID: request.GetClaimantId(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ConsumeRuntimeSecretOperationResponse{
		OperationRef: item.Ref, Kind: runtimeSecretOperationKind(item.Kind), ProjectRef: item.ProjectRef,
		SecretRef: item.SecretRef, Name: item.Name, Description: item.Description,
		ValueType: runtimeSecretValueType(item.ValueType), Namespace: item.Namespace,
		TargetRevision: item.TargetRevision, VersionedSecretNames: append([]string(nil), item.VersionedSecretNames...),
		SecretKey: item.SecretKey, ExpiresAt: timestamp(item.ExpiresAt), ClaimGeneration: item.ClaimGeneration,
		LeaseDeadline: timestamp(item.LeaseDeadline), ExpectedContentSha256: item.ExpectedContentSHA256,
	}
	for _, descriptor := range item.RevisionDescriptors {
		response.RevisionDescriptors = append(response.RevisionDescriptors, castRuntimeSecretRevisionDescriptor(descriptor))
	}
	return response, nil
}

func (server *Server) CompleteRuntimeSecretOperation(ctx context.Context, request *controlplanev1.CompleteRuntimeSecretOperationRequest) (*controlplanev1.CompleteRuntimeSecretOperationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_CompleteRuntimeSecretOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.CompleteRuntimeSecretOperation(ctx, p, platformrepo.RuntimeSecretCompleteInput{
		OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(),
		Materialization: runtimeSecretMaterialization(request.GetMaterialization()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.CompleteRuntimeSecretOperationResponse{Secret: castRuntimeSecret(item)}, nil
}

func (server *Server) FailRuntimeSecretOperation(ctx context.Context, request *controlplanev1.FailRuntimeSecretOperationRequest) (*controlplanev1.FailRuntimeSecretOperationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_FailRuntimeSecretOperation_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.FailRuntimeSecretOperation(ctx, p, platformrepo.RuntimeSecretFailInput{
		OperationRef: request.GetOperationRef(), ClaimantID: request.GetClaimantId(), ClaimGeneration: request.GetClaimGeneration(),
		FailureCode: runtimeSecretFailureCodeName(request.GetFailureCode()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.FailRuntimeSecretOperationResponse{
		OperationRef: result.OperationRef, State: runtimeSecretOperationState(result.State),
		FailureCode: runtimeSecretFailureCode(result.FailureCode),
	}, nil
}

func (server *Server) RecoverRuntimeSecretMaterialization(ctx context.Context, request *controlplanev1.RecoverRuntimeSecretMaterializationRequest) (*controlplanev1.RecoverRuntimeSecretMaterializationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeSecretWorkService_RecoverRuntimeSecretMaterialization_FullMethodName)
	if err != nil {
		return nil, err
	}
	materialization := runtimeSecretMaterialization(request.GetMaterialization())
	if materialization == nil {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := server.service.RecoverRuntimeSecretMaterialization(ctx, p, platformrepo.RuntimeSecretRecoveryInput{
		OperationRef: request.GetOperationRef(), Materialization: *materialization,
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.RecoverRuntimeSecretMaterializationResponse{
		Action: runtimeSecretRecoveryAction(result.Action), OperationState: runtimeSecretOperationState(result.OperationState),
	}
	if result.Secret != nil {
		response.Secret = castRuntimeSecret(*result.Secret)
	}
	return response, nil
}

func castRuntimeSecret(value entity.RuntimeSecret) *controlplanev1.RuntimeSecret {
	result := &controlplanev1.RuntimeSecret{
		Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef, Name: value.Name,
		Description: value.Description, ValueType: runtimeSecretValueType(value.ValueType), State: value.State,
		CurrentRevision: value.CurrentRevision, CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt), Namespace: value.Namespace,
	}
	if value.DisplayHint != nil {
		result.DisplayHint = &controlplanev1.RuntimeSecretDisplayHint{Prefix: value.DisplayHint.Prefix, Suffix: value.DisplayHint.Suffix}
	}
	if value.CurrentRevisionDescriptor != nil {
		result.CurrentRevisionDescriptor = castRuntimeSecretRevisionDescriptor(*value.CurrentRevisionDescriptor)
	}
	return result
}

func castRuntimeSecretRevisionDescriptor(value entity.RuntimeSecretRevisionDescriptor) *controlplanev1.RuntimeSecretRevisionDescriptor {
	return &controlplanev1.RuntimeSecretRevisionDescriptor{
		Revision: value.Revision, Namespace: value.Namespace, SecretName: value.SecretName, SecretKey: value.SecretKey,
		SecretUid: value.SecretUID, SecretResourceVersion: value.SecretResourceVersion, ContentSha256: value.ContentSHA256,
	}
}

func runtimeSecretMaterialization(value *controlplanev1.RuntimeSecretMaterialization) *entity.RuntimeSecretMaterialization {
	if value == nil {
		return nil
	}
	result := &entity.RuntimeSecretMaterialization{
		Namespace: value.GetNamespace(), SecretName: value.GetSecretName(), SecretKey: value.GetSecretKey(),
		SecretUID: value.GetSecretUid(), SecretResourceVersion: value.GetSecretResourceVersion(), ContentSHA256: value.GetContentSha256(),
	}
	if hint := value.GetDisplayHint(); hint != nil {
		result.DisplayHint = &entity.RuntimeSecretDisplayHint{Prefix: hint.GetPrefix(), Suffix: hint.GetSuffix()}
	}
	return result
}

func runtimeSecretValueType(value string) controlplanev1.RuntimeSecretValueType {
	switch value {
	case "STRING":
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING
	case "BINARY":
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_BINARY
	case "JSON":
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_JSON
	default:
		return controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_UNSPECIFIED
	}
}

func runtimeSecretValueTypeName(value controlplanev1.RuntimeSecretValueType) string {
	return enumSuffix(value, "RUNTIME_SECRET_VALUE_TYPE_")
}

func runtimeSecretOperationKind(value string) controlplanev1.RuntimeSecretOperationKind {
	switch value {
	case "CREATE":
		return controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE
	case "ROTATE":
		return controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE
	case "REVEAL":
		return controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL
	case "REVOKE":
		return controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE
	default:
		return controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_UNSPECIFIED
	}
}

func runtimeSecretOperationState(value string) controlplanev1.RuntimeSecretOperationState {
	switch value {
	case "PREPARED":
		return controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_PREPARED
	case "CLAIMED":
		return controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_CLAIMED
	case "COMPLETED":
		return controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED
	case "FAILED":
		return controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED
	default:
		return controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_UNSPECIFIED
	}
}

func runtimeSecretFailureCode(value string) controlplanev1.RuntimeSecretFailureCode {
	switch value {
	case "KUBERNETES_UNAVAILABLE":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_KUBERNETES_UNAVAILABLE
	case "MATERIALIZATION_CONFLICT":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_CONFLICT
	case "MATERIALIZATION_INVALID":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID
	case "STALE_SECRET_VERSION":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_STALE_SECRET_VERSION
	case "RECONCILIATION_FAILED":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_RECONCILIATION_FAILED
	case "GRANT_EXPIRED":
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_GRANT_EXPIRED
	default:
		return controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_UNSPECIFIED
	}
}

func runtimeSecretFailureCodeName(value controlplanev1.RuntimeSecretFailureCode) string {
	return enumSuffix(value, "RUNTIME_SECRET_FAILURE_CODE_")
}

func runtimeSecretRecoveryAction(value string) controlplanev1.RuntimeSecretRecoveryAction {
	switch value {
	case "KEEP":
		return controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP
	case "DELETE":
		return controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_DELETE
	default:
		return controlplanev1.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_UNSPECIFIED
	}
}
