package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PrepareRoleImageImpactPlan dispatches the specialized plan-preparation command through the shared
// execute boundary using exact configuration/revision and mutation inputs; it maps the returned
// optional plan rather than applying it.
// PrepareRoleImageImpactPlan направляет специализированную команду подготовки плана через общую
// execute-границу с точными конфигурацией, ревизией и вводом изменения; переносит полученный
// необязательный план, не применяя его.
func (server *Server) PrepareRoleImageImpactPlan(ctx context.Context, request *cp.PrepareRoleImageImpactPlanRequest) (*cp.PrepareRoleImageImpactPlanResponse, error) {
	return executeResponse(ctx, server.service, cp.PlatformCommandService_PrepareRoleImageImpactPlan_FullMethodName, command.PrepareRoleImageImpactPlan, request.GetMutation(), command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef()}, func(result command.Result) *cp.PrepareRoleImageImpactPlanResponse {
		return &cp.PrepareRoleImageImpactPlanResponse{Plan: castRoleImageImpactPlan(result.RoleImageImpactPlan)}
	})
}

// GetRoleImageImpactPlan resolves the exact query principal, requests a plan page from the domain and
// maps consumer/outcome/version pins in the returned order without applying environment rebinding.
// GetRoleImageImpactPlan разрешает principal точного запроса, получает страницу плана из домена и
// переносит привязки потребителя, результата и версии по порядку, не выполняя перепривязку окружений.
func (server *Server) GetRoleImageImpactPlan(ctx context.Context, request *cp.GetRoleImageImpactPlanRequest) (*cp.GetRoleImageImpactPlanResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_GetRoleImageImpactPlan_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetRoleImageImpactPlan(ctx, p, request.GetPlanRef(), request.GetQuery(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.GetRoleImageImpactPlanResponse{Plan: castRoleImageImpactPlan(&result.Plan), Total: result.Total, Page: &cp.PageInfo{NextPageToken: result.NextPageToken}}
	for _, item := range result.Items {
		response.Items = append(response.Items, &cp.RoleImageImpactItem{Ref: item.Ref, EnvironmentRef: item.EnvironmentRef, EnvironmentVersion: item.EnvironmentVersion,
			SourceVersionRef: item.SourceVersionRef, SourceVersionDigest: item.SourceVersionDigest,
			Consumer: castRuntimeEnvironmentConsumer(item.Consumer),
			Outcome:  cp.RoleImageImpactOutcome(cp.RoleImageImpactOutcome_value["ROLE_IMAGE_IMPACT_OUTCOME_"+item.Outcome]), ResultEnvironmentVersionRef: item.ResultEnvironmentVersionRef, ResultBindingRef: item.ResultBindingRef, ResultBindingVersion: item.ResultBindingVersion})
	}
	return response, nil
}

// castRoleImageImpactPlan preserves nil or projects plan identity/version, configuration/revision,
// image/policy/digest pins, total, enum state and timestamps; it does not validate freshness or
// approve the plan.
// castRoleImageImpactPlan сохраняет nil либо переносит личность и версию плана, конфигурацию, ревизию,
// привязки образа, политики и хешей, количество, enum состояния и сроки; свежесть не проверяет и план
// не одобряет.
func castRoleImageImpactPlan(item *entity.RoleImageImpactPlan) *cp.RoleImageImpactPlan {
	if item == nil {
		return nil
	}
	return &cp.RoleImageImpactPlan{Ref: item.Ref, Version: item.Version, ConfigurationRef: item.ConfigurationRef, ConfigurationVersion: item.ConfigurationVersion,
		RevisionRef: item.RevisionRef, RevisionDigest: item.RevisionDigest, RecipeRef: item.RecipeRef, RecipeGeneration: item.RecipeGeneration, BuildRef: item.BuildRef,
		ArtifactRef: item.ArtifactRef, ArtifactDigest: item.ArtifactDigest, AdmissionPolicyDigest: item.AdmissionPolicyDigest, Digest: item.Digest, Total: item.Total,
		State: cp.RoleImageImpactPlanState(cp.RoleImageImpactPlanState_value["ROLE_IMAGE_IMPACT_PLAN_STATE_"+item.State]), CreatedAt: timestamppb.New(item.CreatedAt), ExpiresAt: timestamppb.New(item.ExpiresAt)}
}
