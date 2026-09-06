package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) PrepareRoleImageImpactPlan(ctx context.Context, request *cp.PrepareRoleImageImpactPlanRequest) (*cp.PrepareRoleImageImpactPlanResponse, error) {
	result, err := execute(ctx, server.service, cp.PlatformCommandService_PrepareRoleImageImpactPlan_FullMethodName, command.PrepareRoleImageImpactPlan, request.GetMutation(), command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef()})
	if err != nil {
		return nil, err
	}
	return &cp.PrepareRoleImageImpactPlanResponse{Plan: castRoleImageImpactPlan(result.RoleImageImpactPlan)}, nil
}

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
			Consumer: &cp.RuntimeEnvironmentConsumer{AgentRef: item.Consumer.AgentRef, AgentVersion: item.Consumer.AgentVersion, BindingRef: item.Consumer.BindingRef, BindingVersion: item.Consumer.BindingVersion, VersionRef: item.Consumer.VersionRef, ProjectRef: item.Consumer.ProjectRef},
			Outcome:  cp.RoleImageImpactOutcome(cp.RoleImageImpactOutcome_value["ROLE_IMAGE_IMPACT_OUTCOME_"+item.Outcome]), ResultEnvironmentVersionRef: item.ResultEnvironmentVersionRef, ResultBindingRef: item.ResultBindingRef, ResultBindingVersion: item.ResultBindingVersion})
	}
	return response, nil
}

func castRoleImageImpactPlan(item *entity.RoleImageImpactPlan) *cp.RoleImageImpactPlan {
	if item == nil {
		return nil
	}
	return &cp.RoleImageImpactPlan{Ref: item.Ref, Version: item.Version, ConfigurationRef: item.ConfigurationRef, ConfigurationVersion: item.ConfigurationVersion,
		RevisionRef: item.RevisionRef, RevisionDigest: item.RevisionDigest, RecipeRef: item.RecipeRef, RecipeGeneration: item.RecipeGeneration, BuildRef: item.BuildRef,
		ArtifactRef: item.ArtifactRef, ArtifactDigest: item.ArtifactDigest, AdmissionPolicyDigest: item.AdmissionPolicyDigest, Digest: item.Digest, Total: item.Total,
		State: cp.RoleImageImpactPlanState(cp.RoleImageImpactPlanState_value["ROLE_IMAGE_IMPACT_PLAN_STATE_"+item.State]), CreatedAt: timestamppb.New(item.CreatedAt), ExpiresAt: timestamppb.New(item.ExpiresAt)}
}
