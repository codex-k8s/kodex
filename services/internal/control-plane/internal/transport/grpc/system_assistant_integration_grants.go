package grpc

import (
	"context"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

func (server *Server) ChangeSystemAssistantIntegrationGrant(ctx context.Context, request *controlplanev1.ChangeSystemAssistantIntegrationGrantRequest) (*controlplanev1.ChangeSystemAssistantIntegrationGrantResponse, error) {
	payload := command.SystemAssistantIntegrationGrantInput{ConnectionRef: request.GetConnectionRef(), CapabilityKey: request.GetCapabilityKey(), Enabled: request.GetEnabled(), ApprovalPolicy: strings.TrimPrefix(request.GetApprovalPolicy().String(), "INTEGRATION_APPROVAL_POLICY_"), ApprovalScopePaths: request.GetApprovalScopePaths()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ChangeSystemAssistantIntegrationGrant_FullMethodName, command.ChangeSystemAssistantIntegrationGrant, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ChangeSystemAssistantIntegrationGrantResponse{Connection: castConnection(*result.Connection)}, nil
}

func (server *Server) GetSystemAssistantIntegrationGrantCandidates(ctx context.Context, request *controlplanev1.GetSystemAssistantIntegrationGrantCandidatesRequest) (*controlplanev1.GetSystemAssistantIntegrationGrantCandidatesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetSystemAssistantIntegrationGrantCandidates_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetSystemAssistantIntegrationGrantCandidates(ctx, p, request.GetConnectionRef(), request.GetQuery(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.GetSystemAssistantIntegrationGrantCandidatesResponse{ScopeKind: runtimeSecretScopeKind(result.ScopeKind), OrganizationRef: result.OrganizationRef, AssistantRef: result.AssistantRef, AssistantVersion: result.AssistantVersion, ConnectionRef: result.ConnectionRef, ConnectionVersion: result.ConnectionVersion, DefinitionVersion: result.DefinitionVersion, DefinitionDigest: result.DefinitionDigest, Items: []*controlplanev1.SystemAssistantIntegrationGrantCandidate{}, Total: result.Total, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken}}
	for _, item := range result.Items {
		reason, err := integrationCandidateReason(item.Reason)
		if err != nil {
			return nil, transportError(err)
		}
		response.Items = append(response.Items, &controlplanev1.SystemAssistantIntegrationGrantCandidate{Capability: castIntegrationCapability(item.Capability), Grantable: item.Grantable, Reason: reason, CurrentGrantRef: item.CurrentGrantRef, CurrentGrantVersion: item.CurrentGrantVersion, CurrentApprovalPolicy: integrationApprovalPolicy(item.CurrentApprovalPolicy), CurrentApprovalScopePaths: append([]string{}, item.CurrentApprovalScopePaths...), CurrentGrantEnabled: item.CurrentGrantEnabled})
	}
	return response, nil
}
