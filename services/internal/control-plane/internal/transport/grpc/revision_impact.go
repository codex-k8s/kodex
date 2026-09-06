package grpc

import (
	"context"
	"encoding/hex"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) PreparePromptTemplateImpact(ctx context.Context, request *cp.PreparePromptTemplateImpactRequest) (*cp.PreparePromptTemplateImpactResponse, error) {
	result, err := execute(ctx, s.service, cp.PlatformCommandService_PreparePromptTemplateImpact_FullMethodName, command.PreparePromptTemplateImpact, request.GetMutation(), command.ManagedConfigurationInput{ConfigurationRef: request.GetConfigurationRef(), RevisionRef: request.GetRevisionRef()})
	if err != nil {
		return nil, err
	}
	return &cp.PreparePromptTemplateImpactResponse{Plan: castRevisionImpactPlan(result.RevisionImpactPlan)}, nil
}

func (s *Server) PrepareInstructionsImpact(ctx context.Context, request *cp.PrepareInstructionsImpactRequest) (*cp.PrepareInstructionsImpactResponse, error) {
	result, err := execute(ctx, s.service, cp.PlatformCommandService_PrepareInstructionsImpact_FullMethodName, command.PrepareInstructionsImpact, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef()})
	if err != nil {
		return nil, err
	}
	return &cp.PrepareInstructionsImpactResponse{Plan: castRevisionImpactPlan(result.RevisionImpactPlan)}, nil
}

func (s *Server) PrepareEnvironmentDraftImpact(ctx context.Context, request *cp.PrepareEnvironmentDraftImpactRequest) (*cp.PrepareEnvironmentDraftImpactResponse, error) {
	result, err := execute(ctx, s.service, cp.PlatformCommandService_PrepareEnvironmentDraftImpact_FullMethodName, command.PrepareEnvironmentDraftImpact, request.GetMutation(), command.RuntimeEnvironmentDraftInput{DraftRef: request.GetDraftRef()})
	if err != nil {
		return nil, err
	}
	return &cp.PrepareEnvironmentDraftImpactResponse{Plan: castRevisionImpactPlan(result.RevisionImpactPlan)}, nil
}

func (s *Server) GetRevisionImpactPlan(ctx context.Context, request *cp.GetRevisionImpactPlanRequest) (*cp.GetRevisionImpactPlanResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_GetRevisionImpactPlan_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := s.service.GetRevisionImpactPlan(ctx, p, request.GetPlanRef(), request.GetQuery(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &cp.GetRevisionImpactPlanResponse{Plan: castRevisionImpactPlan(&result.Plan), Total: result.Total, Page: &cp.PageInfo{NextPageToken: result.NextPageToken}}
	for _, item := range result.Items {
		response.Items = append(response.Items, &cp.RevisionImpactItem{Ref: item.Ref, ProjectRef: item.ProjectRef,
			ConsumerKind: cp.RevisionImpactConsumerKind(cp.RevisionImpactConsumerKind_value["REVISION_IMPACT_CONSUMER_KIND_"+item.ConsumerKind]),
			ConsumerRef:  item.ConsumerRef, ConsumerVersion: item.ConsumerVersion, BindingRef: item.BindingRef, BindingVersion: item.BindingVersion,
			SourceRevisionRef: item.SourceRevisionRef, Outcome: cp.RevisionImpactOutcome(cp.RevisionImpactOutcome_value["REVISION_IMPACT_OUTCOME_"+item.Outcome]),
			ResultRevisionRef: item.ResultRevisionRef, ResultBindingRef: item.ResultBindingRef, ResultBindingVersion: item.ResultBindingVersion, ResultConsumerVersion: item.ResultConsumerVersion})
	}
	return response, nil
}

func validatePublishedImpactResult(result command.Result, kind, planRef, sourceRef string) error {
	p := result.RevisionImpactPlan
	if p == nil || p.Ref != planRef || planRef == "" || p.Kind != kind || p.State != "APPLIED" || p.Version != 2 || p.SourceRef != sourceRef || p.SourceVersion < 1 || p.DraftVersion < 1 || p.DraftRef == "" || p.PublishedRevisionRef != p.DraftRef || p.Total < 0 || p.Total > 1000 {
		return errs.ErrUnavailable
	}
	for _, digest := range []string{p.Digest, p.TargetDigest} {
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != 32 || digest != strings.ToLower(digest) {
			return errs.ErrUnavailable
		}
	}
	if kind == "AGENT_INSTRUCTIONS" {
		if result.Agent == nil || result.Agent.Ref != sourceRef || result.Agent.Version != p.SourceVersion+1 {
			return errs.ErrUnavailable
		}
	} else if kind == "PROMPT_TEMPLATE" {
		if result.ManagedConfiguration == nil || result.ManagedRevision == nil || result.ManagedConfiguration.Ref != sourceRef || result.ManagedConfiguration.Version <= p.SourceVersion || result.ManagedRevision.Ref != p.PublishedRevisionRef || result.ManagedRevision.Digest != p.TargetDigest || result.ManagedRevision.State != "PUBLISHED" {
			return errs.ErrUnavailable
		}
	} else {
		return errs.ErrUnavailable
	}
	return nil
}

func castRevisionImpactPlan(p *entity.RevisionImpactPlan) *cp.RevisionImpactPlan {
	if p == nil {
		return nil
	}
	return &cp.RevisionImpactPlan{Ref: p.Ref, Version: p.Version, Kind: cp.RevisionImpactKind(cp.RevisionImpactKind_value["REVISION_IMPACT_KIND_"+p.Kind]),
		SourceRef: p.SourceRef, SourceVersion: p.SourceVersion, SourceRevisionRef: p.SourceRevisionRef, DraftRef: p.DraftRef, DraftVersion: p.DraftVersion,
		TargetDigest: p.TargetDigest, Digest: p.Digest, Total: p.Total, State: cp.RevisionImpactState(cp.RevisionImpactState_value["REVISION_IMPACT_STATE_"+p.State]),
		CreatedAt: timestamppb.New(p.CreatedAt), ExpiresAt: timestamppb.New(p.ExpiresAt), PublishedRevisionRef: p.PublishedRevisionRef}
}
