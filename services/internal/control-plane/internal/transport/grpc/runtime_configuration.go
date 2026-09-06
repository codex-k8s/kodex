package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
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
	filter := query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())}
	if request.GetAgentRef() != "" || request.GetRuntimeRevisionRef() != "" || request.GetTargetKind() != "" || request.GetTargetRef() != "" || request.GetContext() != nil || request.GetExpectedContextDigest() != "" {
		filter.TemplateContext = &query.TemplateVariableContext{AgentRef: request.GetAgentRef(), RuntimeRevisionRef: request.GetRuntimeRevisionRef(),
			TargetKind: request.GetTargetKind(), TargetRef: request.GetTargetRef(), ExpectedContextDigest: request.GetExpectedContextDigest(), Preview: castPromptPreviewContext(request.GetContext())}
	}
	result, err := server.service.ListPromptContextVariables(ctx, p, filter)
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListTemplateVariablesResponse{Total: result.Total, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken}, ContextPin: castPromptContextPin(result.ContextPin)}
	for _, item := range result.Variables {
		reason, reasonErr := castTemplateAvailabilityReason(item.Reason, item.Available)
		if reasonErr != nil {
			return nil, transportError(reasonErr)
		}
		variable := &controlplanev1.TemplateVariable{Name: item.Name, ValueType: item.Type,
			Description: item.Description, Example: item.Example, Source: item.Source, Collection: item.Collection,
			ItemValueType: item.ItemType, RangeExample: item.RangeExample, Available: item.Available,
			Reason: reason}
		for _, field := range item.ItemFields {
			variable.ItemFields = append(variable.ItemFields, &controlplanev1.TemplateVariableField{Name: field.Name, ValueType: field.Type, Description: field.Description})
		}
		response.Variables = append(response.Variables, variable)
	}
	return response, nil
}

func castTemplateAvailabilityReason(value string, available bool) (controlplanev1.TemplateVariableAvailabilityReason, error) {
	reason, ok := controlplanev1.TemplateVariableAvailabilityReason_value["TEMPLATE_VARIABLE_AVAILABILITY_REASON_"+value]
	if !ok || reason == 0 || available != (value == "AVAILABLE") {
		return 0, errs.ErrUnavailable
	}
	return controlplanev1.TemplateVariableAvailabilityReason(reason), nil
}

func (server *Server) PublishAgentRuntimeConfiguration(ctx context.Context, request *controlplanev1.PublishAgentRuntimeConfigurationRequest) (*controlplanev1.PublishAgentRuntimeConfigurationResponse, error) {
	accounts := make([]entity.ProviderAccountCandidate, 0, len(request.GetProviderAccounts()))
	for _, item := range request.GetProviderAccounts() {
		accounts = append(accounts, entity.ProviderAccountCandidate{AccountRef: item.GetAccountRef(), Weight: item.GetWeight(),
			CatalogRevision: item.GetCatalogRevision(), CatalogDigest: item.GetCatalogDigest(),
			ProviderDefinitionKey: item.GetProviderDefinitionKey(), DefaultReasoningEffort: item.GetDefaultReasoningEffort()})
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
		domainSecrets = append(domainSecrets, entity.RuntimeSecretBinding{Name: item.GetName(), SecretRef: item.GetSecretRef(), Revision: item.GetRevision()})
	}
	domainTools := make([]entity.RuntimeEnvironmentTool, 0, len(tools))
	for _, item := range tools {
		domainTools = append(domainTools, entity.RuntimeEnvironmentTool{Name: item.GetName(), Command: item.GetCommand(), Description: item.GetDescription(), UsageHint: item.GetUsageHint()})
	}
	return domainValues, domainSecrets, domainTools
}

func (server *Server) CreateRuntimeEnvironmentSet(ctx context.Context, request *controlplanev1.CreateRuntimeEnvironmentSetRequest) (*controlplanev1.CreateRuntimeEnvironmentSetResponse, error) {
	values, secrets, tools := domainEnvironment(request.GetValues(), request.GetSecretBindings(), request.GetTools())
	policy, err := domainRuntimeEnvironmentPolicy(request.GetPolicy())
	if err != nil {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateRuntimeEnvironmentSet_FullMethodName,
		command.CreateRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentInput{ProjectRef: request.GetProjectRef(), Name: request.GetName(), Description: request.GetDescription(), ImageArtifactRef: request.GetImageArtifactRef(), Values: values, SecretBindings: secrets, Tools: tools, Policy: policy})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateRuntimeEnvironmentSetResponse{Environment: castRuntimeEnvironment(*result.RuntimeEnvironment)}, nil
}
func (server *Server) PublishRuntimeEnvironmentVersion(ctx context.Context, request *controlplanev1.PublishRuntimeEnvironmentVersionRequest) (*controlplanev1.PublishRuntimeEnvironmentVersionResponse, error) {
	values, secrets, tools := domainEnvironment(request.GetValues(), request.GetSecretBindings(), request.GetTools())
	policy, err := domainRuntimeEnvironmentPolicy(request.GetPolicy())
	if err != nil {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishRuntimeEnvironmentVersion_FullMethodName,
		command.PublishRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentInput{Ref: request.GetEnvironmentRef(), Name: request.GetName(), Description: request.GetDescription(), ImageArtifactRef: request.GetImageArtifactRef(), Values: values, SecretBindings: secrets, Tools: tools, Policy: policy})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PublishRuntimeEnvironmentVersionResponse{Environment: castRuntimeEnvironment(*result.RuntimeEnvironment)}, nil
}

func domainRuntimeEnvironmentPolicy(input *controlplanev1.RuntimeEnvironmentPolicyInput) (runtimecontract.RuntimeEnvironmentPolicy, error) {
	if input == nil || input.GetResources() == nil {
		return runtimecontract.RuntimeEnvironmentPolicy{}, errs.ErrInvalid
	}
	resources := input.GetResources()
	volumes := make([]runtimecontract.RuntimeVolume, 0, len(input.GetVolumes()))
	for _, volume := range input.GetVolumes() {
		volumes = append(volumes, runtimecontract.RuntimeVolume{Name: volume.GetName(), Kind: domainRuntimeVolumeKind(volume.GetKind()), SizeMiB: volume.GetSizeMib()})
	}
	destinations := make([]string, 0, len(input.GetNetworkDestinations()))
	for _, destination := range input.GetNetworkDestinations() {
		destinations = append(destinations, domainRuntimeNetworkDestination(destination))
	}
	return runtimecontract.RuntimeEnvironmentPolicyFromInput(runtimecontract.RuntimeEnvironmentPolicyInput{
		Resources: runtimecontract.RuntimeResourcePolicy{
			CPURequestMilli: resources.GetCpuRequestMilli(), CPULimitMilli: resources.GetCpuLimitMilli(),
			MemoryRequestMiB: resources.GetMemoryRequestMib(), MemoryLimitMiB: resources.GetMemoryLimitMib(),
			EphemeralStorageRequestMiB: resources.GetEphemeralStorageRequestMib(),
			EphemeralStorageLimitMiB:   resources.GetEphemeralStorageLimitMib(),
		}, Volumes: volumes, NetworkDestinations: destinations,
		KubernetesAccess: domainRuntimeKubernetesAccessKind(input.GetKubernetesAccess()),
	})
}

func domainRuntimeVolumeKind(value controlplanev1.RuntimeVolumeKind) string {
	switch value {
	case controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK:
		return runtimecontract.RuntimeVolumeEphemeralDisk
	case controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_MEMORY:
		return runtimecontract.RuntimeVolumeEphemeralMemory
	default:
		return ""
	}
}

func domainRuntimeNetworkDestination(value controlplanev1.RuntimeNetworkDestination) string {
	switch value {
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS:
		return runtimecontract.RuntimeEgressDNS
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK:
		return runtimecontract.RuntimeEgressRuntimeCallback
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY:
		return runtimecontract.RuntimeEgressProviderProxy
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_KUBERNETES_API:
		return runtimecontract.RuntimeEgressKubernetesAPI
	default:
		return ""
	}
}

func domainRuntimeKubernetesAccessKind(value controlplanev1.RuntimeKubernetesAccessKind) string {
	switch value {
	case controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE:
		return runtimecontract.RuntimeKubernetesAccessNone
	case controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_READ_OWN_EXECUTION:
		return runtimecontract.RuntimeKubernetesAccessReadOwnExecution
	default:
		return ""
	}
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
		command.BindAgentRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentBindingInput{AgentRef: request.GetAgentRef(), EnvironmentRef: request.GetEnvironmentRef(), VersionRef: request.GetVersionRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.BindAgentRuntimeEnvironmentResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}

func (server *Server) SetRuntimeEnvironmentEnabled(ctx context.Context, request *controlplanev1.SetRuntimeEnvironmentEnabledRequest) (*controlplanev1.SetRuntimeEnvironmentEnabledResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_SetRuntimeEnvironmentEnabled_FullMethodName,
		command.SetRuntimeEnvironmentEnabled, request.GetMutation(), command.RuntimeEnvironmentLifecycleInput{
			EnvironmentRef: request.GetEnvironmentRef(), Enabled: request.GetEnabled(),
		})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.SetRuntimeEnvironmentEnabledResponse{Environment: castRuntimeEnvironment(*result.RuntimeEnvironment)}, nil
}

func (server *Server) DeleteRuntimeEnvironment(ctx context.Context, request *controlplanev1.DeleteRuntimeEnvironmentRequest) (*controlplanev1.DeleteRuntimeEnvironmentResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_DeleteRuntimeEnvironment_FullMethodName,
		command.DeleteRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: request.GetEnvironmentRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DeleteRuntimeEnvironmentResponse{Environment: castRuntimeEnvironment(*result.RuntimeEnvironment)}, nil
}
