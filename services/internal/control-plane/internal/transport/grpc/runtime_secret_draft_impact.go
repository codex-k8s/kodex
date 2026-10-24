package grpc

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// castDraftImpactPlan projects draft and secret versions, source revision, digest, count, state
// and expiry from the owner impact plan.
// castDraftImpactPlan преобразует версии черновика и секрета, ревизию источника, digest, число,
// состояние и срок из плана влияния владельца.
func castDraftImpactPlan(p entity.RuntimeSecretDraftImpactPlan) *cp.RuntimeSecretDraftImpactPlan {
	return &cp.RuntimeSecretDraftImpactPlan{Ref: p.Ref, DraftRef: p.DraftRef, DraftVersion: p.DraftVersion, SecretRef: p.SecretRef, SecretVersion: p.SecretVersion, SourceRevision: p.SourceRevision, Digest: p.Digest, Total: p.Total, ExpiresAt: timestamp(p.ExpiresAt), State: cp.RuntimeSecretDraftImpactState(cp.RuntimeSecretDraftImpactState_value["RUNTIME_SECRET_DRAFT_IMPACT_STATE_"+p.State])}
}

// PrepareRuntimeSecretDraftImpact uses the verified preparation principal and supplied mutation to
// request a draft impact plan without applying it.
// PrepareRuntimeSecretDraftImpact использует проверенный principal подготовки и переданную
// mutation для запроса плана влияния черновика без его применения.
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

// GetRuntimeSecretDraftImpact reads an authorized impact page and projects affected consumers,
// per-item outcomes and resulting environment/binding pins.
// GetRuntimeSecretDraftImpact читает авторизованную страницу влияния и преобразует затронутых
// потребителей, исходы элементов и привязки результата среды и связи.
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
		consumer := castRuntimeSecretImpactConsumer(item.Consumer)
		response.Items = append(response.Items, &cp.RuntimeSecretDraftImpactItem{Ref: item.Ref, Consumer: consumer, Outcome: cp.RuntimeSecretDraftImpactOutcome(cp.RuntimeSecretDraftImpactOutcome_value["RUNTIME_SECRET_DRAFT_IMPACT_OUTCOME_"+item.Outcome]), ResultEnvironmentVersionRef: item.ResultEnvironmentVersionRef, ResultBindingRef: item.ResultBindingRef, ResultBindingVersion: item.ResultBindingVersion})
	}
	return response, nil
}
