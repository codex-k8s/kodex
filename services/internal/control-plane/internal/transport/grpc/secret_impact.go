package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// GetRuntimeSecretImpact reads authorized secret revision impact and projects owner-scoped
// environment consumers with total and pagination.
// GetRuntimeSecretImpact читает авторизованное влияние ревизии секрета и преобразует потребителей
// среды в области владельца с общим числом и пагинацией.
func (server *Server) GetRuntimeSecretImpact(ctx context.Context, request *controlplanev1.GetRuntimeSecretImpactRequest) (*controlplanev1.GetRuntimeSecretImpactResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRuntimeSecretImpact_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetRuntimeSecretImpact(ctx, p, request.GetSecretRef(), request.GetRevision(), request.GetQuery(), query.Page{Size: request.GetPage().GetPageSize(), Token: request.GetPage().GetPageToken()})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.GetRuntimeSecretImpactResponse{SecretRef: result.SecretRef, SecretVersion: result.SecretVersion, TargetRevision: result.TargetRevision, Total: result.Total,
		Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken}}
	for _, item := range result.Consumers {
		consumer := castRuntimeSecretImpactConsumer(item)
		response.Consumers = append(response.Consumers, consumer)
	}
	return response, nil
}

// RebindRuntimeSecret copies selected environment and consumer pins into the dedicated command and
// requires one returned environment per selection before projecting results.
// RebindRuntimeSecret копирует выбранные привязки среды и потребителей в отдельную команду и
// требует одну возвращённую среду на выбор до преобразования результата.
func (server *Server) RebindRuntimeSecret(ctx context.Context, request *controlplanev1.RebindRuntimeSecretRequest) (*controlplanev1.RebindRuntimeSecretResponse, error) {
	input := command.RuntimeSecretRebindInput{SecretRef: request.GetSecretRef(), Revision: request.GetRevision()}
	for _, item := range request.GetSelections() {
		selection := entity.RuntimeSecretRebindSelection{EnvironmentRef: item.GetEnvironmentRef(), ExpectedEnvironmentVersion: item.GetExpectedEnvironmentVersion(), SourceVersionRef: item.GetSourceVersionRef(),
			ScopeKind: runtimeResourceScopeName(item.GetScopeKind()), OrganizationRef: item.GetOrganizationRef(), ProjectRef: item.GetProjectRef()}
		for _, c := range item.GetConsumers() {
			selection.Consumers = append(selection.Consumers, runtimeEnvironmentConsumerInput(c))
		}
		input.Selections = append(input.Selections, selection)
	}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RebindRuntimeSecret_FullMethodName, command.RebindRuntimeSecret, request.GetMutation(), input)
	if err != nil {
		return nil, err
	}
	if len(result.RuntimeEnvironments) != len(input.Selections) {
		return nil, transportError(errs.ErrUnavailable)
	}
	response := &controlplanev1.RebindRuntimeSecretResponse{}
	for _, environment := range result.RuntimeEnvironments {
		response.Environments = append(response.Environments, castRuntimeEnvironment(environment))
	}
	for _, item := range result.EnvironmentBindings {
		response.Bindings = append(response.Bindings, &controlplanev1.AgentRuntimeEnvironmentBinding{Ref: item.Ref, AgentRef: item.AgentRef, Version: item.Version, EnvironmentRef: item.EnvironmentRef, VersionRef: item.VersionRef, Digest: item.Digest})
	}
	return response, nil
}

// castRuntimeSecretImpactConsumer projects the same owner-scoped environment and nested runtime
// consumer pins for direct and draft impact reads, preserving secret revision slice presence.
// castRuntimeSecretImpactConsumer преобразует одинаковые привязки среды в области владельца и
// вложенного runtime consumer для прямого и draft impact, сохраняя наличие среза ревизий секрета.
func castRuntimeSecretImpactConsumer(item entity.RuntimeSecretImpactConsumer) *controlplanev1.RuntimeSecretImpactConsumer {
	return &controlplanev1.RuntimeSecretImpactConsumer{EnvironmentRef: item.EnvironmentRef,
		EnvironmentVersion: item.EnvironmentVersion, EnvironmentVersionRef: item.EnvironmentVersionRef,
		SecretRevisions: item.SecretRevisions, ScopeKind: roleImageScopeKind(item.ScopeKind),
		OrganizationRef: item.OrganizationRef, ProjectRef: item.ProjectRef,
		Consumer: castRuntimeEnvironmentConsumer(item.Consumer)}
}
