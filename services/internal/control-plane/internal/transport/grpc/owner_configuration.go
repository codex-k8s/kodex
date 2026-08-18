package grpc

import (
	"context"
	"errors"
	"strconv"

	controlplanecontract "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func providerEffectReceiptFromProto(receipt *controlplanev1.ProviderEffectReadbackReceipt) (value.ProviderEffectReceipt, error) {
	if receipt == nil {
		return value.ProviderEffectReceipt{}, nil
	}
	issuedAt, err := requiredTime(receipt.GetIssuedAt())
	if err != nil {
		return value.ProviderEffectReceipt{}, err
	}
	notBefore, err := requiredTime(receipt.GetNotBefore())
	if err != nil {
		return value.ProviderEffectReceipt{}, err
	}
	expiresAt, err := requiredTime(receipt.GetExpiresAt())
	if err != nil {
		return value.ProviderEffectReceipt{}, err
	}
	observedAt, err := optionalTime(receipt.GetObservedAt())
	if err != nil {
		return value.ProviderEffectReceipt{}, err
	}
	resetsAt, err := optionalTime(receipt.GetResetsAt())
	if err != nil {
		return value.ProviderEffectReceipt{}, err
	}
	observationExpiresAt, err := optionalTime(receipt.GetObservationExpiresAt())
	if err != nil {
		return value.ProviderEffectReceipt{}, err
	}
	result := value.ProviderEffectReceipt{
		ContractVersion: receipt.GetContractVersion(), Issuer: receipt.GetIssuer(), Purpose: receipt.GetPurpose(),
		WorkloadID: receipt.GetWorkloadId(), CallerSPIFFEID: receipt.GetCallerSpiffeId(), FullMethod: receipt.GetFullMethod(),
		ActorID: receipt.GetActorId(), OrganizationID: receipt.GetOrganizationId(), ProjectID: receipt.GetProjectId(),
		WorkspaceID: receipt.GetWorkspaceId(), ProviderTeamRef: receipt.GetProviderTeamRef(), ProviderObjectRef: receipt.GetProviderObjectRef(),
		Action: receipt.GetAction(), Effect: receipt.GetEffect(), EffectVersion: receipt.GetEffectVersion(),
		EffectGeneration: receipt.GetEffectGeneration(), EffectSHA256: receipt.GetEffectSha256(), ReceiptID: receipt.GetReceiptId(),
		ReceiptRevision: receipt.GetReceiptRevision(), IssuedAt: issuedAt, NotBefore: notBefore, ExpiresAt: expiresAt,
		CredentialBindingID: receipt.GetCredentialBindingId(), CredentialBindingVersion: receipt.GetCredentialBindingVersion(),
		CredentialBindingSHA256: receipt.GetCredentialBindingSha256(), ProviderUsername: receipt.GetProviderUsername(),
		MaskedStatus: receipt.GetMaskedStatus(),
		Provider:     receipt.GetProvider(), MaskedLabel: receipt.GetMaskedLabel(), Capabilities: receipt.GetCapabilities(),
		Eligible: receipt.GetEligible(), TargetKind: receipt.GetTargetKind(),
		TargetResourceID: receipt.GetTargetResourceId(), TargetStableKey: receipt.GetTargetStableKey(),
		CommandIntentSHA256: receipt.GetCommandIntentSha256(),
		SecretRef:           receipt.GetSecretRef(), SecretVersion: receipt.GetSecretVersion(),
		SecretContentSHA256: receipt.GetSecretContentSha256(), MaskedAccount: receipt.GetMaskedAccount(),
		ObservedUsage: receipt.GetObservedUsage(), ObservedLimit: receipt.GetObservedLimit(),
		ObservationRevision: receipt.GetObservationRevision(), ObservedAt: observedAt,
		WindowDurationSeconds: receipt.GetWindowDurationSeconds(), ResetsAt: resetsAt,
		ObservationExpiresAt: observationExpiresAt,
		ObservationSHA256:    receipt.GetObservationSha256(),
	}
	result.Audience = map[string]string{
		"MATTERMOST_PROVIDER_READBACK_RECEIPT": "urn:mattercodex:provider-readback:mattermost",
		"AI_PROVIDER_READBACK_RECEIPT":         "urn:mattercodex:provider-readback:ai",
	}[result.Purpose]
	if result.ContractVersion == 0 {
		return value.ProviderEffectReceipt{}, errors.New("provider receipt is empty")
	}
	return result, nil
}

func gitReconciliationReceiptFromProto(receipt *controlplanev1.GitReconciliationReceipt) (value.GitReconciliationReceipt, error) {
	if receipt == nil {
		return value.GitReconciliationReceipt{}, errors.New("git reconciliation receipt is empty")
	}
	issuedAt, err := requiredTime(receipt.GetIssuedAt())
	if err != nil {
		return value.GitReconciliationReceipt{}, err
	}
	notBefore, err := requiredTime(receipt.GetNotBefore())
	if err != nil {
		return value.GitReconciliationReceipt{}, err
	}
	expiresAt, err := requiredTime(receipt.GetExpiresAt())
	if err != nil {
		return value.GitReconciliationReceipt{}, err
	}
	return value.GitReconciliationReceipt{
		ContractVersion: receipt.GetContractVersion(), Issuer: receipt.GetIssuer(), Audience: "urn:mattercodex:git-reconciliation", Purpose: receipt.GetPurpose(),
		WorkloadID: receipt.GetWorkloadId(), CallerSPIFFEID: receipt.GetCallerSpiffeId(), FullMethod: receipt.GetFullMethod(),
		ActorID: receipt.GetActorId(), OrganizationID: receipt.GetOrganizationId(), ProjectID: receipt.GetProjectId(),
		TargetKind: receipt.GetTargetKind(), TargetResourceID: receipt.GetTargetResourceId(),
		TargetStableKey: receipt.GetTargetStableKey(), SourceRef: receipt.GetSourceRef(),
		SourceRevision: receipt.GetSourceRevision(), SourceSHA256: receipt.GetSourceSha256(),
		CommandIntentSHA256: receipt.GetCommandIntentSha256(), ReceiptID: receipt.GetReceiptId(),
		ReceiptRevision: receipt.GetReceiptRevision(), IssuedAt: issuedAt, NotBefore: notBefore, ExpiresAt: expiresAt,
	}, nil
}

func (server *Server) manageProtectedConfiguration(
	ctx context.Context,
	fullMethod string,
	input resource.ManageProtectedConfigurationInput,
	semanticIntent ...func(controlplanecontract.VerifiedCommandAuthority) (string, error),
) (*controlplanev1.Resource, error) {
	managed, principal, err := server.manageProtectedConfigurationResource(ctx, fullMethod, input, semanticIntent...)
	if err != nil {
		return nil, err
	}
	encoded, err := toProtoResource(managed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return encoded, nil
}

func (server *Server) manageProtectedConfigurationResource(
	ctx context.Context,
	fullMethod string,
	input resource.ManageProtectedConfigurationInput,
	semanticIntent ...func(controlplanecontract.VerifiedCommandAuthority) (string, error),
) (entity.Resource, value.Principal, error) {
	principal, err := authorization.Principal(ctx, fullMethod)
	if err != nil {
		return entity.Resource{}, value.Principal{}, rpcError("", errs.ErrUnauthenticated)
	}
	if len(semanticIntent) > 1 {
		return entity.Resource{}, principal, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	if len(semanticIntent) == 1 {
		input.SemanticIntentSHA256, err = semanticIntent[0](controlplanecontract.VerifiedCommandAuthority{
			ActorID: principal.ActorID, OrganizationID: principal.OrganizationID,
			ProjectID: principal.ProjectID, WorkloadID: principal.CallerWorkload, FullMethod: fullMethod,
		})
		if err != nil {
			return entity.Resource{}, principal, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
		}
	}
	input.Principal = principal
	input.FullMethod = fullMethod
	managed, err := server.service.ManageProtectedConfiguration(ctx, input)
	if err != nil {
		return entity.Resource{}, principal, rpcError(principal.CorrelationID, err)
	}
	return managed, principal, nil
}

func (server *Server) ManageRoleDefinition(
	ctx context.Context,
	request *controlplanev1.ManageRoleDefinitionRequest,
) (*controlplanev1.ManageRoleDefinitionResponse, error) {
	managed, err := server.manageProtectedConfiguration(ctx,
		controlplanev1.ControlPlaneService_ManageRoleDefinition_FullMethodName,
		resource.ManageProtectedConfigurationInput{
			IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindRoleDefinition,
			Action:     trimEnum(request.GetAction().String(), "PROTECTED_CONFIGURATION_ACTION_"),
			ResourceID: request.GetRoleDefinitionId(), ExpectedVersion: request.GetExpectedVersion(),
			Name: request.GetName(), Spec: roleDefinitionFromProto(request.GetSpec()),
		})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ManageRoleDefinitionResponse{RoleDefinition: managed}, nil
}

func (server *Server) ReconcileGitRoleDefinition(
	ctx context.Context,
	request *controlplanev1.ReconcileGitRoleDefinitionRequest,
) (*controlplanev1.ReconcileGitRoleDefinitionResponse, error) {
	receipt, castErr := gitReconciliationReceiptFromProto(request.GetReconciliationReceipt())
	if castErr != nil {
		return nil, rpcError("", errs.ErrInvalidInput)
	}
	managed, err := server.manageProtectedConfiguration(ctx,
		controlplanev1.ControlPlaneService_ReconcileGitRoleDefinition_FullMethodName,
		resource.ManageProtectedConfigurationInput{
			IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindRoleDefinition, Action: "reconcile_git",
			ResourceID: request.GetRoleDefinitionId(), ExpectedVersion: request.GetExpectedVersion(),
			Name: request.GetName(), Spec: roleDefinitionFromProto(request.GetSpec()), GitReceipt: receipt,
		}, func(authority controlplanecontract.VerifiedCommandAuthority) (string, error) {
			return controlplanecontract.GitReconciliationIntentSHA256(authority, request)
		})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ReconcileGitRoleDefinitionResponse{RoleDefinition: managed}, nil
}

func (server *Server) ManageAgent(
	ctx context.Context,
	request *controlplanev1.ManageAgentRequest,
) (*controlplanev1.ManageAgentResponse, error) {
	managed, principal, err := server.manageProtectedConfigurationResource(ctx,
		controlplanev1.ControlPlaneService_ManageAgent_FullMethodName,
		resource.ManageProtectedConfigurationInput{
			IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindAgent,
			Action:     trimEnum(request.GetAction().String(), "PROTECTED_CONFIGURATION_ACTION_"),
			ResourceID: request.GetAgentId(), ExpectedVersion: request.GetExpectedVersion(),
			Name: request.GetName(), Spec: agentFromProto(request.GetSpec()),
			ReferenceKeys: []string{
				request.GetRoleDefinitionStableKey(), request.GetInstructionSetStableKey(),
				request.GetProviderPoolStableKey(),
			},
		})
	if err != nil {
		return nil, err
	}
	projection, projectionErr := server.service.AgentOwnerProjection(ctx, principal, managed)
	if projectionErr != nil {
		return nil, rpcError(principal.CorrelationID, projectionErr)
	}
	return &controlplanev1.ManageAgentResponse{Projection: agentOwnerProjectionToProto(projection)}, nil
}

func (server *Server) ReconcileGitAgent(
	ctx context.Context,
	request *controlplanev1.ReconcileGitAgentRequest,
) (*controlplanev1.ReconcileGitAgentResponse, error) {
	receipt, castErr := gitReconciliationReceiptFromProto(request.GetReconciliationReceipt())
	if castErr != nil {
		return nil, rpcError("", errs.ErrInvalidInput)
	}
	managed, err := server.manageProtectedConfiguration(ctx,
		controlplanev1.ControlPlaneService_ReconcileGitAgent_FullMethodName,
		resource.ManageProtectedConfigurationInput{
			IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindAgent, Action: "reconcile_git",
			ResourceID: request.GetAgentId(), ExpectedVersion: request.GetExpectedVersion(),
			Name: request.GetName(), Spec: agentFromProto(request.GetSpec()),
			ReferenceKeys: []string{
				request.GetRoleDefinitionStableKey(), request.GetInstructionSetStableKey(),
				request.GetProviderPoolStableKey(),
			}, GitReceipt: receipt,
		}, func(authority controlplanecontract.VerifiedCommandAuthority) (string, error) {
			return controlplanecontract.GitReconciliationIntentSHA256(authority, request)
		})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ReconcileGitAgentResponse{Agent: managed}, nil
}

func (server *Server) ManageAgentMattermostBotIdentity(
	ctx context.Context,
	request *controlplanev1.ManageAgentMattermostBotIdentityRequest,
) (*controlplanev1.ManageAgentMattermostBotIdentityResponse, error) {
	receipt, castErr := providerEffectReceiptFromProto(request.GetProviderReceipt())
	if castErr != nil {
		return nil, rpcError("", errs.ErrInvalidInput)
	}
	action := ""
	switch request.GetAction() {
	case controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_BIND:
		action = "bind_bot"
	case controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REBIND:
		action = "rebind_bot"
	case controlplanev1.AgentMattermostBotIdentityAction_AGENT_MATTERMOST_BOT_IDENTITY_ACTION_REVOKE:
		action = "revoke_bot"
	default:
		return nil, rpcError("", errs.ErrInvalidInput)
	}
	if request.GetReadiness() {
		principal, principalErr := authorization.Principal(ctx,
			controlplanev1.ControlPlaneService_ManageAgentMattermostBotIdentity_FullMethodName)
		if principalErr != nil {
			return nil, rpcError("", errs.ErrUnauthenticated)
		}
		checked, checkErr := server.service.CheckAgentMattermostBotIdentityManageReadiness(ctx,
			resource.ManageProtectedConfigurationInput{
				Principal:      principal,
				FullMethod:     controlplanev1.ControlPlaneService_ManageAgentMattermostBotIdentity_FullMethodName,
				IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindAgent,
				Action: action, ResourceID: request.GetAgentId(), ExpectedVersion: request.GetExpectedVersion(),
				ProviderReceipt: receipt, Readiness: true,
			})
		if checkErr != nil {
			return nil, rpcError(principal.CorrelationID, checkErr)
		}
		encoded, encodeErr := toProtoResource(checked)
		if encodeErr != nil {
			return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
		}
		projection, projectionErr := server.service.AgentOwnerProjection(ctx, principal, checked)
		if projectionErr != nil {
			return nil, rpcError(principal.CorrelationID, projectionErr)
		}
		return &controlplanev1.ManageAgentMattermostBotIdentityResponse{
			Agent: encoded, Projection: agentOwnerProjectionToProto(projection)}, nil
	}
	managed, principal, err := server.manageProtectedConfigurationResource(ctx,
		controlplanev1.ControlPlaneService_ManageAgentMattermostBotIdentity_FullMethodName,
		resource.ManageProtectedConfigurationInput{
			IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindAgent,
			Action: action, ResourceID: request.GetAgentId(), ExpectedVersion: request.GetExpectedVersion(),
			ProviderReceipt: receipt, Readiness: false,
		})
	if err != nil {
		return nil, err
	}
	projection, projectionErr := server.service.AgentOwnerProjection(ctx, principal, managed)
	if projectionErr != nil {
		return nil, rpcError(principal.CorrelationID, projectionErr)
	}
	encoded, encodeErr := toProtoResource(managed)
	if encodeErr != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ManageAgentMattermostBotIdentityResponse{
		Agent: encoded, Projection: agentOwnerProjectionToProto(projection)}, nil
}

func (server *Server) ManageAgentAssignment(
	ctx context.Context,
	request *controlplanev1.ManageAgentAssignmentRequest,
) (*controlplanev1.ManageAgentAssignmentResponse, error) {
	managed, err := server.manageProtectedConfiguration(ctx,
		controlplanev1.ControlPlaneService_ManageAgentAssignment_FullMethodName,
		resource.ManageProtectedConfigurationInput{
			IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindAgentAssignment,
			Action:     trimEnum(request.GetAction().String(), "AGENT_ASSIGNMENT_ACTION_"),
			ResourceID: request.GetAssignmentId(), ExpectedVersion: request.GetExpectedVersion(),
			Name: request.GetName(), Spec: entity.AgentAssignmentSpec{},
			ReferenceKeys: []string{request.GetAgentStableKey(), request.GetRoomStableKey()},
		})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ManageAgentAssignmentResponse{Assignment: managed}, nil
}

func (server *Server) ManageInstructionSet(
	ctx context.Context,
	request *controlplanev1.ManageInstructionSetRequest,
) (*controlplanev1.ManageInstructionSetResponse, error) {
	managed, err := server.manageProtectedConfiguration(ctx,
		controlplanev1.ControlPlaneService_ManageInstructionSet_FullMethodName,
		resource.ManageProtectedConfigurationInput{
			IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindInstructionSet,
			Action:     trimEnum(request.GetAction().String(), "INSTRUCTION_SET_ACTION_"),
			ResourceID: request.GetInstructionSetId(), ExpectedVersion: request.GetExpectedVersion(),
			Name: request.GetName(), Spec: instructionSetFromProto(request.GetSpec()),
			TargetVersion: request.GetTargetVersion(),
		})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ManageInstructionSetResponse{InstructionSet: managed}, nil
}

func (server *Server) ReconcileGitInstructionSet(
	ctx context.Context,
	request *controlplanev1.ReconcileGitInstructionSetRequest,
) (*controlplanev1.ReconcileGitInstructionSetResponse, error) {
	receipt, castErr := gitReconciliationReceiptFromProto(request.GetReconciliationReceipt())
	if castErr != nil {
		return nil, rpcError("", errs.ErrInvalidInput)
	}
	managed, err := server.manageProtectedConfiguration(ctx,
		controlplanev1.ControlPlaneService_ReconcileGitInstructionSet_FullMethodName,
		resource.ManageProtectedConfigurationInput{
			IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindInstructionSet, Action: "reconcile_git",
			ResourceID: request.GetInstructionSetId(), ExpectedVersion: request.GetExpectedVersion(),
			Name: request.GetName(), Spec: instructionSetFromProto(request.GetSpec()), GitReceipt: receipt,
		}, func(authority controlplanecontract.VerifiedCommandAuthority) (string, error) {
			return controlplanecontract.GitReconciliationIntentSHA256(authority, request)
		})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ReconcileGitInstructionSetResponse{InstructionSet: managed}, nil
}

func (server *Server) ManageProviderConnectionReference(
	ctx context.Context,
	request *controlplanev1.ManageProviderConnectionReferenceRequest,
) (*controlplanev1.ManageProviderConnectionReferenceResponse, error) {
	receipt, receiptErr := providerEffectReceiptFromProto(request.GetProviderReceipt())
	if receiptErr != nil {
		return nil, rpcError("", errs.ErrInvalidInput)
	}
	action := trimEnum(request.GetAction().String(), "PROVIDER_CONNECTION_REFERENCE_ACTION_")
	var spec entity.Spec = entity.ProviderConnectionReferenceSpec{}
	if action == "register" || action == "refresh" {
		var castErr error
		spec, castErr = providerReferenceFromProto(request.GetSpec())
		if castErr != nil {
			return nil, rpcError("", errs.ErrInvalidInput)
		}
	}
	managed, err := server.manageProtectedConfiguration(ctx,
		controlplanev1.ControlPlaneService_ManageProviderConnectionReference_FullMethodName,
		resource.ManageProtectedConfigurationInput{
			IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindProviderReference,
			Action:     action,
			ResourceID: request.GetProviderConnectionReferenceId(), ExpectedVersion: request.GetExpectedVersion(),
			Name: request.GetName(), Spec: spec, ProviderReceipt: receipt,
		}, func(authority controlplanecontract.VerifiedCommandAuthority) (string, error) {
			return controlplanecontract.ProviderConnectionReferenceIntentSHA256(authority, request)
		})
	if err != nil {
		return nil, err
	}
	response := &controlplanev1.ManageProviderConnectionReferenceResponse{ProviderConnectionReference: managed}
	if action != "archive" {
		principal, principalErr := authorization.Principal(ctx,
			controlplanev1.ControlPlaneService_ManageProviderConnectionReference_FullMethodName)
		bindingID := managed.GetSpec().GetProviderConnectionReference().GetCredentialBindingId()
		if principalErr != nil || bindingID == "" {
			return nil, rpcError("", errs.ErrInternal)
		}
		binding, bindingErr := server.service.GetMaterializedProviderCredential(ctx, principal, bindingID)
		if bindingErr != nil {
			return nil, rpcError(principal.CorrelationID, bindingErr)
		}
		response.CredentialBinding, bindingErr = toProtoResource(binding)
		if bindingErr != nil {
			return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
		}
	}
	return response, nil
}

func (server *Server) ManageProviderPool(
	ctx context.Context,
	request *controlplanev1.ManageProviderPoolRequest,
) (*controlplanev1.ManageProviderPoolResponse, error) {
	receipt, receiptErr := providerEffectReceiptFromProto(request.GetProviderReceipt())
	if receiptErr != nil {
		return nil, rpcError("", errs.ErrInvalidInput)
	}
	spec, castErr := providerPoolFromProto(request.GetSpec())
	if castErr != nil {
		return nil, rpcError("", errs.ErrInvalidInput)
	}
	keys := make([]string, 0, len(request.GetSpec().GetBindings()))
	for _, binding := range request.GetSpec().GetBindings() {
		keys = append(keys, binding.GetProviderConnectionStableKey())
	}
	managed, err := server.manageProtectedConfiguration(ctx,
		controlplanev1.ControlPlaneService_ManageProviderPool_FullMethodName,
		resource.ManageProtectedConfigurationInput{
			IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindProviderPool,
			Action:     trimEnum(request.GetAction().String(), "PROVIDER_POOL_ACTION_"),
			ResourceID: request.GetProviderPoolId(), ExpectedVersion: request.GetExpectedVersion(),
			Name: request.GetName(), Spec: spec, ReferenceKeys: keys, ProviderReceipt: receipt,
		}, func(authority controlplanecontract.VerifiedCommandAuthority) (string, error) {
			return controlplanecontract.ProviderPoolIntentSHA256(authority, request)
		})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ManageProviderPoolResponse{ProviderPool: managed}, nil
}

func (server *Server) ReconcileGitProviderPool(
	ctx context.Context,
	request *controlplanev1.ReconcileGitProviderPoolRequest,
) (*controlplanev1.ReconcileGitProviderPoolResponse, error) {
	receipt, receiptErr := gitReconciliationReceiptFromProto(request.GetReconciliationReceipt())
	if receiptErr != nil {
		return nil, rpcError("", errs.ErrInvalidInput)
	}
	spec, castErr := providerPoolFromProto(request.GetSpec())
	if castErr != nil {
		return nil, rpcError("", errs.ErrInvalidInput)
	}
	keys := make([]string, 0, len(request.GetSpec().GetBindings()))
	for _, binding := range request.GetSpec().GetBindings() {
		keys = append(keys, binding.GetProviderConnectionStableKey())
	}
	managed, err := server.manageProtectedConfiguration(ctx,
		controlplanev1.ControlPlaneService_ReconcileGitProviderPool_FullMethodName,
		resource.ManageProtectedConfigurationInput{
			IdempotencyKey: request.GetIdempotencyKey(), Kind: enum.KindProviderPool, Action: "reconcile_git",
			ResourceID: request.GetProviderPoolId(), ExpectedVersion: request.GetExpectedVersion(),
			Name: request.GetName(), Spec: spec, ReferenceKeys: keys, GitReceipt: receipt,
		}, func(authority controlplanecontract.VerifiedCommandAuthority) (string, error) {
			return controlplanecontract.GitReconciliationIntentSHA256(authority, request)
		})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ReconcileGitProviderPoolResponse{ProviderPool: managed}, nil
}

func (server *Server) getProtectedConfiguration(
	ctx context.Context,
	fullMethod string,
	resourceID string,
	kind enum.Kind,
) (*controlplanev1.Resource, error) {
	principal, err := authorization.Principal(ctx, fullMethod)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	found, err := server.service.GetProtectedConfiguration(ctx, principal, resourceID, kind)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(found)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return encoded, nil
}

func (server *Server) listProtectedConfigurations(
	ctx context.Context,
	fullMethod string,
	kind enum.Kind,
	states []controlplanev1.LifecycleState,
	pageToken string,
	requestedPageSize uint32,
) ([]*controlplanev1.Resource, string, error) {
	principal, err := authorization.Principal(ctx, fullMethod)
	if err != nil {
		return nil, "", rpcError("", errs.ErrUnauthenticated)
	}
	parsedStates := make([]enum.State, 0, len(states))
	for _, state := range states {
		parsedStates = append(parsedStates, fromProtoState(state))
	}
	limit := pageSize(requestedPageSize)
	found, err := server.service.ListProtectedConfigurations(ctx, principal, kind, parsedStates, pageToken, limit)
	if err != nil {
		return nil, "", rpcError(principal.CorrelationID, err)
	}
	encoded := make([]*controlplanev1.Resource, 0, len(found))
	for _, item := range found {
		projection, castErr := toProtoResource(item)
		if castErr != nil {
			return nil, "", rpcError(principal.CorrelationID, errs.ErrInternal)
		}
		encoded = append(encoded, projection)
	}
	next := ""
	if len(found) == limit {
		next = found[len(found)-1].ID
	}
	return encoded, next, nil
}

func (server *Server) listProtectedHistory(
	ctx context.Context,
	fullMethod string,
	resourceID string,
	kind enum.Kind,
	pageToken string,
	requestedPageSize uint32,
) ([]*controlplanev1.ProtectedResourceHistoryEntry, string, error) {
	principal, err := authorization.Principal(ctx, fullMethod)
	if err != nil {
		return nil, "", rpcError("", errs.ErrUnauthenticated)
	}
	beforeVersion, err := parseUint64PageToken(pageToken)
	if err != nil {
		return nil, "", rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	limit := pageSize(requestedPageSize)
	found, err := server.service.ListProtectedResourceHistory(ctx, resource.ProtectedResourceHistoryInput{
		Principal: principal, ResourceID: resourceID, Kind: kind, BeforeVersion: beforeVersion, Limit: limit,
	})
	if err != nil {
		return nil, "", rpcError(principal.CorrelationID, err)
	}
	entries := make([]*controlplanev1.ProtectedResourceHistoryEntry, 0, len(found))
	for _, item := range found {
		encoded, castErr := protectedHistoryToProto(item)
		if castErr != nil {
			return nil, "", rpcError(principal.CorrelationID, errs.ErrInternal)
		}
		entries = append(entries, encoded)
	}
	next := ""
	if len(found) == limit {
		next = formatUint64PageToken(found[len(found)-1].Resource.Version)
	}
	return entries, next, nil
}

func protectedHistoryToProto(item domainrepo.ProtectedResourceHistory) (*controlplanev1.ProtectedResourceHistoryEntry, error) {
	encoded, err := toProtoResource(item.Resource)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ProtectedResourceHistoryEntry{
		Resource: encoded, Action: item.Action,
		SnapshotSha256: item.SnapshotSHA256, OccurredAt: timestamppb.New(item.OccurredAt),
	}, nil
}

func parseUint64PageToken(token string) (uint64, error) {
	if token == "" {
		return 0, nil
	}
	return strconv.ParseUint(token, 10, 64)
}

func formatUint64PageToken(value uint64) string {
	return strconv.FormatUint(value, 10)
}

func (server *Server) GetRoleDefinition(ctx context.Context, request *controlplanev1.GetRoleDefinitionRequest) (*controlplanev1.GetRoleDefinitionResponse, error) {
	item, err := server.getProtectedConfiguration(ctx, controlplanev1.ControlPlaneService_GetRoleDefinition_FullMethodName, request.GetRoleDefinitionId(), enum.KindRoleDefinition)
	return &controlplanev1.GetRoleDefinitionResponse{RoleDefinition: item}, err
}

func (server *Server) ListRoleDefinitions(ctx context.Context, request *controlplanev1.ListRoleDefinitionsRequest) (*controlplanev1.ListRoleDefinitionsResponse, error) {
	items, next, err := server.listProtectedConfigurations(ctx, controlplanev1.ControlPlaneService_ListRoleDefinitions_FullMethodName, enum.KindRoleDefinition, request.GetStates(), request.GetPageToken(), request.GetPageSize())
	return &controlplanev1.ListRoleDefinitionsResponse{RoleDefinitions: items, NextPageToken: next}, err
}

func (server *Server) ListRoleDefinitionHistory(ctx context.Context, request *controlplanev1.ListRoleDefinitionHistoryRequest) (*controlplanev1.ListRoleDefinitionHistoryResponse, error) {
	items, next, err := server.listProtectedHistory(ctx, controlplanev1.ControlPlaneService_ListRoleDefinitionHistory_FullMethodName, request.GetRoleDefinitionId(), enum.KindRoleDefinition, request.GetPageToken(), request.GetPageSize())
	return &controlplanev1.ListRoleDefinitionHistoryResponse{Entries: items, NextPageToken: next}, err
}

func (server *Server) GetAgent(ctx context.Context, request *controlplanev1.GetAgentRequest) (*controlplanev1.GetAgentResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetAgent_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	private, handled, privateErr := server.service.GetAgentMattermostBotIdentityReadback(ctx, principal, request.GetAgentId())
	if handled {
		if privateErr != nil {
			return nil, rpcError(principal.CorrelationID, privateErr)
		}
		encoded, encodeErr := toProtoResource(private)
		if encodeErr != nil {
			return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
		}
		return &controlplanev1.GetAgentResponse{Agent: encoded}, nil
	}
	projection, projectionErr := server.service.GetAgentOwner(ctx, principal, request.GetAgentId())
	if projectionErr != nil {
		return nil, rpcError(principal.CorrelationID, projectionErr)
	}
	return &controlplanev1.GetAgentResponse{Projection: agentOwnerProjectionToProto(projection)}, nil
}

func (server *Server) ListAgents(ctx context.Context, request *controlplanev1.ListAgentsRequest) (*controlplanev1.ListAgentsResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ListAgents_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	states := make([]enum.State, 0, len(request.GetStates()))
	for _, state := range request.GetStates() {
		states = append(states, fromProtoState(state))
	}
	limit := pageSize(request.GetPageSize())
	items, next, err := server.service.ListAgentOwners(ctx, principal, states, request.GetPageToken(), limit)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListAgentsResponse{}
	for _, item := range items {
		response.Projections = append(response.Projections, agentOwnerProjectionToProto(item))
	}
	response.NextPageToken = next
	return response, nil
}

func (server *Server) ListAgentHistory(ctx context.Context, request *controlplanev1.ListAgentHistoryRequest) (*controlplanev1.ListAgentHistoryResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ListAgentHistory_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	beforeVersion, err := parseUint64PageToken(request.GetPageToken())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	limit := pageSize(request.GetPageSize())
	items, next, err := server.service.ListAgentOwnerHistory(ctx, principal, request.GetAgentId(), beforeVersion, limit)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListAgentHistoryResponse{}
	for _, item := range items {
		response.Projections = append(response.Projections, &controlplanev1.AgentOwnerHistoryEntry{
			Projection: agentOwnerProjectionToProto(item.Projection), Action: agentHistoryActionToProto(item.Action),
			SnapshotSha256: item.SnapshotSHA256, OccurredAt: timestamppb.New(item.OccurredAt),
		})
	}
	response.NextPageToken = next
	return response, nil
}

func agentHistoryActionToProto(action string) controlplanev1.AgentHistoryAction {
	return map[string]controlplanev1.AgentHistoryAction{
		"create":        controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_CREATE,
		"update":        controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_UPDATE,
		"reconcile_git": controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_RECONCILE_GIT,
		"pause":         controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_PAUSE,
		"resume":        controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_RESUME,
		"disable":       controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_DISABLE,
		"enable":        controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_ENABLE,
		"bind_bot":      controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_BIND_BOT,
		"rebind_bot":    controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_REBIND_BOT,
		"revoke_bot":    controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_REVOKE_BOT,
		"archive":       controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_ARCHIVE,
		"delete":        controlplanev1.AgentHistoryAction_AGENT_HISTORY_ACTION_DELETE,
	}[action]
}

func (server *Server) GetAgentAssignment(ctx context.Context, request *controlplanev1.GetAgentAssignmentRequest) (*controlplanev1.GetAgentAssignmentResponse, error) {
	item, err := server.getProtectedConfiguration(ctx, controlplanev1.ControlPlaneService_GetAgentAssignment_FullMethodName, request.GetAssignmentId(), enum.KindAgentAssignment)
	return &controlplanev1.GetAgentAssignmentResponse{Assignment: item}, err
}

func (server *Server) ListAgentAssignments(ctx context.Context, request *controlplanev1.ListAgentAssignmentsRequest) (*controlplanev1.ListAgentAssignmentsResponse, error) {
	items, next, err := server.listProtectedConfigurations(ctx, controlplanev1.ControlPlaneService_ListAgentAssignments_FullMethodName, enum.KindAgentAssignment, request.GetStates(), request.GetPageToken(), request.GetPageSize())
	if err == nil && (request.GetAgentId() != "" || request.GetWorkspaceId() != "") {
		filtered := items[:0]
		for _, item := range items {
			spec := item.GetSpec().GetAgentAssignment()
			if spec != nil && (request.GetAgentId() == "" || spec.GetAgentId() == request.GetAgentId()) &&
				(request.GetWorkspaceId() == "" || spec.GetWorkspaceId() == request.GetWorkspaceId()) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	return &controlplanev1.ListAgentAssignmentsResponse{Assignments: items, NextPageToken: next}, err
}

func (server *Server) ListAgentAssignmentHistory(ctx context.Context, request *controlplanev1.ListAgentAssignmentHistoryRequest) (*controlplanev1.ListAgentAssignmentHistoryResponse, error) {
	items, next, err := server.listProtectedHistory(ctx, controlplanev1.ControlPlaneService_ListAgentAssignmentHistory_FullMethodName, request.GetAssignmentId(), enum.KindAgentAssignment, request.GetPageToken(), request.GetPageSize())
	return &controlplanev1.ListAgentAssignmentHistoryResponse{Entries: items, NextPageToken: next}, err
}

func (server *Server) GetInstructionSet(ctx context.Context, request *controlplanev1.GetInstructionSetRequest) (*controlplanev1.GetInstructionSetResponse, error) {
	item, err := server.getProtectedConfiguration(ctx, controlplanev1.ControlPlaneService_GetInstructionSet_FullMethodName, request.GetInstructionSetId(), enum.KindInstructionSet)
	return &controlplanev1.GetInstructionSetResponse{InstructionSet: item}, err
}

func (server *Server) ListInstructionSets(ctx context.Context, request *controlplanev1.ListInstructionSetsRequest) (*controlplanev1.ListInstructionSetsResponse, error) {
	items, next, err := server.listProtectedConfigurations(ctx, controlplanev1.ControlPlaneService_ListInstructionSets_FullMethodName, enum.KindInstructionSet, request.GetStates(), request.GetPageToken(), request.GetPageSize())
	return &controlplanev1.ListInstructionSetsResponse{InstructionSets: items, NextPageToken: next}, err
}

func (server *Server) ListInstructionSetHistory(ctx context.Context, request *controlplanev1.ListInstructionSetHistoryRequest) (*controlplanev1.ListInstructionSetHistoryResponse, error) {
	items, next, err := server.listProtectedHistory(ctx, controlplanev1.ControlPlaneService_ListInstructionSetHistory_FullMethodName, request.GetInstructionSetId(), enum.KindInstructionSet, request.GetPageToken(), request.GetPageSize())
	return &controlplanev1.ListInstructionSetHistoryResponse{Entries: items, NextPageToken: next}, err
}

func (server *Server) GetProviderConnectionReference(ctx context.Context, request *controlplanev1.GetProviderConnectionReferenceRequest) (*controlplanev1.GetProviderConnectionReferenceResponse, error) {
	item, err := server.getProtectedConfiguration(ctx, controlplanev1.ControlPlaneService_GetProviderConnectionReference_FullMethodName, request.GetProviderConnectionReferenceId(), enum.KindProviderReference)
	return &controlplanev1.GetProviderConnectionReferenceResponse{ProviderConnectionReference: item}, err
}

func (server *Server) ListProviderConnectionReferences(ctx context.Context, request *controlplanev1.ListProviderConnectionReferencesRequest) (*controlplanev1.ListProviderConnectionReferencesResponse, error) {
	items, next, err := server.listProtectedConfigurations(ctx, controlplanev1.ControlPlaneService_ListProviderConnectionReferences_FullMethodName, enum.KindProviderReference, request.GetStates(), request.GetPageToken(), request.GetPageSize())
	return &controlplanev1.ListProviderConnectionReferencesResponse{ProviderConnectionReferences: items, NextPageToken: next}, err
}

func (server *Server) ListProviderConnectionReferenceHistory(ctx context.Context, request *controlplanev1.ListProviderConnectionReferenceHistoryRequest) (*controlplanev1.ListProviderConnectionReferenceHistoryResponse, error) {
	items, next, err := server.listProtectedHistory(ctx, controlplanev1.ControlPlaneService_ListProviderConnectionReferenceHistory_FullMethodName, request.GetProviderConnectionReferenceId(), enum.KindProviderReference, request.GetPageToken(), request.GetPageSize())
	return &controlplanev1.ListProviderConnectionReferenceHistoryResponse{Entries: items, NextPageToken: next}, err
}

func (server *Server) GetProviderPool(ctx context.Context, request *controlplanev1.GetProviderPoolRequest) (*controlplanev1.GetProviderPoolResponse, error) {
	item, err := server.getProtectedConfiguration(ctx, controlplanev1.ControlPlaneService_GetProviderPool_FullMethodName, request.GetProviderPoolId(), enum.KindProviderPool)
	return &controlplanev1.GetProviderPoolResponse{ProviderPool: item}, err
}

func (server *Server) ListProviderPools(ctx context.Context, request *controlplanev1.ListProviderPoolsRequest) (*controlplanev1.ListProviderPoolsResponse, error) {
	items, next, err := server.listProtectedConfigurations(ctx, controlplanev1.ControlPlaneService_ListProviderPools_FullMethodName, enum.KindProviderPool, request.GetStates(), request.GetPageToken(), request.GetPageSize())
	return &controlplanev1.ListProviderPoolsResponse{ProviderPools: items, NextPageToken: next}, err
}

func (server *Server) ListProviderPoolHistory(ctx context.Context, request *controlplanev1.ListProviderPoolHistoryRequest) (*controlplanev1.ListProviderPoolHistoryResponse, error) {
	items, next, err := server.listProtectedHistory(ctx, controlplanev1.ControlPlaneService_ListProviderPoolHistory_FullMethodName, request.GetProviderPoolId(), enum.KindProviderPool, request.GetPageToken(), request.GetPageSize())
	return &controlplanev1.ListProviderPoolHistoryResponse{Entries: items, NextPageToken: next}, err
}

func (server *Server) CompareInstructionSetVersions(ctx context.Context, request *controlplanev1.CompareInstructionSetVersionsRequest) (*controlplanev1.CompareInstructionSetVersionsResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_CompareInstructionSetVersions_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.CompareInstructionVersions(ctx, resource.CompareInstructionVersionsInput{
		Principal: principal, InstructionSetID: request.GetInstructionSetId(),
		LeftVersion: request.GetLeftVersion(), RightVersion: request.GetRightVersion(),
		PageSize: pageSize(request.GetPageSize()), PageToken: request.GetPageToken(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	leftSpec, leftOK := result.Left.Resource.Spec.(entity.InstructionSetSpec)
	rightSpec, rightOK := result.Right.Resource.Spec.(entity.InstructionSetSpec)
	if !leftOK || !rightOK {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	response := &controlplanev1.CompareInstructionSetVersionsResponse{
		ContentEqual: result.ContentEqual, ComparisonSha256: result.ComparisonSHA256,
		Truncated: result.Page.Truncated, NextPageToken: result.Page.NextPageToken,
		LeftVersionRef: &controlplanev1.ConfigurationVersionRef{Version: result.Left.Resource.Version,
			ContentSha256: leftSpec.ContentSHA256, SnapshotSha256: result.Left.SnapshotSHA256},
		RightVersionRef: &controlplanev1.ConfigurationVersionRef{Version: result.Right.Resource.Version,
			ContentSha256: rightSpec.ContentSHA256, SnapshotSha256: result.Right.SnapshotSHA256},
	}
	for _, change := range result.Page.Changes {
		response.Changes = append(response.Changes, &controlplanev1.ConfigurationChange{
			Kind: map[string]controlplanev1.ConfigurationChangeKind{
				"ADDED":   controlplanev1.ConfigurationChangeKind_CONFIGURATION_CHANGE_KIND_ADDED,
				"REMOVED": controlplanev1.ConfigurationChangeKind_CONFIGURATION_CHANGE_KIND_REMOVED,
				"CHANGED": controlplanev1.ConfigurationChangeKind_CONFIGURATION_CHANGE_KIND_CHANGED,
			}[change.Kind], Path: change.Path,
			Display: map[string]controlplanev1.ConfigurationChangeDisplay{
				"TEXT":     controlplanev1.ConfigurationChangeDisplay_CONFIGURATION_CHANGE_DISPLAY_TEXT,
				"REDACTED": controlplanev1.ConfigurationChangeDisplay_CONFIGURATION_CHANGE_DISPLAY_REDACTED,
			}[change.Display], Before: change.Before, After: change.After,
		})
	}
	return response, nil
}

func (server *Server) BindScheduleConfiguration(
	ctx context.Context,
	request *controlplanev1.BindScheduleConfigurationRequest,
) (*controlplanev1.BindScheduleConfigurationResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_BindScheduleConfiguration_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	bound, err := server.service.BindScheduleConfiguration(ctx, resource.BindScheduleConfigurationInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		ScheduleID: request.GetScheduleId(), ExpectedVersion: request.GetExpectedVersion(),
		AgentStableKey: request.GetAgentStableKey(), InstructionSetStableKey: request.GetInstructionSetStableKey(),
		ProviderPoolStableKey: request.GetProviderPoolStableKey(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(bound)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.BindScheduleConfigurationResponse{Schedule: encoded}, nil
}

func (server *Server) CreateScheduleFromOwnerSelections(
	ctx context.Context,
	request *controlplanev1.CreateScheduleFromOwnerSelectionsRequest,
) (*controlplanev1.CreateScheduleFromOwnerSelectionsResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_CreateScheduleFromOwnerSelections_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	intent := request.GetIntent()
	if intent == nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	if intent.GetPresetKey() != "" {
		selection, selectionErr := ownerScheduleSelectionFromProto(intent)
		if selectionErr != nil {
			return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
		}
		spec, specErr := resource.BuildOwnerScheduleSpec(selection)
		if specErr != nil {
			return nil, rpcError(principal.CorrelationID, specErr)
		}
		created, createErr := server.service.CreateScheduleFromOwnerSelections(ctx, resource.CreateScheduleFromOwnerSelectionsInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), Name: request.GetName(),
			AgentStableKey: request.GetAgentStableKey(), InstructionSetStableKey: request.GetInstructionSetStableKey(),
			ProviderPoolStableKey: request.GetProviderPoolStableKey(), RoomStableKey: selection.RoomStableKey,
			PromptArtifactName: selection.Prompt.ArtifactName, PromptKind: selection.Prompt.Kind,
			PromptInlineMarkdown: selection.Prompt.InlineMarkdown, Spec: spec,
		})
		if createErr != nil {
			return nil, rpcError(principal.CorrelationID, createErr)
		}
		readPrincipal := principal
		readPrincipal.Permission = "controlplane.resource.read"
		projection, projectionErr := server.service.GetOwnerScheduleAtVersion(ctx, readPrincipal, created.ID, created.Version)
		if projectionErr != nil {
			return nil, rpcError(principal.CorrelationID, projectionErr)
		}
		return &controlplanev1.CreateScheduleFromOwnerSelectionsResponse{
			Projection: ownerScheduleProjectionToProto(projection)}, nil
	}
	interval, intervalErr := optionalDuration(intent.GetInterval())
	misfireGrace, misfireErr := optionalDuration(intent.GetMisfireGrace())
	initialBackoff, initialErr := optionalDuration(intent.GetInitialBackoff())
	maximumBackoff, maximumErr := optionalDuration(intent.GetMaximumBackoff())
	deadLetterAfter, deadLetterErr := optionalDuration(intent.GetDeadLetterAfter())
	maximumExecution, executionErr := optionalDuration(intent.GetMaximumExecutionDuration())
	if intervalErr != nil || misfireErr != nil || initialErr != nil || maximumErr != nil ||
		deadLetterErr != nil || executionErr != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	created, err := server.service.CreateScheduleFromOwnerSelections(ctx, resource.CreateScheduleFromOwnerSelectionsInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), Name: request.GetName(),
		AgentStableKey: request.GetAgentStableKey(), InstructionSetStableKey: request.GetInstructionSetStableKey(),
		ProviderPoolStableKey: request.GetProviderPoolStableKey(), RoomStableKey: intent.GetRoomStableKey(),
		PromptArtifactName: intent.GetPromptArtifactName(), Spec: entity.ScheduleSpec{
			Cron: intent.GetCron(), Interval: interval, Timezone: intent.GetTimezone(), Calendar: intent.GetCalendar(),
			OverlapPolicy: trimEnum(intent.GetOverlapPolicy().String(), "SCHEDULE_OVERLAP_POLICY_"),
			MisfirePolicy: trimEnum(intent.GetMisfirePolicy().String(), "SCHEDULE_MISFIRE_POLICY_"),
			MisfireGrace:  misfireGrace, DeliveryPolicy: intent.GetDeliveryPolicy(), MaximumAttempts: intent.GetMaximumAttempts(),
			InitialBackoff: initialBackoff, MaximumBackoff: maximumBackoff, DeadLetterAfter: deadLetterAfter,
			SessionPolicy:            trimEnum(intent.GetSessionPolicy().String(), "SCHEDULE_SESSION_POLICY_"),
			NotificationPolicy:       trimEnum(intent.GetNotificationPolicy().String(), "SCHEDULE_NOTIFICATION_POLICY_"),
			MaximumExecutionDuration: maximumExecution, Coalesce: intent.GetCoalesce(),
		},
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	readPrincipal := principal
	readPrincipal.Permission = "controlplane.resource.read"
	projection, projectionErr := server.service.GetOwnerScheduleAtVersion(ctx, readPrincipal, created.ID, created.Version)
	if projectionErr != nil {
		return nil, rpcError(principal.CorrelationID, projectionErr)
	}
	return &controlplanev1.CreateScheduleFromOwnerSelectionsResponse{
		Projection: ownerScheduleProjectionToProto(projection)}, nil
}

func (server *Server) ManageWorkspaceMattermostMapping(
	ctx context.Context,
	request *controlplanev1.ManageWorkspaceMattermostMappingRequest,
) (*controlplanev1.ManageWorkspaceMattermostMappingResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ManageWorkspaceMattermostMapping_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	receipt, castErr := providerEffectReceiptFromProto(request.GetProviderReceipt())
	if castErr != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	managed, err := server.service.ManageWorkspaceMapping(ctx, resource.ManageWorkspaceMappingInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		Action:    workspaceMattermostMappingAction(request.GetAction()),
		MappingID: request.GetMappingId(), ExpectedVersion: request.GetExpectedVersion(),
		ExpectedGeneration: request.GetExpectedGeneration(), ProviderReceipt: receipt, Name: request.GetName(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(managed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ManageWorkspaceMattermostMappingResponse{Mapping: encoded}, nil
}

func workspaceMattermostMappingAction(action controlplanev1.WorkspaceMattermostMappingAction) string {
	switch action {
	case controlplanev1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_BIND:
		return "bind"
	case controlplanev1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_RELINK:
		return "relink"
	case controlplanev1.WorkspaceMattermostMappingAction_WORKSPACE_MATTERMOST_MAPPING_ACTION_UNLINK:
		return "unlink"
	default:
		return ""
	}
}

func (server *Server) GetWorkspaceMattermostMapping(
	ctx context.Context,
	request *controlplanev1.GetWorkspaceMattermostMappingRequest,
) (*controlplanev1.GetWorkspaceMattermostMappingResponse, error) {
	item, err := server.getProtectedConfiguration(ctx,
		controlplanev1.ControlPlaneService_GetWorkspaceMattermostMapping_FullMethodName,
		request.GetMappingId(), enum.KindWorkspaceMapping)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.GetWorkspaceMattermostMappingResponse{Mapping: item}, nil
}

func (server *Server) ListWorkspaceMattermostMappings(
	ctx context.Context,
	request *controlplanev1.ListWorkspaceMattermostMappingsRequest,
) (*controlplanev1.ListWorkspaceMattermostMappingsResponse, error) {
	states := make([]enum.State, 0, len(request.GetStates()))
	for _, state := range request.GetStates() {
		if state == controlplanev1.WorkspaceMattermostMappingState_WORKSPACE_MATTERMOST_MAPPING_STATE_BOUND {
			states = append(states, enum.StateActive)
		} else if state == controlplanev1.WorkspaceMattermostMappingState_WORKSPACE_MATTERMOST_MAPPING_STATE_UNLINKED {
			states = append(states, enum.StateArchived)
		} else {
			return nil, rpcError("", errs.ErrInvalidInput)
		}
	}
	principal, principalErr := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_ListWorkspaceMattermostMappings_FullMethodName)
	if principalErr != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	limit := pageSize(request.GetPageSize())
	found, err := server.service.ListWorkspaceMattermostMappings(ctx, principal, states,
		request.GetPageToken(), limit)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	items := make([]*controlplanev1.Resource, 0, len(found))
	for _, item := range found {
		projection, castErr := toProtoResource(item)
		if castErr != nil {
			return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
		}
		items = append(items, projection)
	}
	if request.GetWorkspaceId() != "" {
		filtered := items[:0]
		for _, item := range items {
			if spec := item.GetSpec().GetWorkspaceMattermostMapping(); spec != nil && spec.GetWorkspaceId() == request.GetWorkspaceId() {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	next := ""
	if len(found) == limit {
		next = found[len(found)-1].ID
	}
	return &controlplanev1.ListWorkspaceMattermostMappingsResponse{Mappings: items, NextPageToken: next}, nil
}

func (server *Server) ManageWorkspaceBackup(
	ctx context.Context,
	request *controlplanev1.ManageWorkspaceBackupRequest,
) (*controlplanev1.ManageWorkspaceBackupResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ManageWorkspaceBackup_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	retainUntil, castErr := optionalTime(request.GetRetainUntil())
	if castErr != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	managed, err := server.service.ManageWorkspaceBackup(ctx, resource.ManageWorkspaceBackupInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		Action:   trimEnum(request.GetAction().String(), "WORKSPACE_BACKUP_ACTION_"),
		BackupID: request.GetBackupId(), ExpectedVersion: request.GetExpectedVersion(),
		Scope:              trimEnum(request.GetScope().String(), "WORKSPACE_BACKUP_SCOPE_"),
		Name:               request.GetName(),
		TerminalReasonCode: request.GetTerminalReasonCode(), RetainUntil: retainUntil,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(managed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ManageWorkspaceBackupResponse{Backup: encoded}, nil
}

func (server *Server) GetWorkspaceBackup(ctx context.Context, request *controlplanev1.GetWorkspaceBackupRequest) (*controlplanev1.GetWorkspaceBackupResponse, error) {
	item, err := server.getProtectedConfiguration(ctx, controlplanev1.ControlPlaneService_GetWorkspaceBackup_FullMethodName, request.GetBackupId(), enum.KindWorkspaceBackup)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.GetWorkspaceBackupResponse{Backup: item}, nil
}

func (server *Server) ListWorkspaceBackups(ctx context.Context, request *controlplanev1.ListWorkspaceBackupsRequest) (*controlplanev1.ListWorkspaceBackupsResponse, error) {
	items, next, err := server.listProtectedConfigurations(ctx, controlplanev1.ControlPlaneService_ListWorkspaceBackups_FullMethodName,
		enum.KindWorkspaceBackup, request.GetStates(), request.GetPageToken(), request.GetPageSize())
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ListWorkspaceBackupsResponse{Backups: items, NextPageToken: next}, nil
}

func (server *Server) ManageWorkspaceRestore(
	ctx context.Context,
	request *controlplanev1.ManageWorkspaceRestoreRequest,
) (*controlplanev1.ManageWorkspaceRestoreResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ManageWorkspaceRestore_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	managed, err := server.service.ManageWorkspaceRestore(ctx, resource.ManageWorkspaceRestoreInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		Action:    trimEnum(request.GetAction().String(), "WORKSPACE_RESTORE_ACTION_"),
		RestoreID: request.GetRestoreId(), ExpectedVersion: request.GetExpectedVersion(),
		BackupID: request.GetBackupId(), ExpectedBackupVersion: request.GetExpectedBackupVersion(),
		MembershipSHA256: request.GetMembershipSha256(), Name: request.GetName(),
		TerminalReasonCode: request.GetTerminalReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	projection, projectionErr := server.service.WorkspaceRestoreOwnerProjection(ctx, principal, managed)
	if projectionErr != nil {
		return nil, rpcError(principal.CorrelationID, projectionErr)
	}
	return &controlplanev1.ManageWorkspaceRestoreResponse{
		Projection: workspaceRestoreOwnerProjectionToProto(projection)}, nil
}

func (server *Server) GetWorkspaceRestore(ctx context.Context, request *controlplanev1.GetWorkspaceRestoreRequest) (*controlplanev1.GetWorkspaceRestoreResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetWorkspaceRestore_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	projection, projectionErr := server.service.GetWorkspaceRestoreOwner(ctx, principal, request.GetRestoreId())
	if projectionErr != nil {
		return nil, rpcError(principal.CorrelationID, projectionErr)
	}
	return &controlplanev1.GetWorkspaceRestoreResponse{
		Projection: workspaceRestoreOwnerProjectionToProto(projection)}, nil
}

func (server *Server) ListWorkspaceRestores(ctx context.Context, request *controlplanev1.ListWorkspaceRestoresRequest) (*controlplanev1.ListWorkspaceRestoresResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ListWorkspaceRestores_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	if request.GetBackupId() != "" && value.ValidateID(request.GetBackupId()) != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	states := make([]enum.State, 0, len(request.GetStates()))
	for _, state := range request.GetStates() {
		states = append(states, fromProtoState(state))
	}
	limit := pageSize(request.GetPageSize())
	items, next, err := server.service.ListWorkspaceRestoreOwners(ctx, principal, request.GetBackupId(),
		states, request.GetPageToken(), limit)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListWorkspaceRestoresResponse{}
	for _, projection := range items {
		response.Projections = append(response.Projections, workspaceRestoreOwnerProjectionToProto(projection))
	}
	response.NextPageToken = next
	return response, nil
}

func (server *Server) ManageRuntimeIncident(
	ctx context.Context,
	request *controlplanev1.ManageRuntimeIncidentRequest,
) (*controlplanev1.ManageRuntimeIncidentResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ManageRuntimeIncident_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.ManageRuntimeIncident(ctx, resource.ManageRuntimeIncidentInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), IncidentID: request.GetIncidentId(),
		ExpectedVersion: request.GetExpectedVersion(),
		Action:          trimEnum(request.GetAction().String(), "RUNTIME_INCIDENT_ACTION_"),
		ReasonCode:      request.GetReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ManageRuntimeIncidentResponse{}
	projection, projectionErr := server.service.RuntimeIncidentOwnerProjection(ctx, principal, result.Incident)
	if projectionErr != nil {
		return nil, rpcError(principal.CorrelationID, projectionErr)
	}
	response.Projection = runtimeIncidentOwnerProjectionToProto(projection)
	return response, nil
}

func (server *Server) GetRuntimeIncident(
	ctx context.Context,
	request *controlplanev1.GetRuntimeIncidentRequest,
) (*controlplanev1.GetRuntimeIncidentResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetRuntimeIncident_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	projection, projectionErr := server.service.GetRuntimeIncidentOwner(ctx, principal, request.GetIncidentId())
	if projectionErr != nil {
		return nil, rpcError(principal.CorrelationID, projectionErr)
	}
	return &controlplanev1.GetRuntimeIncidentResponse{
		Projection: runtimeIncidentOwnerProjectionToProto(projection)}, nil
}

func (server *Server) ListRuntimeIncidentHistory(
	ctx context.Context,
	request *controlplanev1.ListRuntimeIncidentHistoryRequest,
) (*controlplanev1.ListRuntimeIncidentHistoryResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ListRuntimeIncidentHistory_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	beforeVersion, err := parseUint64PageToken(request.GetPageToken())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	limit := pageSize(request.GetPageSize())
	history, err := server.service.ListRuntimeIncidentOwnerHistory(ctx, principal,
		request.GetIncidentId(), beforeVersion, limit)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListRuntimeIncidentHistoryResponse{
		Entries:     make([]*controlplanev1.RuntimeIncidentHistoryEntry, 0, len(history.Entries)),
		NextActions: runtimeIncidentNextActionsToProto(history.Current.NextActions),
	}
	for _, entry := range history.Entries {
		item := &controlplanev1.RuntimeIncidentHistoryEntry{
			Version: entry.Version, State: runtimeIncidentStateToProto(entry.State),
			Action: runtimeIncidentActionToProto(entry.Action), ReasonCode: entry.ReasonCode,
			OccurredAt: timestamppb.New(entry.OccurredAt), ExecutionFence: entry.ExecutionFence,
		}
		if entry.Version == history.Current.Version && entry.ExecutionFence == history.Current.ExecutionFence {
			item.NextActions = runtimeIncidentNextActionsToProto(history.Current.NextActions)
		}
		response.Entries = append(response.Entries, item)
	}
	response.NextPageToken = history.NextPageToken
	return response, nil
}

func runtimeIncidentStateToProto(state string) controlplanev1.RuntimeIncidentState {
	return map[string]controlplanev1.RuntimeIncidentState{
		"OPEN":         controlplanev1.RuntimeIncidentState_RUNTIME_INCIDENT_STATE_OPEN,
		"ACKNOWLEDGED": controlplanev1.RuntimeIncidentState_RUNTIME_INCIDENT_STATE_ACKNOWLEDGED,
		"RETRYING":     controlplanev1.RuntimeIncidentState_RUNTIME_INCIDENT_STATE_RETRYING,
		"RELEASED":     controlplanev1.RuntimeIncidentState_RUNTIME_INCIDENT_STATE_RELEASED,
		"CLOSED":       controlplanev1.RuntimeIncidentState_RUNTIME_INCIDENT_STATE_CLOSED,
	}[state]
}

func runtimeIncidentActionToProto(action string) controlplanev1.RuntimeIncidentAction {
	return map[string]controlplanev1.RuntimeIncidentAction{
		"acknowledge": controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_ACKNOWLEDGE,
		"retry":       controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_RETRY,
		"release":     controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_RELEASE,
		"close":       controlplanev1.RuntimeIncidentAction_RUNTIME_INCIDENT_ACTION_CLOSE,
	}[action]
}

func (server *Server) ManageRun(
	ctx context.Context,
	request *controlplanev1.ManageRunRequest,
) (*controlplanev1.ManageRunResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ManageRun_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.ManageRun(ctx, resource.ManageRunInput{
		Principal:      principal,
		IdempotencyKey: request.GetIdempotencyKey(), ProcessRunID: request.GetProcessRunId(),
		ExpectedVersion: request.GetExpectedVersion(),
		Action:          trimEnum(request.GetAction().String(), "RUN_ACTION_"), ReasonCode: request.GetReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ManageRunResponse{}
	readPrincipal := principal
	readPrincipal.Permission = "controlplane.resource.read"
	detail, detailErr := server.service.GetRunDetailPage(ctx, readPrincipal, result.ProcessRun.ID, 100, "")
	if detailErr != nil {
		return nil, rpcError(principal.CorrelationID, detailErr)
	}
	if detail.ProcessRun.Version != result.ProcessRun.Version {
		return nil, rpcError(principal.CorrelationID, errs.ErrVersionMismatch)
	}
	projection, projectionErr := server.service.RunOwnerProjection(ctx, principal, detail)
	if projectionErr != nil {
		return nil, rpcError(principal.CorrelationID, projectionErr)
	}
	response.Projection = runOwnerProjectionToProto(projection)
	for _, incident := range detail.IncidentProjections {
		response.IncidentProjections = append(response.IncidentProjections,
			runtimeIncidentOwnerProjectionToProto(incident))
	}
	response.IncidentsNextPageToken = detail.IncidentsNextPageToken
	return response, nil
}

func (server *Server) GetRunDetail(
	ctx context.Context,
	request *controlplanev1.GetRunDetailRequest,
) (*controlplanev1.GetRunDetailResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetRunDetail_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	result, err := server.service.GetRunDetailPage(ctx, principal, request.GetProcessRunId(),
		pageSize(request.GetIncidentPageSize()), request.GetIncidentPageToken())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.GetRunDetailResponse{}
	for _, projection := range result.IncidentProjections {
		response.IncidentProjections = append(response.IncidentProjections,
			runtimeIncidentOwnerProjectionToProto(projection))
	}
	projection, projectionErr := server.service.RunOwnerProjection(ctx, principal, result)
	if projectionErr != nil {
		return nil, rpcError(principal.CorrelationID, projectionErr)
	}
	response.Projection = runOwnerProjectionToProto(projection)
	response.IncidentsNextPageToken = result.IncidentsNextPageToken
	return response, nil
}

func (server *Server) GetRunLineage(
	ctx context.Context,
	request *controlplanev1.GetRunLineageRequest,
) (*controlplanev1.GetRunLineageResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_GetRunLineage_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	lineage, err := server.service.GetRunLineagePage(ctx, principal, request.GetProcessRunId(),
		pageSize(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.GetRunLineageResponse{NextPageToken: lineage.NextPageToken,
		Run: runOwnerProjectionToProto(lineage.Run), Truncated: lineage.Truncated}
	for _, item := range lineage.Projections {
		response.Projections = append(response.Projections, &controlplanev1.RunLineageNode{
			NodeRef: item.NodeRef, ParentRef: item.ParentRef,
			Kind: runLineageKindToProto(item.Kind), State: runLineageStateToProto(item.State),
			Version: item.Version, Attempt: item.Attempt, CreatedAt: timestamppb.New(item.CreatedAt),
			UpdatedAt: timestamppb.New(item.UpdatedAt), DisplayName: item.DisplayName,
			Agent: ownerDisplayValueToProto(item.Agent), Role: ownerDisplayValueToProto(item.Role),
			Model: ownerDisplayValueToProto(item.Model), Provider: ownerDisplayValueToProto(item.Provider),
		})
	}
	for _, action := range lineage.Run.NextActions {
		response.NextActions = append(response.NextActions, runNextActionToProto(action))
	}
	return response, nil
}

func (server *Server) ListRunTimeline(
	ctx context.Context,
	request *controlplanev1.ListRunTimelineRequest,
) (*controlplanev1.ListRunTimelineResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ListRunTimeline_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	limit := pageSize(request.GetPageSize())
	page, err := server.service.ListRunTimelineOwner(ctx, principal, request.GetProcessRunId(), request.GetPageToken(), limit)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListRunTimelineResponse{Run: runOwnerProjectionToProto(page.Run)}
	for _, item := range page.Projections {
		projection := &controlplanev1.RunTimelineEntry{EventRef: item.EventRef,
			Kind: runTimelineKindToProto(item.Kind), Display: item.Display,
			Outcome: runTimelineOutcomeToProto(item.Outcome), Version: item.Version,
			OccurredAt: timestamppb.New(item.OccurredAt)}
		for _, action := range item.NextActions {
			projection.NextActions = append(projection.NextActions, runNextActionToProto(action))
		}
		response.Projections = append(response.Projections, projection)
	}
	for _, action := range page.Run.NextActions {
		response.NextActions = append(response.NextActions, runNextActionToProto(action))
	}
	response.NextPageToken = page.NextPageToken
	return response, nil
}

func (server *Server) ListRunArtifacts(
	ctx context.Context,
	request *controlplanev1.ListRunArtifactsRequest,
) (*controlplanev1.ListRunArtifactsResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_ListRunArtifacts_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	limit := pageSize(request.GetPageSize())
	items, next, err := server.service.ListRunArtifacts(ctx, principal, request.GetProcessRunId(), request.GetPageToken(), limit)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListRunArtifactsResponse{}
	detail, detailErr := server.service.GetRunDetail(ctx, principal, request.GetProcessRunId())
	if detailErr != nil {
		return nil, rpcError(principal.CorrelationID, detailErr)
	}
	ownerProjection, ownerProjectionErr := server.service.RunOwnerProjection(ctx, principal, detail)
	if ownerProjectionErr != nil {
		return nil, rpcError(principal.CorrelationID, ownerProjectionErr)
	}
	projections, projectionErr := resource.RunArtifactOwnerProjections(items)
	if projectionErr != nil {
		return nil, rpcError(principal.CorrelationID, projectionErr)
	}
	for _, item := range projections {
		response.Projections = append(response.Projections, &controlplanev1.RunArtifactProjection{
			ArtifactRef: item.ArtifactRef, DisplayName: item.DisplayName, Kind: item.Kind,
			MediaType: item.MediaType, SizeBytes: item.SizeBytes, Sha256: item.SHA256,
			Status: runArtifactStatusToProto(item.Status), CreatedAt: timestamppb.New(item.CreatedAt),
		})
	}
	for _, action := range ownerProjection.NextActions {
		response.NextActions = append(response.NextActions, runNextActionToProto(action))
	}
	response.NextPageToken = next
	return response, nil
}
