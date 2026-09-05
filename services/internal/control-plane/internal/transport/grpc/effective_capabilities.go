package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) GetAgentEffectiveCapabilities(ctx context.Context, request *controlplanev1.GetAgentEffectiveCapabilitiesRequest) (*controlplanev1.GetAgentEffectiveCapabilitiesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetAgentEffectiveCapabilities_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetAgentEffectiveCapabilities(ctx, p, request.GetAgentRef(), request.GetWorkflowRef(), request.GetStepKey(), query.Filter{Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.GetAgentEffectiveCapabilitiesResponse{
		AgentRef: result.AgentRef, ProjectRef: result.ProjectRef, AgentVersion: result.AgentVersion,
		RuntimeConfigurationRef: result.RuntimeConfigurationRef, RuntimeConfigurationVersion: result.RuntimeConfigurationVersion,
		EnvironmentVersionRef: result.EnvironmentVersionRef, WorkflowRef: result.WorkflowRef, WorkflowVersionRef: result.WorkflowVersionRef,
		StepKey: result.StepKey, Digest: result.Digest, RuntimeReady: result.RuntimeReady, EvaluatedAt: timestamp(result.EvaluatedAt),
		Capabilities: []*controlplanev1.EffectiveCapability{}, Total: result.Total, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken},
	}
	for _, item := range result.Items {
		response.Capabilities = append(response.Capabilities, &controlplanev1.EffectiveCapability{
			Key: item.Key, Name: item.Name, Description: item.Description, Source: item.Source, Reason: item.Reason,
			Requested: item.Requested, Required: item.Required, Effective: item.Effective, Grantable: item.Grantable,
			ConnectionRef: item.ConnectionRef, GrantRef: item.GrantRef, DefinitionDigest: item.DefinitionDigest,
			ConnectionVersion: item.ConnectionVersion, GrantVersion: item.GrantVersion,
		})
	}
	return response, nil
}
