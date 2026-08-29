package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) GetAgentRuntimeConfiguration(ctx context.Context, request *controlplanev1.GetAgentRuntimeConfigurationRequest) (*controlplanev1.GetAgentRuntimeConfigurationResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetAgentRuntimeConfiguration_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetAgentRuntimeConfiguration(ctx, p, request.GetAgentRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetAgentRuntimeConfigurationResponse{RuntimeConfiguration: castRuntimeConfigurationView(item)}, nil
}

func (server *Server) ListAgentRuntimeConfigurationVersions(ctx context.Context, request *controlplanev1.ListAgentRuntimeConfigurationVersionsRequest) (*controlplanev1.ListAgentRuntimeConfigurationVersionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListAgentRuntimeConfigurationVersions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListAgentRuntimeConfigurations(ctx, p, query.Filter{ResourceRef: request.GetAgentRef(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListAgentRuntimeConfigurationVersionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Configurations = append(response.Configurations, castAgentRuntimeConfiguration(item))
	}
	return response, nil
}

func (server *Server) ListRuntimeEnvironmentSets(ctx context.Context, request *controlplanev1.ListRuntimeEnvironmentSetsRequest) (*controlplanev1.ListRuntimeEnvironmentSetsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListRuntimeEnvironmentSets_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListRuntimeEnvironments(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRuntimeEnvironmentSetsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Environments = append(response.Environments, castRuntimeEnvironment(item))
	}
	return response, nil
}

func (server *Server) GetRuntimeEnvironmentSet(ctx context.Context, request *controlplanev1.GetRuntimeEnvironmentSetRequest) (*controlplanev1.GetRuntimeEnvironmentSetResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRuntimeEnvironmentSet_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetRuntimeEnvironment(ctx, p, request.GetEnvironmentRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetRuntimeEnvironmentSetResponse{Environment: castRuntimeEnvironment(item)}, nil
}

func (server *Server) ListRuntimeEnvironmentVersions(ctx context.Context, request *controlplanev1.ListRuntimeEnvironmentVersionsRequest) (*controlplanev1.ListRuntimeEnvironmentVersionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListRuntimeEnvironmentVersions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListRuntimeEnvironmentVersions(ctx, p, query.Filter{ResourceRef: request.GetEnvironmentRef(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRuntimeEnvironmentVersionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Versions = append(response.Versions, castRuntimeEnvironmentVersion(item))
	}
	return response, nil
}

func (server *Server) ListTemplateVariables(ctx context.Context, request *controlplanev1.ListTemplateVariablesRequest) (*controlplanev1.ListTemplateVariablesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListTemplateVariables_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListTemplateVariables(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListTemplateVariablesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Variables = append(response.Variables, &controlplanev1.TemplateVariable{Name: item.Name, ValueType: item.Type,
			Description: item.Description, Example: item.Example, Source: item.Source})
	}
	return response, nil
}

func (server *Server) PublishAgentRuntimeConfiguration(ctx context.Context, request *controlplanev1.PublishAgentRuntimeConfigurationRequest) (*controlplanev1.PublishAgentRuntimeConfigurationResponse, error) {
	accounts := make([]entity.ProviderAccountCandidate, 0, len(request.GetProviderAccounts()))
	for _, item := range request.GetProviderAccounts() {
		accounts = append(accounts, entity.ProviderAccountCandidate{AccountRef: item.GetAccountRef(), Weight: item.GetWeight()})
	}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishAgentRuntimeConfiguration_FullMethodName,
		command.PublishAgentRuntimeConfig, request.GetMutation(), command.AgentRuntimeConfigurationInput{AgentRef: request.GetAgentRef(),
			RuntimeProfileRef: request.GetRuntimeProfileRef(), Model: request.GetModel(), ProviderPolicyMode: request.GetProviderPolicyMode(), ProviderAccounts: accounts})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PublishAgentRuntimeConfigurationResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}

func (server *Server) CreateConfigOverlayDraft(ctx context.Context, request *controlplanev1.CreateConfigOverlayDraftRequest) (*controlplanev1.CreateConfigOverlayDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateConfigOverlayDraft_FullMethodName,
		command.CreateConfigOverlayDraft, request.GetMutation(), command.ConfigOverlayInput{AgentRef: request.GetAgentRef(), Content: request.GetContent()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateConfigOverlayDraftResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}
func (server *Server) ValidateConfigOverlayDraft(ctx context.Context, request *controlplanev1.ValidateConfigOverlayDraftRequest) (*controlplanev1.ValidateConfigOverlayDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ValidateConfigOverlayDraft_FullMethodName,
		command.ValidateConfigOverlayDraft, request.GetMutation(), command.ConfigOverlayInput{AgentRef: request.GetAgentRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ValidateConfigOverlayDraftResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}
func (server *Server) PublishConfigOverlayDraft(ctx context.Context, request *controlplanev1.PublishConfigOverlayDraftRequest) (*controlplanev1.PublishConfigOverlayDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishConfigOverlayDraft_FullMethodName,
		command.PublishConfigOverlayDraft, request.GetMutation(), command.ConfigOverlayInput{AgentRef: request.GetAgentRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PublishConfigOverlayDraftResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}
func (server *Server) RollbackConfigOverlay(ctx context.Context, request *controlplanev1.RollbackConfigOverlayRequest) (*controlplanev1.RollbackConfigOverlayResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RollbackConfigOverlay_FullMethodName,
		command.RollbackConfigOverlay, request.GetMutation(), command.ConfigOverlayInput{AgentRef: request.GetAgentRef(), PublishedOverlayRef: request.GetPublishedOverlayRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RollbackConfigOverlayResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}

func domainEnvironment(values []*controlplanev1.RuntimeEnvironmentValue, secrets []*controlplanev1.RuntimeSecretBinding, tools []*controlplanev1.RuntimeEnvironmentTool) ([]entity.RuntimeEnvironmentValue, []entity.RuntimeSecretBinding, []entity.RuntimeEnvironmentTool) {
	domainValues := make([]entity.RuntimeEnvironmentValue, 0, len(values))
	for _, item := range values {
		domainValues = append(domainValues, entity.RuntimeEnvironmentValue{Name: item.GetName(), Value: item.GetValue()})
	}
	domainSecrets := make([]entity.RuntimeSecretBinding, 0, len(secrets))
	for _, item := range secrets {
		domainSecrets = append(domainSecrets, entity.RuntimeSecretBinding{Name: item.GetName(), SecretRef: item.GetSecretRef()})
	}
	domainTools := make([]entity.RuntimeEnvironmentTool, 0, len(tools))
	for _, item := range tools {
		domainTools = append(domainTools, entity.RuntimeEnvironmentTool{Name: item.GetName(), Command: item.GetCommand(), Description: item.GetDescription(), UsageHint: item.GetUsageHint()})
	}
	return domainValues, domainSecrets, domainTools
}

func (server *Server) CreateRuntimeEnvironmentSet(ctx context.Context, request *controlplanev1.CreateRuntimeEnvironmentSetRequest) (*controlplanev1.CreateRuntimeEnvironmentSetResponse, error) {
	values, secrets, tools := domainEnvironment(request.GetValues(), request.GetSecretBindings(), request.GetTools())
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateRuntimeEnvironmentSet_FullMethodName,
		command.CreateRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentInput{ProjectRef: request.GetProjectRef(), Name: request.GetName(), Description: request.GetDescription(), ImageArtifactRef: request.GetImageArtifactRef(), Values: values, SecretBindings: secrets, Tools: tools})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateRuntimeEnvironmentSetResponse{Environment: castRuntimeEnvironment(*result.RuntimeEnvironment)}, nil
}
func (server *Server) PublishRuntimeEnvironmentVersion(ctx context.Context, request *controlplanev1.PublishRuntimeEnvironmentVersionRequest) (*controlplanev1.PublishRuntimeEnvironmentVersionResponse, error) {
	values, secrets, tools := domainEnvironment(request.GetValues(), request.GetSecretBindings(), request.GetTools())
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishRuntimeEnvironmentVersion_FullMethodName,
		command.PublishRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentInput{Ref: request.GetEnvironmentRef(), Name: request.GetName(), Description: request.GetDescription(), ImageArtifactRef: request.GetImageArtifactRef(), Values: values, SecretBindings: secrets, Tools: tools})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PublishRuntimeEnvironmentVersionResponse{Environment: castRuntimeEnvironment(*result.RuntimeEnvironment)}, nil
}
func (server *Server) RollbackRuntimeEnvironment(ctx context.Context, request *controlplanev1.RollbackRuntimeEnvironmentRequest) (*controlplanev1.RollbackRuntimeEnvironmentResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RollbackRuntimeEnvironment_FullMethodName,
		command.RollbackRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentInput{Ref: request.GetEnvironmentRef(), PublishedVersionRef: request.GetPublishedVersionRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RollbackRuntimeEnvironmentResponse{Environment: castRuntimeEnvironment(*result.RuntimeEnvironment)}, nil
}
func (server *Server) BindAgentRuntimeEnvironment(ctx context.Context, request *controlplanev1.BindAgentRuntimeEnvironmentRequest) (*controlplanev1.BindAgentRuntimeEnvironmentResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_BindAgentRuntimeEnvironment_FullMethodName,
		command.BindAgentRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentBindingInput{AgentRef: request.GetAgentRef(), EnvironmentRef: request.GetEnvironmentRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.BindAgentRuntimeEnvironmentResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}
