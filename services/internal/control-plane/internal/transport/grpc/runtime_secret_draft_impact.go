package grpc

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func castDraftImpactPlan(p entity.RuntimeSecretDraftImpactPlan) *cp.RuntimeSecretDraftImpactPlan {
	return &cp.RuntimeSecretDraftImpactPlan{Ref: p.Ref, DraftRef: p.DraftRef, DraftVersion: p.DraftVersion, SecretRef: p.SecretRef, SecretVersion: p.SecretVersion, SourceRevision: p.SourceRevision, Digest: p.Digest, Total: p.Total, ExpiresAt: timestamp(p.ExpiresAt), State: cp.RuntimeSecretDraftImpactState(cp.RuntimeSecretDraftImpactState_value["RUNTIME_SECRET_DRAFT_IMPACT_STATE_"+p.State])}
}
func (server *Server) PrepareRuntimeSecretDraftImpact(ctx context.Context, request *cp.PrepareRuntimeSecretDraftImpactRequest) (*cp.PrepareRuntimeSecretDraftImpactResponse, error) {
	p, err := principal(ctx, cp.PlatformCommandService_PrepareRuntimeSecretDraftImpact_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.PrepareRuntimeSecretDraftImpact(ctx, p, request.GetDraftRef(), mutation(request.GetMutation()))
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.PrepareRuntimeSecretDraftImpactResponse{Plan: castDraftImpactPlan(result)}, nil
}
func (server *Server) GetRuntimeSecretDraftImpact(ctx context.Context, request *cp.GetRuntimeSecretDraftImpactRequest) (*cp.GetRuntimeSecretDraftImpactResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_GetRuntimeSecretDraftImpact_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetRuntimeSecretDraftImpact(ctx, p, request.GetPlanRef(), request.GetQuery(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.GetRuntimeSecretDraftImpactResponse{Plan: castDraftImpactPlan(result.Plan), Total: result.Total, Page: &cp.PageInfo{NextPageToken: result.NextPageToken}}
	for _, item := range result.Items {
		c := item.Consumer
		consumer := &cp.RuntimeSecretImpactConsumer{EnvironmentRef: c.EnvironmentRef, EnvironmentVersion: c.EnvironmentVersion, EnvironmentVersionRef: c.EnvironmentVersionRef, SecretRevisions: c.SecretRevisions}
		a := c.Consumer
		consumer.Consumer = &cp.RuntimeEnvironmentConsumer{AgentRef: a.AgentRef, AgentVersion: a.AgentVersion, BindingRef: a.BindingRef, BindingVersion: a.BindingVersion, VersionRef: a.VersionRef, ProjectRef: a.ProjectRef}
		response.Items = append(response.Items, &cp.RuntimeSecretDraftImpactItem{Ref: item.Ref, Consumer: consumer, Outcome: cp.RuntimeSecretDraftImpactOutcome(cp.RuntimeSecretDraftImpactOutcome_value["RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_"+item.Outcome]), ResultEnvironmentVersionRef: item.ResultEnvironmentVersionRef, ResultBindingRef: item.ResultBindingRef, ResultBindingVersion: item.ResultBindingVersion})
	}
	return response, nil
}
