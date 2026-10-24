package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// GetRuntimeEnvironmentImpact reads affected consumers for the requested environment/version with
// verified principal and pagination, preserving target digest and version pins.
// GetRuntimeEnvironmentImpact читает затронутых потребителей заданной среды и версии с проверенным
// principal и пагинацией, сохраняя digest цели и привязки версий.
func (server *Server) GetRuntimeEnvironmentImpact(ctx context.Context, request *controlplanev1.GetRuntimeEnvironmentImpactRequest) (*controlplanev1.GetRuntimeEnvironmentImpactResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRuntimeEnvironmentImpact_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetRuntimeEnvironmentImpact(ctx, p, request.GetEnvironmentRef(), request.GetVersionRef(), request.GetQuery(),
		query.Page{Size: request.GetPage().GetPageSize(), Token: request.GetPage().GetPageToken()})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.GetRuntimeEnvironmentImpactResponse{EnvironmentRef: result.EnvironmentRef,
		EnvironmentVersion: result.EnvironmentVersion, TargetVersionRef: result.TargetVersionRef, TargetDigest: result.TargetDigest,
		Total: result.Total, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken}}
	for _, item := range result.Consumers {
		response.Consumers = append(response.Consumers, castRuntimeEnvironmentConsumer(item))
	}
	return response, nil
}

// RebindRuntimeEnvironment converts selected consumer pins before executing the dedicated rebind
// command and projects the owner-returned environment bindings.
// RebindRuntimeEnvironment преобразует привязки выбранных потребителей до отдельной команды
// перепривязки и возвращает привязки среды владельца.
func (server *Server) RebindRuntimeEnvironment(ctx context.Context, request *controlplanev1.RebindRuntimeEnvironmentRequest) (*controlplanev1.RebindRuntimeEnvironmentResponse, error) {
	input := command.RuntimeEnvironmentRebindInput{EnvironmentRef: request.GetEnvironmentRef(), VersionRef: request.GetVersionRef()}
	for _, item := range request.GetConsumers() {
		input.Consumers = append(input.Consumers, runtimeEnvironmentConsumerInput(item))
	}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RebindRuntimeEnvironment_FullMethodName,
		command.RebindRuntimeEnvironment, request.GetMutation(), input)
	if err != nil {
		return nil, err
	}
	response := &controlplanev1.RebindRuntimeEnvironmentResponse{}
	for _, item := range result.EnvironmentBindings {
		response.Bindings = append(response.Bindings, &controlplanev1.AgentRuntimeEnvironmentBinding{Ref: item.Ref, AgentRef: item.AgentRef,
			Version: item.Version, EnvironmentRef: item.EnvironmentRef, VersionRef: item.VersionRef, Digest: item.Digest})
	}
	return response, nil
}

// castRuntimeEnvironmentConsumer copies consumer agent, binding, source version and owner scope
// pins from the domain read model.
// castRuntimeEnvironmentConsumer копирует привязки агента, связи, версии источника и области
// владельца из доменной модели чтения потребителя.
func castRuntimeEnvironmentConsumer(item entity.RuntimeEnvironmentConsumer) *controlplanev1.RuntimeEnvironmentConsumer {
	return &controlplanev1.RuntimeEnvironmentConsumer{AgentRef: item.AgentRef, AgentVersion: item.AgentVersion,
		BindingRef: item.BindingRef, BindingVersion: item.BindingVersion, VersionRef: item.VersionRef,
		ProjectRef: item.ProjectRef, ScopeKind: roleImageScopeKind(item.ScopeKind), OrganizationRef: item.OrganizationRef}
}

// runtimeEnvironmentConsumerInput copies requested consumer and scope locators for owner
// validation without treating those locators as proof of ownership.
// runtimeEnvironmentConsumerInput копирует запрошенные ссылки потребителя и области для проверки
// владельцем без признания ссылок доказательством владения.
func runtimeEnvironmentConsumerInput(item *controlplanev1.RuntimeEnvironmentConsumer) entity.RuntimeEnvironmentConsumer {
	return entity.RuntimeEnvironmentConsumer{AgentRef: item.GetAgentRef(), AgentVersion: item.GetAgentVersion(),
		BindingRef: item.GetBindingRef(), BindingVersion: item.GetBindingVersion(), VersionRef: item.GetVersionRef(),
		ProjectRef: item.GetProjectRef(), ScopeKind: runtimeResourceScopeName(item.GetScopeKind()), OrganizationRef: item.GetOrganizationRef()}
}

// runtimeResourceScopeName maps only ORGANIZATION and PROJECT enum values to domain scope names
// and returns empty for every other value.
// runtimeResourceScopeName переводит только enum ORGANIZATION и PROJECT в доменные имена областей
// и возвращает пустую строку для прочих значений.
func runtimeResourceScopeName(kind controlplanev1.RuntimeResourceScopeKind) string {
	switch kind {
	case controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION:
		return "ORGANIZATION"
	case controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT:
		return "PROJECT"
	default:
		return ""
	}
}
