package grpc

import (
	"context"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type managedDraftRequest interface {
	GetConfigurationRef() string
	GetProjectRef() string
	GetName() string
	GetContentFormat() string
	GetContent() string
}

type managedRevisionRequest interface {
	GetConfigurationRef() string
	GetRevisionRef() string
}

type managedRebindRequest interface {
	GetConfigurationRef() string
	GetRevisionRef() string
	GetImpactDigest() string
	GetConsumers() []*controlplanev1.ManagedConfigurationConsumer
}

func managedDraftInput(request managedDraftRequest) command.ManagedConfigurationInput {
	return command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), ProjectRef: request.GetProjectRef(),
		Name: request.GetName(), ContentFormat: request.GetContentFormat(), Content: request.GetContent()}
}

func managedRevisionInput(request managedRevisionRequest) command.ManagedConfigurationInput {
	return command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef()}
}

func managedRebindInput(request managedRebindRequest) command.ManagedConfigurationInput {
	result := command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef(), ImpactDigest: request.GetImpactDigest()}
	for _, item := range request.GetConsumers() {
		result.Consumers = append(result.Consumers, entity.ManagedConfigurationConsumer{Kind: item.GetKind(), Ref: item.GetRef(), RevisionRef: item.GetRevisionRef(), Version: item.GetVersion()})
	}
	return result
}

func (server *Server) managedMutation(ctx context.Context, method string, kind command.Kind, mutation *controlplanev1.MutationContext, input command.ManagedConfigurationInput) (*controlplanev1.ManagedConfigurationSet, *controlplanev1.ManagedConfigurationRevision, error) {
	result, err := execute(ctx, server.service, method, kind, mutation, input)
	if err != nil {
		return nil, nil, err
	}
	return castManagedConfiguration(result.ManagedConfiguration), castManagedRevision(result.ManagedRevision), nil
}

func (server *Server) CreatePromptTemplateDraft(ctx context.Context, request *controlplanev1.CreatePromptTemplateDraftRequest) (*controlplanev1.CreatePromptTemplateDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CreatePromptTemplateDraft_FullMethodName, command.CreatePromptTemplateDraft, request.GetMutation(), managedDraftInput(request))
	return &controlplanev1.CreatePromptTemplateDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) ValidatePromptTemplateDraft(ctx context.Context, request *controlplanev1.ValidatePromptTemplateDraftRequest) (*controlplanev1.ValidatePromptTemplateDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_ValidatePromptTemplateDraft_FullMethodName, command.ValidatePromptTemplateDraft, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.ValidatePromptTemplateDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) PublishPromptTemplateDraft(ctx context.Context, request *controlplanev1.PublishPromptTemplateDraftRequest) (*controlplanev1.PublishPromptTemplateDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_PublishPromptTemplateDraft_FullMethodName, command.PublishPromptTemplateDraft, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.PublishPromptTemplateDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) RebindPromptTemplateConsumers(ctx context.Context, request *controlplanev1.RebindPromptTemplateConsumersRequest) (*controlplanev1.RebindPromptTemplateConsumersResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_RebindPromptTemplateConsumers_FullMethodName, command.RebindPromptTemplate, request.GetMutation(), managedRebindInput(request))
	return &controlplanev1.RebindPromptTemplateConsumersResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) CreateRoleImageRevisionDraft(ctx context.Context, request *controlplanev1.CreateRoleImageRevisionDraftRequest) (*controlplanev1.CreateRoleImageRevisionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CreateRoleImageRevisionDraft_FullMethodName, command.CreateRoleImageRevisionDraft, request.GetMutation(), managedDraftInput(request))
	return &controlplanev1.CreateRoleImageRevisionDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) ValidateRoleImageRevisionDraft(ctx context.Context, request *controlplanev1.ValidateRoleImageRevisionDraftRequest) (*controlplanev1.ValidateRoleImageRevisionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_ValidateRoleImageRevisionDraft_FullMethodName, command.ValidateRoleImageRevision, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.ValidateRoleImageRevisionDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) PublishRoleImageRevisionDraft(ctx context.Context, request *controlplanev1.PublishRoleImageRevisionDraftRequest) (*controlplanev1.PublishRoleImageRevisionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_PublishRoleImageRevisionDraft_FullMethodName, command.PublishRoleImageRevision, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.PublishRoleImageRevisionDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) RebindRoleImageConsumers(ctx context.Context, request *controlplanev1.RebindRoleImageConsumersRequest) (*controlplanev1.RebindRoleImageConsumersResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_RebindRoleImageConsumers_FullMethodName, command.RebindRoleImage, request.GetMutation(), managedRebindInput(request))
	return &controlplanev1.RebindRoleImageConsumersResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) CreateIntegrationDefinitionDraft(ctx context.Context, request *controlplanev1.CreateIntegrationDefinitionDraftRequest) (*controlplanev1.CreateIntegrationDefinitionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CreateIntegrationDefinitionDraft_FullMethodName, command.CreateIntegrationDefinition, request.GetMutation(), managedDraftInput(request))
	return &controlplanev1.CreateIntegrationDefinitionDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) ValidateIntegrationDefinitionDraft(ctx context.Context, request *controlplanev1.ValidateIntegrationDefinitionDraftRequest) (*controlplanev1.ValidateIntegrationDefinitionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_ValidateIntegrationDefinitionDraft_FullMethodName, command.ValidateIntegrationDefinition, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.ValidateIntegrationDefinitionDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) PublishIntegrationDefinitionDraft(ctx context.Context, request *controlplanev1.PublishIntegrationDefinitionDraftRequest) (*controlplanev1.PublishIntegrationDefinitionDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_PublishIntegrationDefinitionDraft_FullMethodName, command.PublishIntegrationDefinition, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.PublishIntegrationDefinitionDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) RebindIntegrationDefinitionConsumers(ctx context.Context, request *controlplanev1.RebindIntegrationDefinitionConsumersRequest) (*controlplanev1.RebindIntegrationDefinitionConsumersResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_RebindIntegrationDefinitionConsumers_FullMethodName, command.RebindIntegrationDefinition, request.GetMutation(), managedRebindInput(request))
	return &controlplanev1.RebindIntegrationDefinitionConsumersResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) CreateSystemSTTConfigurationDraft(ctx context.Context, request *controlplanev1.CreateSystemSTTConfigurationDraftRequest) (*controlplanev1.CreateSystemSTTConfigurationDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CreateSystemSTTConfigurationDraft_FullMethodName, command.CreateSystemSTTDraft, request.GetMutation(), managedDraftInput(request))
	return &controlplanev1.CreateSystemSTTConfigurationDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) ValidateSystemSTTConfigurationDraft(ctx context.Context, request *controlplanev1.ValidateSystemSTTConfigurationDraftRequest) (*controlplanev1.ValidateSystemSTTConfigurationDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_ValidateSystemSTTConfigurationDraft_FullMethodName, command.ValidateSystemSTTDraft, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.ValidateSystemSTTConfigurationDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) PublishSystemSTTConfigurationDraft(ctx context.Context, request *controlplanev1.PublishSystemSTTConfigurationDraftRequest) (*controlplanev1.PublishSystemSTTConfigurationDraftResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_PublishSystemSTTConfigurationDraft_FullMethodName, command.PublishSystemSTTDraft, request.GetMutation(), managedRevisionInput(request))
	return &controlplanev1.PublishSystemSTTConfigurationDraftResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) RebindSystemSTTConsumers(ctx context.Context, request *controlplanev1.RebindSystemSTTConsumersRequest) (*controlplanev1.RebindSystemSTTConsumersResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_RebindSystemSTTConsumers_FullMethodName, command.RebindSystemSTT, request.GetMutation(), managedRebindInput(request))
	return &controlplanev1.RebindSystemSTTConsumersResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) DetachGitManagedConfiguration(ctx context.Context, request *controlplanev1.DetachGitManagedConfigurationRequest) (*controlplanev1.DetachGitManagedConfigurationResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_DetachGitManagedConfiguration_FullMethodName, command.DetachGitManagedConfiguration, request.GetMutation(), command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef()})
	return &controlplanev1.DetachGitManagedConfigurationResponse{Configuration: configuration, Revision: revision}, err
}
func (server *Server) CopyGitManagedConfiguration(ctx context.Context, request *controlplanev1.CopyGitManagedConfigurationRequest) (*controlplanev1.CopyGitManagedConfigurationResponse, error) {
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_CopyGitManagedConfiguration_FullMethodName, command.CopyGitManagedConfiguration, request.GetMutation(), command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), Name: request.GetName()})
	return &controlplanev1.CopyGitManagedConfigurationResponse{Configuration: configuration, Revision: revision}, err
}

func (server *Server) ListManagedConfigurations(ctx context.Context, request *controlplanev1.ListManagedConfigurationsRequest) (*controlplanev1.ListManagedConfigurationsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListManagedConfigurations_FullMethodName)
	if err != nil {
		return nil, err
	}
	kind := ""
	if request.GetKind() != controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_UNSPECIFIED {
		name, known := controlplanev1.ManagedConfigurationKind_name[int32(request.GetKind())]
		if !known {
			return nil, status.Error(codes.InvalidArgument, "invalid configuration kind")
		}
		kind = strings.TrimPrefix(name, "MANAGED_CONFIGURATION_KIND_")
	}
	items, total, next, err := server.service.ListManagedConfigurations(ctx, p, query.Filter{
		ProjectRef: request.GetProjectRef(), Category: kind, Query: request.GetQuery(), Page: page(request.GetPage()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListManagedConfigurationsResponse{Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Configurations = append(response.Configurations, castManagedConfiguration(&item))
	}
	return response, nil
}

func (server *Server) ListManagedConfigurationHistory(ctx context.Context, request *controlplanev1.ListManagedConfigurationHistoryRequest) (*controlplanev1.ListManagedConfigurationHistoryResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListManagedConfigurationHistory_FullMethodName)
	if err != nil {
		return nil, err
	}
	configuration, revisions, total, next, err := server.service.ListManagedConfigurationHistory(ctx, p, request.GetConfigurationRef(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListManagedConfigurationHistoryResponse{Configuration: castManagedConfiguration(&configuration), Total: total, Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for index := range revisions {
		response.Revisions = append(response.Revisions, castManagedRevision(&revisions[index]))
	}
	return response, nil
}

func (server *Server) GetManagedConfigurationImpact(ctx context.Context, request *controlplanev1.GetManagedConfigurationImpactRequest) (*controlplanev1.GetManagedConfigurationImpactResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetManagedConfigurationImpact_FullMethodName)
	if err != nil {
		return nil, err
	}
	impact, err := server.service.GetManagedConfigurationImpact(ctx, p, request.GetConfigurationRef(), request.GetRevisionRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetManagedConfigurationImpactResponse{Impact: castManagedImpact(impact)}, nil
}

func (server *Server) GetRuntimeEnvironmentRoleImageConfiguration(ctx context.Context, request *controlplanev1.GetRuntimeEnvironmentRoleImageConfigurationRequest) (*controlplanev1.GetRuntimeEnvironmentRoleImageConfigurationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_GetRuntimeEnvironmentRoleImageConfiguration_FullMethodName)
	if err != nil {
		return nil, err
	}
	binding, err := server.service.GetEffectiveManagedConfiguration(ctx, p, "ROLE_IMAGE", "RUNTIME_ENVIRONMENT", request.GetEnvironmentRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetRuntimeEnvironmentRoleImageConfigurationResponse{Binding: castManagedBinding(binding)}, nil
}

func (server *Server) GetIntegrationConnectionDefinitionConfiguration(ctx context.Context, request *controlplanev1.GetIntegrationConnectionDefinitionConfigurationRequest) (*controlplanev1.GetIntegrationConnectionDefinitionConfigurationResponse, error) {
	p, err := principal(ctx, controlplanev1.RuntimeWorkService_GetIntegrationConnectionDefinitionConfiguration_FullMethodName)
	if err != nil {
		return nil, err
	}
	binding, err := server.service.GetEffectiveManagedConfiguration(ctx, p, "INTEGRATION_DEFINITION", "INTEGRATION_CONNECTION", request.GetConnectionRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetIntegrationConnectionDefinitionConfigurationResponse{Binding: castManagedBinding(binding)}, nil
}

func (server *Server) GetSystemSTTConfiguration(ctx context.Context, _ *controlplanev1.GetSystemSTTConfigurationRequest) (*controlplanev1.GetSystemSTTConfigurationResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetSystemSTTConfiguration_FullMethodName)
	if err != nil {
		return nil, err
	}
	configuration, err := server.service.GetSystemSTTConfiguration(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetSystemSTTConfigurationResponse{Configuration: &controlplanev1.SystemSTTConfiguration{
		ConfigurationRef: configuration.ConfigurationRef, RevisionRef: configuration.RevisionRef, Revision: configuration.Revision,
		Digest: configuration.Digest, ProviderAccountRef: configuration.ProviderAccountRef, Model: configuration.Model,
		Language: configuration.Language, PermissionKey: configuration.PermissionKey, Ready: configuration.Ready,
		ReadinessBlockers: append([]string(nil), configuration.ReadinessBlockers...), ProviderCredentialGeneration: configuration.ProviderCredentialGeneration,
		Enabled: configuration.Enabled, MaximumAudioBytes: configuration.MaximumAudioBytes,
		MaximumAudioDurationMilliseconds: configuration.MaximumAudioDurationMilliseconds, ProviderTimeoutMilliseconds: configuration.ProviderTimeoutMilliseconds,
		Parameters: &controlplanev1.SystemSTTParameters{Languages: append([]string(nil), configuration.Parameters.Languages...),
			Keywords: append([]string(nil), configuration.Parameters.Keywords...), Prompt: configuration.Parameters.Prompt,
			Temperature: configuration.Parameters.Temperature, ChunkingStrategy: configuration.Parameters.ChunkingStrategy, Stream: configuration.Parameters.Stream},
	}}, nil
}

func castManagedRevision(value *entity.ManagedConfigurationRevision) *controlplanev1.ManagedConfigurationRevision {
	if value == nil {
		return nil
	}
	return &controlplanev1.ManagedConfigurationRevision{Ref: value.Ref, Revision: value.Revision,
		State:         controlplanev1.ManagedConfigurationState(controlplanev1.ManagedConfigurationState_value["MANAGED_CONFIGURATION_STATE_"+value.State]),
		ContentFormat: value.ContentFormat, Content: value.Content, Digest: value.Digest,
		ValidationDiagnostics: append([]string(nil), value.ValidationDiagnostics...), ParentRevisionRef: value.ParentRevisionRef,
		CreatedAt: timestamp(value.CreatedAt), ValidatedAt: optionalTimestamp(value.ValidatedAt), PublishedAt: optionalTimestamp(value.PublishedAt)}
}

func castManagedConfiguration(value *entity.ManagedConfigurationSet) *controlplanev1.ManagedConfigurationSet {
	if value == nil {
		return nil
	}
	return &controlplanev1.ManagedConfigurationSet{Ref: value.Ref, Version: value.Version, ProjectRef: value.ProjectRef,
		Kind: controlplanev1.ManagedConfigurationKind(controlplanev1.ManagedConfigurationKind_value["MANAGED_CONFIGURATION_KIND_"+value.Kind]),
		Name: value.Name, ManagedBy: controlplanev1.ManagedConfigurationOwner(controlplanev1.ManagedConfigurationOwner_value["MANAGED_CONFIGURATION_OWNER_"+value.ManagedBy]),
		Source: value.Source, SourceRevision: value.SourceRevision, CurrentRevision: castManagedRevision(value.CurrentRevision), UpdatedAt: timestamp(value.UpdatedAt)}
}

func castManagedConsumer(value entity.ManagedConfigurationConsumer) *controlplanev1.ManagedConfigurationConsumer {
	return &controlplanev1.ManagedConfigurationConsumer{Kind: value.Kind, Ref: value.Ref, RevisionRef: value.RevisionRef, Version: value.Version}
}

func castManagedBinding(value entity.ManagedConfigurationBindingSnapshot) *controlplanev1.ManagedConfigurationBindingSnapshot {
	return &controlplanev1.ManagedConfigurationBindingSnapshot{
		BindingRef: value.Ref, BindingVersion: value.Version, ConsumerKind: value.ConsumerKind, ConsumerRef: value.ConsumerRef,
		Configuration: castManagedConfiguration(&value.Configuration), Revision: castManagedRevision(&value.Revision),
	}
}

func castManagedImpact(value entity.ManagedConfigurationImpact) *controlplanev1.ManagedConfigurationImpact {
	result := &controlplanev1.ManagedConfigurationImpact{ConfigurationRef: value.ConfigurationRef, TargetRevisionRef: value.TargetRevisionRef, Digest: value.Digest}
	for _, item := range value.Consumers {
		result.Consumers = append(result.Consumers, castManagedConsumer(item))
	}
	return result
}
