package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetAgentRuntimeConfiguration reads the authorized agent runtime view under the exact method
// principal and casts the owner result without rebuilding runtime state from request fields.
// GetAgentRuntimeConfiguration читает разрешённое agent runtime view под principal точного метода
// и преобразует owner result без восстановления runtime state из request fields.
func (server *Server) GetAgentRuntimeConfiguration(ctx context.Context, request *controlplanev1.GetAgentRuntimeConfigurationRequest) (*controlplanev1.GetAgentRuntimeConfigurationResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetAgentRuntimeConfiguration_FullMethodName, func(p value.Principal) (*controlplanev1.GetAgentRuntimeConfigurationResponse, error) {
		item, err := server.service.GetAgentRuntimeConfiguration(ctx, p, request.GetAgentRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetAgentRuntimeConfigurationResponse{RuntimeConfiguration: castRuntimeConfigurationView(item)}, nil
	})
}

// ListAgentRuntimeConfigurationVersions passes the agent locator and page to the owner, preserving
// version order and next cursor while using domain-to-wire casters.
// ListAgentRuntimeConfigurationVersions передаёт agent locator и page владельцу, сохраняя порядок
// versions и next cursor через domain-to-wire casters.
func (server *Server) ListAgentRuntimeConfigurationVersions(ctx context.Context, request *controlplanev1.ListAgentRuntimeConfigurationVersionsRequest) (*controlplanev1.ListAgentRuntimeConfigurationVersionsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListAgentRuntimeConfigurationVersions_FullMethodName, func(p value.Principal) (*controlplanev1.ListAgentRuntimeConfigurationVersionsResponse, error) {
		items, next, err := server.service.ListAgentRuntimeConfigurations(ctx, p, query.Filter{ResourceRef: request.GetAgentRef(), Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListAgentRuntimeConfigurationVersionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		response.Configurations = castResponseItems(items, castAgentRuntimeConfiguration)
		return response, nil
	})
}

// ListRuntimeEnvironmentSets reads owner-filtered runtime environments using project/search/page
// locators under the verified query principal.
// ListRuntimeEnvironmentSets читает отфильтрованные владельцем runtime environments по project,
// search и page locators под проверенным query principal.
func (server *Server) ListRuntimeEnvironmentSets(ctx context.Context, request *controlplanev1.ListRuntimeEnvironmentSetsRequest) (*controlplanev1.ListRuntimeEnvironmentSetsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListRuntimeEnvironmentSets_FullMethodName, func(p value.Principal) (*controlplanev1.ListRuntimeEnvironmentSetsResponse, error) {
		items, next, err := server.service.ListRuntimeEnvironments(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListRuntimeEnvironmentSetsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		response.Environments = castResponseItems(items, castRuntimeEnvironment)
		return response, nil
	})
}

// GetRuntimeEnvironmentSet reads one environment through the exact query principal and owner scope
// before serialization.
// GetRuntimeEnvironmentSet читает одно environment через principal точного query и owner scope до
// сериализации.
func (server *Server) GetRuntimeEnvironmentSet(ctx context.Context, request *controlplanev1.GetRuntimeEnvironmentSetRequest) (*controlplanev1.GetRuntimeEnvironmentSetResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetRuntimeEnvironmentSet_FullMethodName, func(p value.Principal) (*controlplanev1.GetRuntimeEnvironmentSetResponse, error) {
		item, err := server.service.GetRuntimeEnvironment(ctx, p, request.GetEnvironmentRef())
		if err != nil {
			return nil, err
		}
		return &controlplanev1.GetRuntimeEnvironmentSetResponse{Environment: castRuntimeEnvironment(item)}, nil
	})
}

// ListRuntimeEnvironmentVersions reads the owner environment-version page and preserves its next
// cursor without assigning a current version from the request.
// ListRuntimeEnvironmentVersions читает owner environment-version page и сохраняет next cursor без
// назначения current version из запроса.
func (server *Server) ListRuntimeEnvironmentVersions(ctx context.Context, request *controlplanev1.ListRuntimeEnvironmentVersionsRequest) (*controlplanev1.ListRuntimeEnvironmentVersionsResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListRuntimeEnvironmentVersions_FullMethodName, func(p value.Principal) (*controlplanev1.ListRuntimeEnvironmentVersionsResponse, error) {
		items, next, err := server.service.ListRuntimeEnvironmentVersions(ctx, p, query.Filter{ResourceRef: request.GetEnvironmentRef(), Page: page(request.GetPage())})
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListRuntimeEnvironmentVersionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		response.Versions = castResponseItems(items, castRuntimeEnvironmentVersion)
		return response, nil
	})
}

// ListTemplateVariables passes optional exact preview/context pins to owner context resolution,
// then projects variables and rejects unknown or availability-inconsistent reason enums without
// partial output.
// ListTemplateVariables передаёт необязательные точные preview и context pins owner context
// resolution, затем проецирует variables и отклоняет неизвестные либо несовместимые с availability
// reason enums без частичного вывода.
func (server *Server) ListTemplateVariables(ctx context.Context, request *controlplanev1.ListTemplateVariablesRequest) (*controlplanev1.ListTemplateVariablesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListTemplateVariables_FullMethodName, func(p value.Principal) (*controlplanev1.ListTemplateVariablesResponse, error) {
		filter := query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), SourceKind: request.GetSource(), Page: page(request.GetPage())}
		if request.GetAgentRef() != "" || request.GetRuntimeRevisionRef() != "" || request.GetTargetKind() != "" || request.GetTargetRef() != "" || request.GetContext() != nil || request.GetExpectedContextDigest() != "" {
			filter.TemplateContext = &query.TemplateVariableContext{AgentRef: request.GetAgentRef(), RuntimeRevisionRef: request.GetRuntimeRevisionRef(),
				TargetKind: request.GetTargetKind(), TargetRef: request.GetTargetRef(), ExpectedContextDigest: request.GetExpectedContextDigest(), Preview: castPromptPreviewContext(request.GetContext())}
		}
		result, err := server.service.ListPromptContextVariables(ctx, p, filter)
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListTemplateVariablesResponse{Total: result.Total, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken}, ContextPin: castPromptContextPin(result.ContextPin)}
		for _, item := range result.Variables {
			reason, reasonErr := castTemplateAvailabilityReason(item.Reason, item.Available)
			if reasonErr != nil {
				return nil, reasonErr
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
	})
}

// castTemplateAvailabilityReason requires a known nonzero reason enum whose AVAILABLE value agrees
// exactly with the availability flag.
// castTemplateAvailabilityReason требует известный ненулевой reason enum с точным соответствием
// AVAILABLE значению availability flag.
func castTemplateAvailabilityReason(value string, available bool) (controlplanev1.TemplateVariableAvailabilityReason, error) {
	reason, ok := controlplanev1.TemplateVariableAvailabilityReason_value["TEMPLATE_VARIABLE_AVAILABILITY_REASON_"+value]
	if !ok || reason == 0 || available != (value == "AVAILABLE") {
		return 0, errs.ErrUnavailable
	}
	return controlplanev1.TemplateVariableAvailabilityReason(reason), nil
}

// PublishAgentRuntimeConfiguration copies provider account weight/catalog/digest/reasoning pins
// and runtime policy fields into the specialized owner publication command.
// PublishAgentRuntimeConfiguration копирует provider account weight, catalog, digest и reasoning
// pins и runtime policy fields в специализированную owner publication command.
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

// CreateConfigOverlayDraft passes complete overlay content and agent locator to the owner draft
// command without applying runtime configuration directly.
// CreateConfigOverlayDraft передаёт полный overlay content и agent locator owner draft command без
// прямого применения runtime configuration.
func (server *Server) CreateConfigOverlayDraft(ctx context.Context, request *controlplanev1.CreateConfigOverlayDraftRequest) (*controlplanev1.CreateConfigOverlayDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateConfigOverlayDraft_FullMethodName,
		command.CreateConfigOverlayDraft, request.GetMutation(), command.ConfigOverlayInput{AgentRef: request.GetAgentRef(), Content: request.GetContent()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateConfigOverlayDraftResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}

// ValidateConfigOverlayDraft delegates overlay validation for the agent through the exact
// specialized owner command.
// ValidateConfigOverlayDraft передаёт overlay validation агента точной специализированной owner
// command.
func (server *Server) ValidateConfigOverlayDraft(ctx context.Context, request *controlplanev1.ValidateConfigOverlayDraftRequest) (*controlplanev1.ValidateConfigOverlayDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ValidateConfigOverlayDraft_FullMethodName,
		command.ValidateConfigOverlayDraft, request.GetMutation(), command.ConfigOverlayInput{AgentRef: request.GetAgentRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ValidateConfigOverlayDraftResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}

// PublishConfigOverlayDraft delegates overlay publication to the owner and returns its resulting
// runtime configuration view.
// PublishConfigOverlayDraft передаёт overlay publication владельцу и возвращает итоговое runtime
// configuration view.
func (server *Server) PublishConfigOverlayDraft(ctx context.Context, request *controlplanev1.PublishConfigOverlayDraftRequest) (*controlplanev1.PublishConfigOverlayDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishConfigOverlayDraft_FullMethodName,
		command.PublishConfigOverlayDraft, request.GetMutation(), command.ConfigOverlayInput{AgentRef: request.GetAgentRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PublishConfigOverlayDraftResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}

// RollbackConfigOverlay passes the published overlay locator to the dedicated owner rollback
// command rather than accepting replacement runtime content.
// RollbackConfigOverlay передаёт published overlay locator отдельной owner rollback command вместо
// приёма replacement runtime content.
func (server *Server) RollbackConfigOverlay(ctx context.Context, request *controlplanev1.RollbackConfigOverlayRequest) (*controlplanev1.RollbackConfigOverlayResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RollbackConfigOverlay_FullMethodName,
		command.RollbackConfigOverlay, request.GetMutation(), command.ConfigOverlayInput{AgentRef: request.GetAgentRef(), PublishedOverlayRef: request.GetPublishedOverlayRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RollbackConfigOverlayResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}

// domainEnvironment copies public environment values, logical Secret revision bindings and tool
// metadata into nonnil domain slices without resolving credential values.
// domainEnvironment копирует public environment values, логические Secret revision bindings и tool
// metadata в nonnil доменные slices без разрешения credential values.
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

// CreateRuntimeEnvironmentSet converts public values/secret descriptors/tools and validates the
// typed policy before dispatching a PROJECT-scoped owner create command.
// CreateRuntimeEnvironmentSet преобразует public values, secret descriptors и tools и проверяет
// typed policy до вызова PROJECT-scoped owner create command.
func (server *Server) CreateRuntimeEnvironmentSet(ctx context.Context, request *controlplanev1.CreateRuntimeEnvironmentSetRequest) (*controlplanev1.CreateRuntimeEnvironmentSetResponse, error) {
	values, secrets, tools := domainEnvironment(request.GetValues(), request.GetSecretBindings(), request.GetTools())
	policy, err := domainRuntimeEnvironmentPolicy(request.GetPolicy())
	if err != nil {
		return nil, transportError(errs.ErrInvalid)
	}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateRuntimeEnvironmentSet_FullMethodName,
		command.CreateRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentInput{ScopeKind: "PROJECT", ProjectRef: request.GetProjectRef(), Name: request.GetName(), Description: request.GetDescription(), ImageArtifactRef: request.GetImageArtifactRef(), Values: values, SecretBindings: secrets, Tools: tools, Policy: policy})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateRuntimeEnvironmentSetResponse{Environment: castRuntimeEnvironment(*result.RuntimeEnvironment)}, nil
}

// PublishRuntimeEnvironmentVersion converts the requested full environment state and validated
// policy for the owner version-publication command with mutation context.
// PublishRuntimeEnvironmentVersion преобразует полный запрошенный environment state и проверенную
// policy для owner version-publication command с mutation context.
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

// domainRuntimeEnvironmentPolicy requires resources, preserves optional workspace limits and
// copies volumes/network/web/Kubernetes settings through the canonical runtime policy validator.
// domainRuntimeEnvironmentPolicy требует resources, сохраняет необязательные workspace limits и
// передаёт volumes, network, web и Kubernetes настройки каноническому runtime policy validator.
func domainRuntimeEnvironmentPolicy(input *controlplanev1.RuntimeEnvironmentPolicyInput) (runtimecontract.RuntimeEnvironmentPolicy, error) {
	if input == nil || input.GetResources() == nil {
		return runtimecontract.RuntimeEnvironmentPolicy{}, errs.ErrInvalid
	}
	resources := input.GetResources()
	var workspaceLimits *runtimecontract.RuntimeWorkspaceLimits
	if limits := resources.GetWorkspaceLimits(); limits != nil {
		workspaceLimits = &runtimecontract.RuntimeWorkspaceLimits{MaxBytes: limits.GetMaxBytes(), MaxFiles: limits.GetMaxFiles()}
	}
	volumes := make([]runtimecontract.RuntimeVolume, 0, len(input.GetVolumes()))
	for _, volume := range input.GetVolumes() {
		volumes = append(volumes, runtimecontract.RuntimeVolume{Name: volume.GetName(), Kind: domainRuntimeVolumeKind(volume.GetKind()), SizeMiB: volume.GetSizeMib()})
	}
	destinations := make([]string, 0, len(input.GetNetworkDestinations()))
	for _, destination := range input.GetNetworkDestinations() {
		destinations = append(destinations, domainRuntimeNetworkDestination(destination))
	}
	webAccess := runtimecontract.RuntimeWebAccess{Mode: domainRuntimeWebAccessMode(input.GetWebAccess().GetMode())}
	for _, rule := range input.GetWebAccess().GetRules() {
		webAccess.Rules = append(webAccess.Rules, runtimecontract.RuntimeWebAccessRule{
			DomainPattern: rule.GetDomainPattern(), Protocol: rule.GetProtocol(), Port: rule.GetPort(), HTTPMethods: append([]string(nil), rule.GetHttpMethods()...),
		})
	}
	return runtimecontract.RuntimeEnvironmentPolicyFromInput(runtimecontract.RuntimeEnvironmentPolicyInput{
		Resources: runtimecontract.RuntimeResourcePolicy{
			WorkspaceLimits: workspaceLimits,
			CPURequestMilli: resources.GetCpuRequestMilli(), CPULimitMilli: resources.GetCpuLimitMilli(),
			MemoryRequestMiB: resources.GetMemoryRequestMib(), MemoryLimitMiB: resources.GetMemoryLimitMib(),
			EphemeralStorageRequestMiB: resources.GetEphemeralStorageRequestMib(),
			EphemeralStorageLimitMiB:   resources.GetEphemeralStorageLimitMib(),
		}, Volumes: volumes, NetworkDestinations: destinations, WebAccess: webAccess,
		KubernetesAccess: domainRuntimeKubernetesAccessKind(input.GetKubernetesAccess()),
	})
}

// domainRuntimeWebAccessMode maps only the closed web access modes to domain names and leaves
// unsupported values empty for canonical policy rejection.
// domainRuntimeWebAccessMode преобразует только закрытые web access modes в доменные имена и
// оставляет неподдержанные значения пустыми для canonical policy rejection.
func domainRuntimeWebAccessMode(value controlplanev1.RuntimeWebAccessMode) string {
	switch value {
	case controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_NONE:
		return runtimecontract.RuntimeWebAccessNone
	case controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_ALLOWLIST_READ_ONLY:
		return runtimecontract.RuntimeWebAccessAllowlistReadOnly
	case controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_ALLOWLIST_FULL:
		return runtimecontract.RuntimeWebAccessAllowlistFull
	case controlplanev1.RuntimeWebAccessMode_RUNTIME_WEB_ACCESS_MODE_FULL_PUBLIC:
		return runtimecontract.RuntimeWebAccessFullPublic
	default:
		return ""
	}
}

// domainRuntimeVolumeKind maps only ephemeral disk/memory volume kinds and returns an empty value
// for unsupported variants.
// domainRuntimeVolumeKind преобразует только ephemeral disk и memory volume kinds и возвращает
// пустое значение для неподдержанного варианта.
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

// domainRuntimeNetworkDestination maps only DNS, runtime-callback and provider-proxy destinations
// without accepting arbitrary destination names.
// domainRuntimeNetworkDestination преобразует только DNS, runtime-callback и provider-proxy
// destinations без приёма произвольных destination names.
func domainRuntimeNetworkDestination(value controlplanev1.RuntimeNetworkDestination) string {
	switch value {
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS:
		return runtimecontract.RuntimeEgressDNS
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK:
		return runtimecontract.RuntimeEgressRuntimeCallback
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY:
		return runtimecontract.RuntimeEgressProviderProxy
	default:
		return ""
	}
}

// domainRuntimeKubernetesAccessKind maps only the supported NONE Kubernetes access kind, leaving
// every other enum empty for policy validation.
// domainRuntimeKubernetesAccessKind преобразует только поддержанный NONE Kubernetes access kind,
// оставляя остальные enums пустыми для policy validation.
func domainRuntimeKubernetesAccessKind(value controlplanev1.RuntimeKubernetesAccessKind) string {
	switch value {
	case controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE:
		return runtimecontract.RuntimeKubernetesAccessNone
	default:
		return ""
	}
}

// RollbackRuntimeEnvironment passes the environment and published-version locators to the
// specialized owner rollback command.
// RollbackRuntimeEnvironment передаёт environment и published-version locators специализированной
// owner rollback command.
func (server *Server) RollbackRuntimeEnvironment(ctx context.Context, request *controlplanev1.RollbackRuntimeEnvironmentRequest) (*controlplanev1.RollbackRuntimeEnvironmentResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RollbackRuntimeEnvironment_FullMethodName,
		command.RollbackRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentInput{Ref: request.GetEnvironmentRef(), PublishedVersionRef: request.GetPublishedVersionRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RollbackRuntimeEnvironmentResponse{Environment: castRuntimeEnvironment(*result.RuntimeEnvironment)}, nil
}

// BindAgentRuntimeEnvironment passes agent/environment/version locators to owner binding with
// mutation/version context, without granting authority from the version reference alone.
// BindAgentRuntimeEnvironment передаёт agent, environment и version locators owner binding с
// mutation и version context без выдачи полномочий одной ссылкой version.
func (server *Server) BindAgentRuntimeEnvironment(ctx context.Context, request *controlplanev1.BindAgentRuntimeEnvironmentRequest) (*controlplanev1.BindAgentRuntimeEnvironmentResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_BindAgentRuntimeEnvironment_FullMethodName,
		command.BindAgentRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentBindingInput{AgentRef: request.GetAgentRef(), EnvironmentRef: request.GetEnvironmentRef(), VersionRef: request.GetVersionRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.BindAgentRuntimeEnvironmentResponse{RuntimeConfiguration: castRuntimeConfigurationView(*result.RuntimeConfiguration)}, nil
}

// SetRuntimeEnvironmentEnabled passes the explicit enabled flag through the owner environment
// lifecycle command under the exact RPC principal.
// SetRuntimeEnvironmentEnabled передаёт явный enabled flag owner environment lifecycle command под
// principal точного RPC.
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

// DeleteRuntimeEnvironment passes the environment locator through the dedicated owner deletion
// command rather than deleting configuration resources in the transport.
// DeleteRuntimeEnvironment передаёт environment locator отдельной owner deletion command вместо
// удаления configuration resources в transport.
func (server *Server) DeleteRuntimeEnvironment(ctx context.Context, request *controlplanev1.DeleteRuntimeEnvironmentRequest) (*controlplanev1.DeleteRuntimeEnvironmentResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_DeleteRuntimeEnvironment_FullMethodName,
		command.DeleteRuntimeEnvironment, request.GetMutation(), command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: request.GetEnvironmentRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.DeleteRuntimeEnvironmentResponse{Environment: castRuntimeEnvironment(*result.RuntimeEnvironment)}, nil
}
