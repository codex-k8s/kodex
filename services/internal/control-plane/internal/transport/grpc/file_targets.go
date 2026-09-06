package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) ListArtifactBindingTargets(ctx context.Context, request *controlplanev1.ListArtifactBindingTargetsRequest) (*controlplanev1.ListArtifactBindingTargetsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListArtifactBindingTargets_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.ListArtifactBindingTargets(ctx, p, request.GetArtifactRef(), query.Filter{Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListArtifactBindingTargetsResponse{
		ArtifactRef: result.ArtifactRef, ArtifactVersion: result.ArtifactVersion, ProjectRef: result.ProjectRef,
		Total: result.Total, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken}, Digest: result.Digest,
		EvaluatedAt: timestamp(result.EvaluatedAt), Items: []*controlplanev1.ArtifactBindingTarget{},
	}
	for _, item := range result.Items {
		response.Items = append(response.Items, &controlplanev1.ArtifactBindingTarget{
			AgentRef: item.AgentRef, AgentVersion: item.AgentVersion, Name: item.Name, State: agentState(item.State),
			Bound: item.Bound, CanBind: item.CanBind, CanUnbind: item.CanUnbind,
			BindReason:   controlplanev1.ArtifactBindingTargetReason(controlplanev1.ArtifactBindingTargetReason_value["ARTIFACT_BINDING_TARGET_REASON_"+item.BindReason]),
			UnbindReason: controlplanev1.ArtifactBindingTargetReason(controlplanev1.ArtifactBindingTargetReason_value["ARTIFACT_BINDING_TARGET_REASON_"+item.UnbindReason]),
		})
	}
	return response, nil
}

func (server *Server) GetRunAttachmentEligibility(ctx context.Context, request *controlplanev1.GetRunAttachmentEligibilityRequest) (*controlplanev1.GetRunAttachmentEligibilityResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRunAttachmentEligibility_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.GetRunAttachmentEligibility(ctx, p, request.GetProjectRef(), runTarget(request.GetTarget()), request.GetRunRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetRunAttachmentEligibilityResponse{
		ProjectRef: result.ProjectRef, Target: castRunTarget(result.Target), RunRef: result.RunRef, RunVersion: result.RunVersion,
		WorkflowVersionRef: result.WorkflowVersionRef, Eligible: result.Eligible, Digest: result.Digest, EvaluatedAt: timestamp(result.EvaluatedAt),
		Reason: controlplanev1.RunAttachmentEligibilityReason(controlplanev1.RunAttachmentEligibilityReason_value["RUN_ATTACHMENT_ELIGIBILITY_REASON_"+result.Reason]),
	}, nil
}
