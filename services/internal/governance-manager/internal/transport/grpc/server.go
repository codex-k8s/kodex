package grpc

import (
	"context"

	"github.com/google/uuid"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
	grpcruntime "google.golang.org/grpc"
)

var _ governancev1.GovernanceManagerServiceServer = (*Server)(nil)

// Server exposes GovernanceManagerService over gRPC.
type Server struct {
	governancev1.UnimplementedGovernanceManagerServiceServer

	service governanceService
}

type governanceService interface {
	BacklogOperation(context.Context, governanceservice.BacklogOperationInput) error
	CreateRiskProfile(context.Context, governanceservice.CreateRiskProfileInput) (entity.RiskProfile, error)
	CreateRiskProfileVersion(context.Context, governanceservice.CreateRiskProfileVersionInput) (entity.RiskProfileVersion, error)
	ActivateRiskProfileVersion(context.Context, governanceservice.ActivateRiskProfileVersionInput) (entity.RiskProfileVersion, error)
	ArchiveRiskProfile(context.Context, governanceservice.ArchiveRiskProfileInput) (entity.RiskProfile, error)
	GetRiskProfile(context.Context, uuid.UUID) (entity.RiskProfile, error)
	GetRiskProfileVersion(context.Context, uuid.UUID, int64) (entity.RiskProfileVersion, error)
	ListRiskProfiles(context.Context, governanceservice.ListRiskProfilesInput) ([]entity.RiskProfile, query.PageResult, error)
	ListRiskRules(context.Context, governanceservice.ListRiskRulesInput) ([]entity.RiskRule, query.PageResult, error)
	ListGatePolicies(context.Context, governanceservice.ListGatePoliciesInput) ([]entity.GatePolicy, query.PageResult, error)
	EvaluateRisk(context.Context, governanceservice.EvaluateRiskInput) (entity.RiskAssessment, error)
	GetRiskAssessment(context.Context, uuid.UUID) (entity.RiskAssessment, error)
	ListRiskAssessments(context.Context, governanceservice.ListRiskAssessmentsInput) ([]entity.RiskAssessment, query.PageResult, error)
	ListRiskFactors(context.Context, governanceservice.ListRiskFactorsInput) ([]entity.RiskFactor, query.PageResult, error)
	RecordReviewSignal(context.Context, governanceservice.RecordReviewSignalInput) (entity.ReviewSignal, error)
	ListReviewSignals(context.Context, governanceservice.ListReviewSignalsInput) ([]entity.ReviewSignal, query.PageResult, error)
	RequestGate(context.Context, governanceservice.RequestGateInput) (entity.GateRequest, error)
	SubmitGateDecision(context.Context, governanceservice.SubmitGateDecisionInput) (entity.GateDecision, entity.GateRequest, error)
	CancelGate(context.Context, governanceservice.CancelGateInput) (entity.GateRequest, error)
	ExpireGate(context.Context, governanceservice.ExpireGateInput) (entity.GateRequest, error)
	GetGateDecision(context.Context, governanceservice.GetGateDecisionInput) (entity.GateDecision, error)
	ListGateDecisions(context.Context, governanceservice.ListGateDecisionsInput) ([]entity.GateDecision, query.PageResult, error)
	GetGateRequest(context.Context, governanceservice.GetGateRequestInput) (entity.GateRequest, error)
	ListGateRequests(context.Context, governanceservice.ListGateRequestsInput) ([]entity.GateRequest, query.PageResult, error)
	BuildReleaseDecisionPackage(context.Context, governanceservice.BuildReleaseDecisionPackageInput) (entity.ReleaseDecisionPackage, error)
	GetReleaseDecisionPackage(context.Context, uuid.UUID) (entity.ReleaseDecisionPackage, error)
	ListReleaseDecisionPackages(context.Context, governanceservice.ListReleaseDecisionPackagesInput) ([]entity.ReleaseDecisionPackage, query.PageResult, error)
}

// NewServer creates a governance-manager gRPC server boundary.
func NewServer(service governanceService) *Server {
	if service == nil {
		panic("governance-manager grpc service is required")
	}
	return &Server{service: service}
}

// RegisterGovernanceManagerService registers governance-manager handlers in a gRPC runtime.
func RegisterGovernanceManagerService(registrar grpcruntime.ServiceRegistrar, service governanceService) {
	governancev1.RegisterGovernanceManagerServiceServer(registrar, NewServer(service))
}

type terminalGateCommandInput struct {
	gateRequestID          uuid.UUID
	reason                 string
	interactionDeliveryRef value.InteractionDeliveryRef
	meta                   governanceservice.CommandMeta
}

type terminalGateHandler func(context.Context, terminalGateCommandInput) (entity.GateRequest, error)

func terminalGateCommand(gateRequestID string, reason string, ref *governancev1.InteractionDeliveryRef, meta *governancev1.CommandMeta) (terminalGateCommandInput, error) {
	metaValue, err := commandMeta(meta)
	if err != nil {
		return terminalGateCommandInput{}, err
	}
	id, err := requiredUUID(gateRequestID)
	if err != nil {
		return terminalGateCommandInput{}, err
	}
	return terminalGateCommandInput{
		gateRequestID:          id,
		reason:                 reason,
		interactionDeliveryRef: interactionDeliveryRef(ref),
		meta:                   metaValue,
	}, nil
}

func (server *Server) terminalGateCommandResponse(
	ctx context.Context,
	gateRequestID string,
	reason string,
	ref *governancev1.InteractionDeliveryRef,
	meta *governancev1.CommandMeta,
	handler terminalGateHandler,
) (*governancev1.GateRequestResponse, error) {
	command, err := terminalGateCommand(gateRequestID, reason, ref, meta)
	if err != nil {
		return nil, err
	}
	return server.terminalGateResponse(ctx, command, handler)
}

func (server *Server) terminalGateResponse(ctx context.Context, command terminalGateCommandInput, handler terminalGateHandler) (*governancev1.GateRequestResponse, error) {
	request, err := handler(ctx, command)
	if err != nil {
		return nil, err
	}
	return &governancev1.GateRequestResponse{GateRequest: toGateRequest(request)}, nil
}

func (server *Server) cancelGate(ctx context.Context, command terminalGateCommandInput) (entity.GateRequest, error) {
	return server.service.CancelGate(ctx, governanceservice.CancelGateInput{
		GateRequestID:          command.gateRequestID,
		Reason:                 command.reason,
		InteractionDeliveryRef: command.interactionDeliveryRef,
		Meta:                   command.meta,
	})
}

func (server *Server) expireGate(ctx context.Context, command terminalGateCommandInput) (entity.GateRequest, error) {
	input := governanceservice.ExpireGateInput{
		GateRequestID:          command.gateRequestID,
		Reason:                 command.reason,
		InteractionDeliveryRef: command.interactionDeliveryRef,
		Meta:                   command.meta,
	}
	return server.service.ExpireGate(ctx, input)
}

// CreateRiskProfile creates risk profile metadata.
func (server *Server) CreateRiskProfile(ctx context.Context, req *governancev1.CreateRiskProfileRequest) (*governancev1.RiskProfileResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	profile, err := server.service.CreateRiskProfile(ctx, governanceservice.CreateRiskProfileInput{
		Scope:       scopeRef(req.GetScope()),
		Slug:        req.GetSlug(),
		DisplayName: localizedTexts(req.GetDisplayName()),
		Description: localizedTexts(req.GetDescription()),
		Meta:        meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskProfileResponse{RiskProfile: toRiskProfile(profile)}, nil
}

// CreateRiskProfileVersion creates an immutable policy version.
func (server *Server) CreateRiskProfileVersion(ctx context.Context, req *governancev1.CreateRiskProfileVersionRequest) (*governancev1.RiskProfileVersionResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	riskProfileID, err := requiredUUID(req.GetRiskProfileId())
	if err != nil {
		return nil, err
	}
	rules, err := riskRuleDrafts(riskProfileID, 0, req.GetRules())
	if err != nil {
		return nil, err
	}
	version, err := server.service.CreateRiskProfileVersion(ctx, governanceservice.CreateRiskProfileVersionInput{
		RiskProfileID: riskProfileID,
		Rules:         rules,
		GatePolicies:  gatePolicyDrafts(riskProfileID, 0, req.GetGatePolicies()),
		Meta:          meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskProfileVersionResponse{RiskProfileVersion: toRiskProfileVersion(version)}, nil
}

// ActivateRiskProfileVersion activates one profile version.
func (server *Server) ActivateRiskProfileVersion(ctx context.Context, req *governancev1.ActivateRiskProfileVersionRequest) (*governancev1.RiskProfileVersionResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	riskProfileID, err := requiredUUID(req.GetRiskProfileId())
	if err != nil {
		return nil, err
	}
	version, err := server.service.ActivateRiskProfileVersion(ctx, governanceservice.ActivateRiskProfileVersionInput{
		RiskProfileID:  riskProfileID,
		ProfileVersion: req.GetProfileVersion(),
		Meta:           meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskProfileVersionResponse{RiskProfileVersion: toRiskProfileVersion(version)}, nil
}

// ArchiveRiskProfile archives profile metadata.
func (server *Server) ArchiveRiskProfile(ctx context.Context, req *governancev1.ArchiveRiskProfileRequest) (*governancev1.RiskProfileResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	riskProfileID, err := requiredUUID(req.GetRiskProfileId())
	if err != nil {
		return nil, err
	}
	profile, err := server.service.ArchiveRiskProfile(ctx, governanceservice.ArchiveRiskProfileInput{RiskProfileID: riskProfileID, Meta: meta})
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskProfileResponse{RiskProfile: toRiskProfile(profile)}, nil
}

// GetRiskProfile returns profile metadata.
func (server *Server) GetRiskProfile(ctx context.Context, req *governancev1.GetRiskProfileRequest) (*governancev1.RiskProfileResponse, error) {
	id, err := requiredUUID(req.GetRiskProfileId())
	if err != nil {
		return nil, err
	}
	profile, err := server.service.GetRiskProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskProfileResponse{RiskProfile: toRiskProfile(profile)}, nil
}

// GetRiskProfileVersion returns one immutable profile version.
func (server *Server) GetRiskProfileVersion(ctx context.Context, req *governancev1.GetRiskProfileVersionRequest) (*governancev1.RiskProfileVersionResponse, error) {
	id, err := requiredUUID(req.GetRiskProfileId())
	if err != nil {
		return nil, err
	}
	version, err := server.service.GetRiskProfileVersion(ctx, id, req.GetProfileVersion())
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskProfileVersionResponse{RiskProfileVersion: toRiskProfileVersion(version)}, nil
}

// ListRiskProfiles returns profiles by scope and status.
func (server *Server) ListRiskProfiles(ctx context.Context, req *governancev1.ListRiskProfilesRequest) (*governancev1.ListRiskProfilesResponse, error) {
	items, page, err := server.service.ListRiskProfiles(ctx, governanceservice.ListRiskProfilesInput{
		Filter: query.RiskProfileFilter{
			Scope:  scopeRef(req.GetScope()),
			Status: riskProfileStatus(req.GetStatus()),
			Page:   pageRequest(req.GetPage()),
		},
	})
	if err != nil {
		return nil, err
	}
	response := &governancev1.ListRiskProfilesResponse{Page: pageResponse(page)}
	for _, item := range items {
		response.RiskProfiles = append(response.RiskProfiles, toRiskProfile(item))
	}
	return response, nil
}

// ListRiskRules returns risk rules by profile version.
func (server *Server) ListRiskRules(ctx context.Context, req *governancev1.ListRiskRulesRequest) (*governancev1.ListRiskRulesResponse, error) {
	id, err := requiredUUID(req.GetRiskProfileId())
	if err != nil {
		return nil, err
	}
	items, page, err := server.service.ListRiskRules(ctx, governanceservice.ListRiskRulesInput{
		Filter: query.RuleFilter{
			RiskProfileID:  id,
			ProfileVersion: req.GetProfileVersion(),
			RuleKind:       riskRuleKind(req.GetRuleKind()),
			Status:         ruleStatus(req.GetStatus()),
			Page:           pageRequest(req.GetPage()),
		},
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ListRiskRulesResponse{RiskRules: toRiskRules(items), Page: pageResponse(page)}, nil
}

// ListGatePolicies returns gate policies by profile version.
func (server *Server) ListGatePolicies(ctx context.Context, req *governancev1.ListGatePoliciesRequest) (*governancev1.ListGatePoliciesResponse, error) {
	id, err := requiredUUID(req.GetRiskProfileId())
	if err != nil {
		return nil, err
	}
	items, page, err := server.service.ListGatePolicies(ctx, governanceservice.ListGatePoliciesInput{
		Filter: query.GatePolicyFilter{
			RiskProfileID:  id,
			ProfileVersion: req.GetProfileVersion(),
			GateKind:       gateKind(req.GetGateKind()),
			Status:         ruleStatus(req.GetStatus()),
			Page:           pageRequest(req.GetPage()),
		},
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ListGatePoliciesResponse{GatePolicies: toGatePolicies(items), Page: pageResponse(page)}, nil
}

// EvaluateRisk stores a minimal assessment record for GOV-3.
func (server *Server) EvaluateRisk(ctx context.Context, req *governancev1.EvaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	providerContext, err := protoObject(req.GetProviderContext())
	if err != nil {
		return nil, err
	}
	agentContext, err := protoObject(req.GetAgentContext())
	if err != nil {
		return nil, err
	}
	runtimeContext, err := protoObject(req.GetRuntimeContext())
	if err != nil {
		return nil, err
	}
	assessment, err := server.service.EvaluateRisk(ctx, governanceservice.EvaluateRiskInput{
		Target:          targetRef(req.GetTarget()),
		ProjectContext:  projectContext(req.GetProjectContext()),
		ProviderContext: providerContext,
		AgentContext:    agentContext,
		RuntimeContext:  runtimeContext,
		EvidenceRefs:    evidenceRefs(req.GetEvidenceRefs()),
		RiskProfileRef:  req.GetRiskProfileRef(),
		Meta:            meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskAssessmentResponse{RiskAssessment: toRiskAssessment(assessment)}, nil
}

// ReevaluateRisk is a stable contract handler reserved for GOV-4 risk classification.
func (server *Server) ReevaluateRisk(ctx context.Context, _ *governancev1.ReevaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error) {
	return nil, server.backlog(ctx, enum.OperationReevaluateRisk)
}

// GetRiskAssessment returns one assessment.
func (server *Server) GetRiskAssessment(ctx context.Context, req *governancev1.GetRiskAssessmentRequest) (*governancev1.RiskAssessmentResponse, error) {
	id, err := requiredUUID(req.GetRiskAssessmentId())
	if err != nil {
		return nil, err
	}
	assessment, err := server.service.GetRiskAssessment(ctx, id)
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskAssessmentResponse{RiskAssessment: toRiskAssessment(assessment)}, nil
}

// ListRiskAssessments returns assessments by target, project, risk class or status.
func (server *Server) ListRiskAssessments(ctx context.Context, req *governancev1.ListRiskAssessmentsRequest) (*governancev1.ListRiskAssessmentsResponse, error) {
	items, page, err := server.service.ListRiskAssessments(ctx, governanceservice.ListRiskAssessmentsInput{
		Filter: query.RiskAssessmentFilter{
			Target:             targetRef(req.GetTarget()),
			ProjectContext:     projectContext(req.GetProjectContext()),
			EffectiveRiskClass: riskClass(req.GetEffectiveRiskClass()),
			Status:             riskAssessmentStatus(req.GetStatus()),
			Page:               pageRequest(req.GetPage()),
		},
	})
	if err != nil {
		return nil, err
	}
	response := &governancev1.ListRiskAssessmentsResponse{Page: pageResponse(page)}
	for _, item := range items {
		response.RiskAssessments = append(response.RiskAssessments, toRiskAssessment(item))
	}
	return response, nil
}

// ListRiskFactors returns factors recorded for an assessment.
func (server *Server) ListRiskFactors(ctx context.Context, req *governancev1.ListRiskFactorsRequest) (*governancev1.ListRiskFactorsResponse, error) {
	assessmentID, err := requiredUUID(req.GetRiskAssessmentId())
	if err != nil {
		return nil, err
	}
	items, page, err := server.service.ListRiskFactors(ctx, governanceservice.ListRiskFactorsInput{
		Filter: query.RiskFactorFilter{
			RiskAssessmentID: assessmentID,
			SourceType:       riskFactorSourceType(req.GetSourceType()),
			Page:             pageRequest(req.GetPage()),
		},
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ListRiskFactorsResponse{RiskFactors: toRiskFactors(items), Page: pageResponse(page)}, nil
}

// RecordReviewSignal records a bounded review signal reference.
func (server *Server) RecordReviewSignal(ctx context.Context, req *governancev1.RecordReviewSignalRequest) (*governancev1.ReviewSignalResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	riskAssessmentID, err := optionalUUID(req.GetRiskAssessmentId())
	if err != nil {
		return nil, err
	}
	signal := enum.Confidence("")
	if req.Confidence != nil {
		signal = confidence(req.GetConfidence())
	}
	item, err := server.service.RecordReviewSignal(ctx, governanceservice.RecordReviewSignalInput{
		RiskAssessmentID: riskAssessmentID,
		Target:           targetRef(req.GetTarget()),
		RoleKind:         reviewRoleKind(req.GetRoleKind()),
		AuthorRef:        req.GetAuthorRef(),
		Outcome:          reviewSignalOutcome(req.GetOutcome()),
		Severity:         signalSeverity(req.GetSeverity()),
		Confidence:       signal,
		EvidenceRefs:     evidenceRefs(req.GetEvidenceRefs()),
		Summary:          req.GetSummary(),
		Meta:             meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ReviewSignalResponse{ReviewSignal: toReviewSignal(item)}, nil
}

// ListReviewSignals returns review signals by target, assessment, role or outcome.
func (server *Server) ListReviewSignals(ctx context.Context, req *governancev1.ListReviewSignalsRequest) (*governancev1.ListReviewSignalsResponse, error) {
	riskAssessmentID, err := optionalUUID(req.GetRiskAssessmentId())
	if err != nil {
		return nil, err
	}
	items, page, err := server.service.ListReviewSignals(ctx, governanceservice.ListReviewSignalsInput{
		Filter: query.ReviewSignalFilter{
			RiskAssessmentID: riskAssessmentID,
			Target:           targetRef(req.GetTarget()),
			RoleKind:         reviewRoleKind(req.GetRoleKind()),
			Outcome:          reviewSignalOutcome(req.GetOutcome()),
			Page:             pageRequest(req.GetPage()),
		},
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ListReviewSignalsResponse{ReviewSignals: toReviewSignals(items), Page: pageResponse(page)}, nil
}

// RequestGate creates a governance gate request reference.
func (server *Server) RequestGate(ctx context.Context, req *governancev1.RequestGateRequest) (*governancev1.GateRequestResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	riskAssessmentID, err := optionalUUID(req.GetRiskAssessmentId())
	if err != nil {
		return nil, err
	}
	gatePolicyID, err := optionalUUID(req.GetGatePolicyId())
	if err != nil {
		return nil, err
	}
	request, err := server.service.RequestGate(ctx, governanceservice.RequestGateInput{
		RiskAssessmentID:       riskAssessmentID,
		GatePolicyID:           gatePolicyID,
		Target:                 targetRef(req.GetTarget()),
		InteractionDeliveryRef: interactionDeliveryRef(req.GetInteractionDeliveryRef()),
		EvidenceRefs:           evidenceRefs(req.GetEvidenceRefs()),
		EvidenceSummary:        req.GetEvidenceSummary(),
		Meta:                   meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.GateRequestResponse{GateRequest: toGateRequest(request)}, nil
}

// SubmitGateDecision records the final governance decision for a gate request.
func (server *Server) SubmitGateDecision(ctx context.Context, req *governancev1.SubmitGateDecisionRequest) (*governancev1.GateDecisionResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	gateRequestID, err := requiredUUID(req.GetGateRequestId())
	if err != nil {
		return nil, err
	}
	decision, request, err := server.service.SubmitGateDecision(ctx, governanceservice.SubmitGateDecisionInput{
		GateRequestID:          gateRequestID,
		DecisionActorRef:       req.GetDecisionActorRef(),
		DecisionPolicyRef:      req.GetDecisionPolicyRef(),
		Outcome:                gateOutcome(req.GetOutcome()),
		Reason:                 req.GetReason(),
		ConditionsSummary:      req.GetConditionsSummary(),
		InteractionDeliveryRef: interactionDeliveryRef(req.GetInteractionDeliveryRef()),
		SourceRef:              req.GetInteractionDeliveryRef().GetDecisionRef(),
		Meta:                   meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.GateDecisionResponse{GateDecision: toGateDecision(decision), GateRequest: toGateRequest(request)}, nil
}

// CancelGate cancels an open governance gate request.
func (server *Server) CancelGate(ctx context.Context, req *governancev1.CancelGateRequest) (*governancev1.GateRequestResponse, error) {
	return server.terminalGateCommandResponse(ctx, req.GetGateRequestId(), req.GetReason(), req.GetInteractionDeliveryRef(), req.GetMeta(), server.cancelGate)
}

// ExpireGate expires an open governance gate request.
func (server *Server) ExpireGate(ctx context.Context, req *governancev1.ExpireGateRequest) (*governancev1.GateRequestResponse, error) {
	response, err := server.terminalGateCommandResponse(ctx, req.GetGateRequestId(), req.GetReason(), req.GetInteractionDeliveryRef(), req.GetMeta(), server.expireGate)
	return response, err
}

// GetGateDecision returns one final governance gate decision.
func (server *Server) GetGateDecision(ctx context.Context, req *governancev1.GetGateDecisionRequest) (*governancev1.GateDecisionResponse, error) {
	meta, err := queryMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(req.GetGateDecisionId())
	if err != nil {
		return nil, err
	}
	decision, err := server.service.GetGateDecision(ctx, governanceservice.GetGateDecisionInput{GateDecisionID: id, Meta: meta})
	if err != nil {
		return nil, err
	}
	return &governancev1.GateDecisionResponse{GateDecision: toGateDecision(decision)}, nil
}

// ListGateDecisions returns gate decisions by gate request, target or outcome.
func (server *Server) ListGateDecisions(ctx context.Context, req *governancev1.ListGateDecisionsRequest) (*governancev1.ListGateDecisionsResponse, error) {
	meta, err := queryMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	gateRequestID, err := optionalUUID(req.GetGateRequestId())
	if err != nil {
		return nil, err
	}
	items, page, err := server.service.ListGateDecisions(ctx, governanceservice.ListGateDecisionsInput{
		Filter: query.GateDecisionFilter{
			GateRequestID: gateRequestID,
			Target:        targetRef(req.GetTarget()),
			Outcome:       gateOutcome(req.GetOutcome()),
			Page:          pageRequest(req.GetPage()),
		},
		Meta: meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ListGateDecisionsResponse{GateDecisions: toGateDecisions(items), Page: pageResponse(page)}, nil
}

// GetGateRequest returns one gate request.
func (server *Server) GetGateRequest(ctx context.Context, req *governancev1.GetGateRequestRequest) (*governancev1.GateRequestResponse, error) {
	meta, err := queryMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(req.GetGateRequestId())
	if err != nil {
		return nil, err
	}
	request, err := server.service.GetGateRequest(ctx, governanceservice.GetGateRequestInput{GateRequestID: id, Meta: meta})
	if err != nil {
		return nil, err
	}
	response := &governancev1.GateRequestResponse{GateRequest: toGateRequest(request)}
	if req.GetIncludeDecision() {
		decisions, _, err := server.service.ListGateDecisions(ctx, governanceservice.ListGateDecisionsInput{Filter: query.GateDecisionFilter{GateRequestID: &id, Page: query.PageRequest{PageSize: 1}}, Meta: meta})
		if err != nil {
			return nil, err
		}
		if len(decisions) > 0 {
			response.GateDecision = toGateDecision(decisions[0])
		}
	}
	return response, nil
}

// ListGateRequests returns gate requests by target or status.
func (server *Server) ListGateRequests(ctx context.Context, req *governancev1.ListGateRequestsRequest) (*governancev1.ListGateRequestsResponse, error) {
	meta, err := queryMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	riskAssessmentID, err := optionalUUID(req.GetRiskAssessmentId())
	if err != nil {
		return nil, err
	}
	items, page, err := server.service.ListGateRequests(ctx, governanceservice.ListGateRequestsInput{
		Filter: query.GateRequestFilter{
			RiskAssessmentID: riskAssessmentID,
			Target:           targetRef(req.GetTarget()),
			Status:           gateRequestStatus(req.GetStatus()),
			Page:             pageRequest(req.GetPage()),
		},
		Meta: meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ListGateRequestsResponse{GateRequests: toGateRequests(items), Page: pageResponse(page)}, nil
}

// BuildReleaseDecisionPackage stores bounded release evidence refs.
func (server *Server) BuildReleaseDecisionPackage(ctx context.Context, req *governancev1.BuildReleaseDecisionPackageRequest) (*governancev1.ReleaseDecisionPackageResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	providers, err := providerRefs(req.GetProviderRefs())
	if err != nil {
		return nil, err
	}
	runtimes, err := runtimeRefs(req.GetRuntimeRefs())
	if err != nil {
		return nil, err
	}
	agentContext, err := protoObject(req.GetAgentContext())
	if err != nil {
		return nil, err
	}
	reviewSignalIDs, err := reviewSignalIDs(req.GetReviewSignalIds())
	if err != nil {
		return nil, err
	}
	item, err := server.service.BuildReleaseDecisionPackage(ctx, governanceservice.BuildReleaseDecisionPackageInput{
		ReleaseCandidateRef:     req.GetReleaseCandidateRef(),
		ProjectContext:          projectContext(req.GetProjectContext()),
		RepositoryRefs:          req.GetRepositoryRefs(),
		ProviderRefs:            providers,
		RuntimeRefs:             runtimes,
		AgentContext:            agentContext,
		ReviewSignalIDs:         reviewSignalIDs,
		EvidenceRefs:            evidenceRefs(req.GetEvidenceRefs()),
		KnownLimitationsSummary: req.GetKnownLimitationsSummary(),
		Meta:                    meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ReleaseDecisionPackageResponse{ReleaseDecisionPackage: toReleaseDecisionPackage(item)}, nil
}

// GetReleaseDecisionPackage returns one release decision package.
func (server *Server) GetReleaseDecisionPackage(ctx context.Context, req *governancev1.GetReleaseDecisionPackageRequest) (*governancev1.ReleaseDecisionPackageResponse, error) {
	id, err := requiredUUID(req.GetReleaseDecisionPackageId())
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetReleaseDecisionPackage(ctx, id)
	if err != nil {
		return nil, err
	}
	return &governancev1.ReleaseDecisionPackageResponse{ReleaseDecisionPackage: toReleaseDecisionPackage(item)}, nil
}

// ListReleaseDecisionPackages returns release packages by project, candidate or status.
func (server *Server) ListReleaseDecisionPackages(ctx context.Context, req *governancev1.ListReleaseDecisionPackagesRequest) (*governancev1.ListReleaseDecisionPackagesResponse, error) {
	items, page, err := server.service.ListReleaseDecisionPackages(ctx, governanceservice.ListReleaseDecisionPackagesInput{
		Filter: query.ReleaseDecisionPackageFilter{
			ProjectContext:      projectContext(req.GetProjectContext()),
			ReleaseCandidateRef: req.GetReleaseCandidateRef(),
			Status:              releaseDecisionPackageStatus(req.GetStatus()),
			Page:                pageRequest(req.GetPage()),
		},
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ListReleaseDecisionPackagesResponse{ReleaseDecisionPackages: toReleaseDecisionPackages(items), Page: pageResponse(page)}, nil
}

// RequestReleaseDecision is a stable contract handler reserved for GOV-6 release decisions.
func (server *Server) RequestReleaseDecision(ctx context.Context, _ *governancev1.RequestReleaseDecisionRequest) (*governancev1.ReleaseDecisionResponse, error) {
	return nil, server.backlog(ctx, enum.OperationRequestReleaseDecision)
}

// SubmitReleaseDecision is a stable contract handler reserved for GOV-6 release decisions.
func (server *Server) SubmitReleaseDecision(ctx context.Context, _ *governancev1.SubmitReleaseDecisionRequest) (*governancev1.ReleaseDecisionResponse, error) {
	return nil, server.backlog(ctx, enum.OperationSubmitReleaseDecision)
}

// GetReleaseDecision is a stable contract handler reserved for GOV-6 release decision reads.
func (server *Server) GetReleaseDecision(ctx context.Context, _ *governancev1.GetReleaseDecisionRequest) (*governancev1.ReleaseDecisionResponse, error) {
	return nil, server.backlog(ctx, enum.OperationGetReleaseDecision)
}

// ListReleaseDecisions is a stable contract handler reserved for GOV-6 release decision reads.
func (server *Server) ListReleaseDecisions(ctx context.Context, _ *governancev1.ListReleaseDecisionsRequest) (*governancev1.ListReleaseDecisionsResponse, error) {
	return nil, server.backlog(ctx, enum.OperationListReleaseDecisions)
}

// RecordBlockingSignal is a stable contract handler reserved for GOV-5 blocking signals.
func (server *Server) RecordBlockingSignal(ctx context.Context, _ *governancev1.RecordBlockingSignalRequest) (*governancev1.BlockingSignalResponse, error) {
	return nil, server.backlog(ctx, enum.OperationRecordBlockingSignal)
}

// ResolveBlockingSignal is a stable contract handler reserved for GOV-5 blocking signals.
func (server *Server) ResolveBlockingSignal(ctx context.Context, _ *governancev1.ResolveBlockingSignalRequest) (*governancev1.BlockingSignalResponse, error) {
	return nil, server.backlog(ctx, enum.OperationResolveBlockingSignal)
}

// ListBlockingSignals is a stable contract handler reserved for GOV-5 blocking signal reads.
func (server *Server) ListBlockingSignals(ctx context.Context, _ *governancev1.ListBlockingSignalsRequest) (*governancev1.ListBlockingSignalsResponse, error) {
	return nil, server.backlog(ctx, enum.OperationListBlockingSignals)
}

// RecordReleaseSafetyState is a stable contract handler reserved for GOV-6 release safety-loop.
func (server *Server) RecordReleaseSafetyState(ctx context.Context, _ *governancev1.RecordReleaseSafetyStateRequest) (*governancev1.ReleaseSafetyStateResponse, error) {
	return nil, server.backlog(ctx, enum.OperationRecordReleaseSafetyState)
}

// GetReleaseSafetyState is a stable contract handler reserved for GOV-6 release safety-loop reads.
func (server *Server) GetReleaseSafetyState(ctx context.Context, _ *governancev1.GetReleaseSafetyStateRequest) (*governancev1.ReleaseSafetyStateResponse, error) {
	return nil, server.backlog(ctx, enum.OperationGetReleaseSafetyState)
}

func (server *Server) backlog(ctx context.Context, operation enum.Operation) error {
	return server.service.BacklogOperation(ctx, governanceservice.BacklogOperationInput{Operation: operation})
}
