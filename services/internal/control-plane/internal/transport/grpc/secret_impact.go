package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) GetRuntimeSecretImpact(ctx context.Context, request *controlplanev1.GetRuntimeSecretImpactRequest) (*controlplanev1.GetRuntimeSecretImpactResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRuntimeSecretImpact_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetRuntimeSecretImpact(ctx, p, request.GetSecretRef(), request.GetRevision(), query.Page{Size: request.GetPage().GetPageSize(), Token: request.GetPage().GetPageToken()})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.GetRuntimeSecretImpactResponse{SecretRef: result.SecretRef, SecretVersion: result.SecretVersion, TargetRevision: result.TargetRevision, Total: result.Total,
		Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken}}
	for _, item := range result.Consumers {
		consumer := &controlplanev1.RuntimeSecretImpactConsumer{EnvironmentRef: item.EnvironmentRef, EnvironmentVersion: item.EnvironmentVersion, EnvironmentVersionRef: item.EnvironmentVersionRef, SecretRevisions: item.SecretRevisions}
		if item.Consumer.AgentRef != "" {
			c := item.Consumer
			consumer.Consumer = &controlplanev1.RuntimeEnvironmentConsumer{AgentRef: c.AgentRef, AgentVersion: c.AgentVersion, BindingRef: c.BindingRef, BindingVersion: c.BindingVersion, VersionRef: c.VersionRef, ProjectRef: c.ProjectRef}
		}
		response.Consumers = append(response.Consumers, consumer)
	}
	return response, nil
}

func (server *Server) RebindRuntimeSecret(ctx context.Context, request *controlplanev1.RebindRuntimeSecretRequest) (*controlplanev1.RebindRuntimeSecretResponse, error) {
	input := command.RuntimeSecretRebindInput{SecretRef: request.GetSecretRef(), Revision: request.GetRevision()}
	for _, item := range request.GetSelections() {
		selection := entity.RuntimeSecretRebindSelection{EnvironmentRef: item.GetEnvironmentRef(), ExpectedEnvironmentVersion: item.GetExpectedEnvironmentVersion(), SourceVersionRef: item.GetSourceVersionRef()}
		for _, c := range item.GetConsumers() {
			selection.Consumers = append(selection.Consumers, entity.RuntimeEnvironmentConsumer{AgentRef: c.GetAgentRef(), AgentVersion: c.GetAgentVersion(), BindingRef: c.GetBindingRef(), BindingVersion: c.GetBindingVersion(), VersionRef: c.GetVersionRef(), ProjectRef: c.GetProjectRef()})
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
