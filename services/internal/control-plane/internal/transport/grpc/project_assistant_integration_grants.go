package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func castAssistantIntegrationGrantCandidate(item entity.SystemAssistantIntegrationGrantCandidate) (*cp.SystemAssistantIntegrationGrantCandidate, error) {
	reason, err := integrationCandidateReason(item.Reason)
	if err != nil {
		return nil, err
	}
	return &cp.SystemAssistantIntegrationGrantCandidate{Capability: castIntegrationCapability(item.Capability), Grantable: item.Grantable, Reason: reason,
		CurrentGrantRef: item.CurrentGrantRef, CurrentGrantVersion: item.CurrentGrantVersion, CurrentApprovalPolicy: integrationApprovalPolicy(item.CurrentApprovalPolicy),
		CurrentApprovalScopePaths: append([]string{}, item.CurrentApprovalScopePaths...), CurrentGrantEnabled: item.CurrentGrantEnabled}, nil
}

func (server *Server) GetProjectAssistantIntegrationGrantCandidates(ctx context.Context, request *cp.GetProjectAssistantIntegrationGrantCandidatesRequest) (*cp.GetProjectAssistantIntegrationGrantCandidatesResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_GetProjectAssistantIntegrationGrantCandidates_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetProjectAssistantIntegrationGrantCandidates(ctx, p, request.GetProjectRef(), request.GetConnectionRef(), request.GetQuery(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.GetProjectAssistantIntegrationGrantCandidatesResponse{ScopeKind: runtimeSecretScopeKind(result.ScopeKind), OrganizationRef: result.OrganizationRef,
		ProjectRef: result.ProjectRef, AssistantProfileRef: result.ProfileRef, ProfileVersion: result.ProfileVersion, AssistantRef: result.AssistantRef, AssistantVersion: result.AssistantVersion,
		ConnectionRef: result.ConnectionRef, ConnectionVersion: result.ConnectionVersion, DefinitionVersion: result.DefinitionVersion, DefinitionDigest: result.DefinitionDigest,
		Items: []*cp.SystemAssistantIntegrationGrantCandidate{}, Total: result.Total, Page: &cp.PageInfo{NextPageToken: result.NextPageToken}}
	for _, item := range result.Items {
		candidate, err := castAssistantIntegrationGrantCandidate(item)
		if err != nil {
			return nil, transportError(err)
		}
		response.Items = append(response.Items, candidate)
	}
	return response, nil
}
