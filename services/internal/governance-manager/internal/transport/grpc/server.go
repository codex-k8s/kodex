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
	ReevaluateRisk(context.Context, governanceservice.ReevaluateRiskInput) (entity.RiskAssessment, error)
	GetRiskAssessment(context.Context, governanceservice.GetRiskAssessmentInput) (entity.RiskAssessment, error)
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
	RecordReleaseRuntimeEvidence(context.Context, governanceservice.RecordReleaseRuntimeEvidenceInput) (entity.ReleaseDecisionPackage, error)
	RecordReleaseAgentEvidence(context.Context, governanceservice.RecordReleaseAgentEvidenceInput) (entity.ReleaseDecisionPackage, error)
	GetReleaseDecisionPackage(context.Context, governanceservice.GetReleaseDecisionPackageInput) (entity.ReleaseDecisionPackage, error)
	ListReleaseDecisionPackages(context.Context, governanceservice.ListReleaseDecisionPackagesInput) ([]entity.ReleaseDecisionPackage, query.PageResult, error)
	RequestReleaseDecision(context.Context, governanceservice.RequestReleaseDecisionInput) (entity.ReleaseDecision, entity.ReleaseDecisionPackage, error)
	SubmitReleaseDecision(context.Context, governanceservice.SubmitReleaseDecisionInput) (entity.ReleaseDecision, entity.ReleaseDecisionPackage, error)
	GetReleaseDecision(context.Context, governanceservice.GetReleaseDecisionInput) (entity.ReleaseDecision, error)
	ListReleaseDecisions(context.Context, governanceservice.ListReleaseDecisionsInput) ([]entity.ReleaseDecision, query.PageResult, error)
	RecordBlockingSignal(context.Context, governanceservice.RecordBlockingSignalInput) (entity.BlockingSignal, error)
	ResolveBlockingSignal(context.Context, governanceservice.ResolveBlockingSignalInput) (entity.BlockingSignal, error)
	ListBlockingSignals(context.Context, governanceservice.ListBlockingSignalsInput) ([]entity.BlockingSignal, query.PageResult, error)
	RecordReleaseSafetyState(context.Context, governanceservice.RecordReleaseSafetyStateInput) (entity.ReleaseSafetyState, error)
	GetReleaseSafetyState(context.Context, governanceservice.GetReleaseSafetyStateInput) (entity.ReleaseSafetyState, error)
	GetGovernanceSummary(context.Context, governanceservice.GetGovernanceSummaryInput) (entity.GovernanceSummary, error)
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

func commandMetaAndID(meta *governancev1.CommandMeta, rawID string) (governanceservice.CommandMeta, uuid.UUID, error) {
	metaValue, err := commandMeta(meta)
	return requiredMetaID(metaValue, err, rawID)
}

func queryMetaAndID(meta *governancev1.QueryMeta, rawID string) (governanceservice.QueryMeta, uuid.UUID, error) {
	metaValue, err := queryMeta(meta)
	return requiredMetaID(metaValue, err, rawID)
}

func requiredMetaID[T any](metaValue T, err error, rawID string) (T, uuid.UUID, error) {
	if err != nil {
		var zero T
		return zero, uuid.Nil, err
	}
	id, err := requiredUUID(rawID)
	if err != nil {
		var zero T
		return zero, uuid.Nil, err
	}
	return metaValue, id, nil
}

func queryMetaAndOptionalID(meta *governancev1.QueryMeta, rawID string) (governanceservice.QueryMeta, *uuid.UUID, error) {
	metaValue, err := queryMeta(meta)
	if err != nil {
		return governanceservice.QueryMeta{}, nil, err
	}
	id, err := optionalUUID(rawID)
	if err != nil {
		return governanceservice.QueryMeta{}, nil, err
	}
	return metaValue, id, nil
}

func queryIDResponse[T any, R any](ctx context.Context, metaValue *governancev1.QueryMeta, rawID string, load func(context.Context, governanceservice.QueryMeta, uuid.UUID) (T, error), response func(T) R) (R, error) {
	meta, id, err := queryMetaAndID(metaValue, rawID)
	if err != nil {
		var zero R
		return zero, err
	}
	item, err := load(ctx, meta, id)
	if err != nil {
		var zero R
		return zero, err
	}
	return response(item), nil
}

func listOptionalIDResponse[T any, R any](ctx context.Context, metaValue *governancev1.QueryMeta, rawID string, load func(context.Context, governanceservice.QueryMeta, *uuid.UUID) ([]T, query.PageResult, error), response func([]T, query.PageResult) R) (R, error) {
	meta, id, err := queryMetaAndOptionalID(metaValue, rawID)
	if err != nil {
		var zero R
		return zero, err
	}
	items, page, err := load(ctx, meta, id)
	if err != nil {
		var zero R
		return zero, err
	}
	return response(items, page), nil
}

func terminalGateCommand(gateRequestID string, reason string, ref *governancev1.InteractionDeliveryRef, meta *governancev1.CommandMeta) (terminalGateCommandInput, error) {
	metaValue, id, err := commandMetaAndID(meta, gateRequestID)
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
	meta, riskProfileID, err := commandMetaAndID(req.GetMeta(), req.GetRiskProfileId())
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
	meta, riskProfileID, err := commandMetaAndID(req.GetMeta(), req.GetRiskProfileId())
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
	return server.archiveRiskProfileResponse(ctx, req)
}

func (server *Server) archiveRiskProfileResponse(ctx context.Context, req *governancev1.ArchiveRiskProfileRequest) (*governancev1.RiskProfileResponse, error) {
	meta, riskProfileID, err := commandMetaAndID(req.GetMeta(), req.GetRiskProfileId())
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
	filter, err := gatePolicyFilter(req)
	if err != nil {
		return nil, err
	}
	items, page, err := server.service.ListGatePolicies(ctx, governanceservice.ListGatePoliciesInput{
		Filter: filter,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ListGatePoliciesResponse{GatePolicies: toGatePolicies(items), Page: pageResponse(page)}, nil
}

func gatePolicyFilter(req *governancev1.ListGatePoliciesRequest) (query.GatePolicyFilter, error) {
	id, err := requiredUUID(req.GetRiskProfileId())
	if err != nil {
		return query.GatePolicyFilter{}, err
	}
	return query.GatePolicyFilter{
		RiskProfileID:  id,
		ProfileVersion: req.GetProfileVersion(),
		GateKind:       gateKind(req.GetGateKind()),
		Status:         ruleStatus(req.GetStatus()),
		Page:           pageRequest(req.GetPage()),
	}, nil
}

// EvaluateRisk stores a deterministic assessment produced from safe summaries and refs.
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
		Target:            targetRef(req.GetTarget()),
		ProjectContext:    projectContext(req.GetProjectContext()),
		ProviderContext:   providerContext,
		AgentContext:      agentContext,
		RuntimeContext:    runtimeContext,
		EvidenceRefs:      evidenceRefs(req.GetEvidenceRefs()),
		RiskProfileRef:    req.GetRiskProfileRef(),
		EvaluationSummary: riskEvaluationSummary(req.GetEvaluationSummary()),
		Meta:              meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskAssessmentResponse{RiskAssessment: toRiskAssessment(assessment)}, nil
}

// ReevaluateRisk recalculates a stored assessment with optimistic concurrency.
func (server *Server) ReevaluateRisk(ctx context.Context, req *governancev1.ReevaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	assessmentID, err := requiredUUID(req.GetRiskAssessmentId())
	if err != nil {
		return nil, err
	}
	assessment, err := server.service.ReevaluateRisk(ctx, governanceservice.ReevaluateRiskInput{
		RiskAssessmentID:  assessmentID,
		NewEvidenceRefs:   evidenceRefs(req.GetNewEvidenceRefs()),
		Reason:            req.GetReevaluationReason(),
		EvaluationSummary: riskEvaluationSummary(req.GetEvaluationSummary()),
		RiskProfileRef:    req.GetRiskProfileRef(),
		Meta:              meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.RiskAssessmentResponse{RiskAssessment: toRiskAssessment(assessment)}, nil
}

// GetRiskAssessment returns one assessment.
func (server *Server) GetRiskAssessment(ctx context.Context, req *governancev1.GetRiskAssessmentRequest) (*governancev1.RiskAssessmentResponse, error) {
	return server.riskAssessmentByIDResponse(ctx, req.GetRiskAssessmentId(), req.GetMeta(), req.GetIncludeFactors(), req.GetIncludeReviewSignals())
}

func (server *Server) riskAssessmentByIDResponse(ctx context.Context, riskAssessmentID string, metaValue *governancev1.QueryMeta, includeFactors bool, includeReviewSignals bool) (*governancev1.RiskAssessmentResponse, error) {
	meta, err := queryMeta(metaValue)
	if err != nil {
		return nil, err
	}
	id, err := requiredUUID(riskAssessmentID)
	if err != nil {
		return nil, err
	}
	assessment, err := server.service.GetRiskAssessment(ctx, governanceservice.GetRiskAssessmentInput{RiskAssessmentID: id, Meta: meta})
	if err != nil {
		return nil, err
	}
	response := &governancev1.RiskAssessmentResponse{RiskAssessment: toRiskAssessment(assessment)}
	if includeFactors {
		factors, _, err := server.service.ListRiskFactors(ctx, governanceservice.ListRiskFactorsInput{
			Filter: query.RiskFactorFilter{RiskAssessmentID: id},
			Meta:   meta,
		})
		if err != nil {
			return nil, err
		}
		response.RiskFactors = toRiskFactors(factors)
	}
	if includeReviewSignals {
		signals, _, err := server.service.ListReviewSignals(ctx, governanceservice.ListReviewSignalsInput{
			Filter: query.ReviewSignalFilter{RiskAssessmentID: &id},
			Meta:   meta,
		})
		if err != nil {
			return nil, err
		}
		response.ReviewSignals = toReviewSignals(signals)
	}
	return response, nil
}

// ListRiskAssessments returns assessments by target, project, risk class or status.
func (server *Server) ListRiskAssessments(ctx context.Context, req *governancev1.ListRiskAssessmentsRequest) (*governancev1.ListRiskAssessmentsResponse, error) {
	meta, err := queryMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	items, page, err := server.service.ListRiskAssessments(ctx, governanceservice.ListRiskAssessmentsInput{
		Filter: query.RiskAssessmentFilter{
			Target:             targetRef(req.GetTarget()),
			ProjectContext:     projectContext(req.GetProjectContext()),
			EffectiveRiskClass: riskClass(req.GetEffectiveRiskClass()),
			Status:             riskAssessmentStatus(req.GetStatus()),
			Page:               pageRequest(req.GetPage()),
		},
		Meta: meta,
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
	meta, err := queryMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
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
		Meta: meta,
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
	filter := query.ReviewSignalFilter{
		Target:   targetRef(req.GetTarget()),
		RoleKind: reviewRoleKind(req.GetRoleKind()),
		Outcome:  reviewSignalOutcome(req.GetOutcome()),
		Page:     pageRequest(req.GetPage()),
	}
	return listOptionalIDResponse(ctx, req.GetMeta(), req.GetRiskAssessmentId(),
		func(ctx context.Context, meta governanceservice.QueryMeta, riskAssessmentID *uuid.UUID) ([]entity.ReviewSignal, query.PageResult, error) {
			filter.RiskAssessmentID = riskAssessmentID
			return server.service.ListReviewSignals(ctx, governanceservice.ListReviewSignalsInput{
				Filter: filter,
				Meta:   meta,
			})
		},
		func(items []entity.ReviewSignal, page query.PageResult) *governancev1.ListReviewSignalsResponse {
			return &governancev1.ListReviewSignalsResponse{ReviewSignals: toReviewSignals(items), Page: pageResponse(page)}
		},
	)
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
	gateRequestID, err := requiredUUID(req.GetGateRequestId())
	if err != nil {
		return nil, err
	}
	decision, err := server.service.GetGateDecision(ctx, governanceservice.GetGateDecisionInput{GateDecisionID: id, GateRequestID: gateRequestID, Meta: meta})
	if err != nil {
		return nil, err
	}
	return &governancev1.GateDecisionResponse{GateDecision: toGateDecision(decision)}, nil
}

// ListGateDecisions returns gate decisions by gate request or target, optionally refined by outcome.
func (server *Server) ListGateDecisions(ctx context.Context, req *governancev1.ListGateDecisionsRequest) (*governancev1.ListGateDecisionsResponse, error) {
	return listOptionalIDResponse(ctx, req.GetMeta(), req.GetGateRequestId(),
		func(ctx context.Context, meta governanceservice.QueryMeta, gateRequestID *uuid.UUID) ([]entity.GateDecision, query.PageResult, error) {
			return server.service.ListGateDecisions(ctx, governanceservice.ListGateDecisionsInput{
				Filter: query.GateDecisionFilter{
					GateRequestID: gateRequestID,
					Target:        targetRef(req.GetTarget()),
					Outcome:       gateOutcome(req.GetOutcome()),
					Page:          pageRequest(req.GetPage()),
				},
				Meta: meta,
			})
		},
		func(items []entity.GateDecision, page query.PageResult) *governancev1.ListGateDecisionsResponse {
			return &governancev1.ListGateDecisionsResponse{GateDecisions: toGateDecisions(items), Page: pageResponse(page)}
		},
	)
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

// ListGateRequests returns gate requests by target or assessment, optionally refined by status.
func (server *Server) ListGateRequests(ctx context.Context, req *governancev1.ListGateRequestsRequest) (*governancev1.ListGateRequestsResponse, error) {
	meta, riskAssessmentID, err := queryMetaAndOptionalID(req.GetMeta(), req.GetRiskAssessmentId())
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
	response := &governancev1.ListGateRequestsResponse{Page: pageResponse(page)}
	response.GateRequests = toGateRequests(items)
	return response, nil
}

// BuildReleaseDecisionPackage stores bounded release evidence refs.
func (server *Server) BuildReleaseDecisionPackage(ctx context.Context, req *governancev1.BuildReleaseDecisionPackageRequest) (*governancev1.ReleaseDecisionPackageResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	riskAssessmentID, err := optionalUUID(req.GetRiskAssessmentId())
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
		RiskAssessmentID:        riskAssessmentID,
		ProviderRefs:            providers,
		RuntimeRefs:             runtimes,
		AgentContext:            agentContext,
		ReviewSignalIDs:         reviewSignalIDs,
		EvidenceRefs:            evidenceRefs(req.GetEvidenceRefs()),
		IntegrationRefs:         releaseIntegrationRefs(req.GetIntegrationRefs()),
		KnownLimitationsSummary: req.GetKnownLimitationsSummary(),
		Meta:                    meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ReleaseDecisionPackageResponse{ReleaseDecisionPackage: toReleaseDecisionPackage(item)}, nil
}

// RecordReleaseRuntimeEvidence appends safe runtime/deploy refs to a release package.
func (server *Server) RecordReleaseRuntimeEvidence(ctx context.Context, req *governancev1.RecordReleaseRuntimeEvidenceRequest) (*governancev1.ReleaseDecisionPackageResponse, error) {
	payload := parseRuntimeEvidencePayload(req.GetRuntimeRefs())
	return server.recordReleaseEvidenceParsedResponse(ctx, req.GetMeta(), req.GetReleaseDecisionPackageId(), payload, req.GetEvidenceRefs(), req.GetIntegrationRefs(), releaseEvidenceTargetRuntime)
}

// RecordReleaseAgentEvidence appends safe agent evidence refs to a release package.
func (server *Server) RecordReleaseAgentEvidence(ctx context.Context, req *governancev1.RecordReleaseAgentEvidenceRequest) (*governancev1.ReleaseDecisionPackageResponse, error) {
	return server.recordReleaseEvidenceParsedResponse(ctx, req.GetMeta(), req.GetReleaseDecisionPackageId(), parseAgentEvidencePayload(req.GetAgentContext()), req.GetEvidenceRefs(), req.GetIntegrationRefs(), releaseEvidenceTargetAgent)
}

type releaseEvidenceTarget int

const (
	releaseEvidenceTargetRuntime releaseEvidenceTarget = iota + 1
	releaseEvidenceTargetAgent
)

type releaseEvidencePayload struct {
	value []byte
	err   error
}

func parseRuntimeEvidencePayload(items []*governancev1.RuntimeContextRef) releaseEvidencePayload {
	payload, err := runtimeRefs(items)
	return releaseEvidencePayload{value: payload, err: err}
}

func parseAgentEvidencePayload(item *governancev1.AgentContextRef) releaseEvidencePayload {
	payload, err := protoObject(item)
	return releaseEvidencePayload{value: payload, err: err}
}

func (server *Server) recordReleaseEvidenceParsedResponse(
	ctx context.Context,
	metaRequest *governancev1.CommandMeta,
	packageIDValue string,
	payload releaseEvidencePayload,
	evidenceRefMessages []*governancev1.EvidenceRef,
	integrationRefMessages []*governancev1.ReleaseIntegrationRef,
	target releaseEvidenceTarget,
) (*governancev1.ReleaseDecisionPackageResponse, error) {
	if payload.err != nil {
		return nil, payload.err
	}
	return server.recordReleaseEvidenceResponse(ctx, metaRequest, packageIDValue, payload.value, evidenceRefMessages, integrationRefMessages, target)
}

func (server *Server) recordReleaseEvidenceResponse(
	ctx context.Context,
	metaRequest *governancev1.CommandMeta,
	packageIDValue string,
	payload []byte,
	evidenceRefMessages []*governancev1.EvidenceRef,
	integrationRefMessages []*governancev1.ReleaseIntegrationRef,
	target releaseEvidenceTarget,
) (*governancev1.ReleaseDecisionPackageResponse, error) {
	return server.releasePackageCommandResponse(ctx, metaRequest, packageIDValue, func(meta governanceservice.CommandMeta, packageID uuid.UUID) (entity.ReleaseDecisionPackage, error) {
		refs := evidenceRefs(evidenceRefMessages)
		integrationRefs := releaseIntegrationRefs(integrationRefMessages)
		switch target {
		case releaseEvidenceTargetRuntime:
			return server.service.RecordReleaseRuntimeEvidence(ctx, governanceservice.RecordReleaseRuntimeEvidenceInput{
				ReleaseDecisionPackageID: packageID,
				RuntimeRefs:              payload,
				EvidenceRefs:             refs,
				IntegrationRefs:          integrationRefs,
				Meta:                     meta,
			})
		case releaseEvidenceTargetAgent:
			return server.service.RecordReleaseAgentEvidence(ctx, governanceservice.RecordReleaseAgentEvidenceInput{
				ReleaseDecisionPackageID: packageID,
				AgentContext:             payload,
				EvidenceRefs:             refs,
				IntegrationRefs:          integrationRefs,
				Meta:                     meta,
			})
		default:
			panic("unsupported release evidence target")
		}
	})
}

func (server *Server) releasePackageCommandResponse(ctx context.Context, metaRequest *governancev1.CommandMeta, packageIDValue string, call func(governanceservice.CommandMeta, uuid.UUID) (entity.ReleaseDecisionPackage, error)) (*governancev1.ReleaseDecisionPackageResponse, error) {
	meta, packageID, err := commandMetaAndID(metaRequest, packageIDValue)
	if err != nil {
		return nil, err
	}
	item, err := call(meta, packageID)
	if err != nil {
		return nil, err
	}
	return &governancev1.ReleaseDecisionPackageResponse{ReleaseDecisionPackage: toReleaseDecisionPackage(item)}, nil
}

// GetReleaseDecisionPackage returns one release decision package.
func (server *Server) GetReleaseDecisionPackage(ctx context.Context, req *governancev1.GetReleaseDecisionPackageRequest) (*governancev1.ReleaseDecisionPackageResponse, error) {
	return queryIDResponse(ctx, req.GetMeta(), req.GetReleaseDecisionPackageId(),
		func(ctx context.Context, meta governanceservice.QueryMeta, id uuid.UUID) (entity.ReleaseDecisionPackage, error) {
			return server.service.GetReleaseDecisionPackage(ctx, governanceservice.GetReleaseDecisionPackageInput{ReleaseDecisionPackageID: id, Meta: meta})
		},
		func(item entity.ReleaseDecisionPackage) *governancev1.ReleaseDecisionPackageResponse {
			return &governancev1.ReleaseDecisionPackageResponse{ReleaseDecisionPackage: toReleaseDecisionPackage(item)}
		},
	)
}

// ListReleaseDecisionPackages returns release packages by project, candidate or status.
func (server *Server) ListReleaseDecisionPackages(ctx context.Context, req *governancev1.ListReleaseDecisionPackagesRequest) (*governancev1.ListReleaseDecisionPackagesResponse, error) {
	meta, err := queryMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	items, page, err := server.service.ListReleaseDecisionPackages(ctx, governanceservice.ListReleaseDecisionPackagesInput{
		Filter: query.ReleaseDecisionPackageFilter{
			ProjectContext:      projectContext(req.GetProjectContext()),
			ReleaseCandidateRef: req.GetReleaseCandidateRef(),
			Status:              releaseDecisionPackageStatus(req.GetStatus()),
			Page:                pageRequest(req.GetPage()),
		},
		Meta: meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ListReleaseDecisionPackagesResponse{ReleaseDecisionPackages: toReleaseDecisionPackages(items), Page: pageResponse(page)}, nil
}

// RequestReleaseDecision starts release decision lifecycle.
func (server *Server) RequestReleaseDecision(ctx context.Context, req *governancev1.RequestReleaseDecisionRequest) (*governancev1.ReleaseDecisionResponse, error) {
	meta, packageID, err := commandMetaAndID(req.GetMeta(), req.GetReleaseDecisionPackageId())
	if err != nil {
		return nil, err
	}
	decision, pkg, err := server.service.RequestReleaseDecision(ctx, governanceservice.RequestReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		RequestGateIfRequired:    req.GetRequestGateIfRequired(),
		Meta:                     meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ReleaseDecisionResponse{ReleaseDecision: toReleaseDecision(decision), ReleaseDecisionPackage: toReleaseDecisionPackage(pkg)}, nil
}

// SubmitReleaseDecision resolves a release decision.
func (server *Server) SubmitReleaseDecision(ctx context.Context, req *governancev1.SubmitReleaseDecisionRequest) (*governancev1.ReleaseDecisionResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	packageID, err := requiredUUID(req.GetReleaseDecisionPackageId())
	if err != nil {
		return nil, err
	}
	gateDecisionID, err := optionalUUID(req.GetGateDecisionId())
	if err != nil {
		return nil, err
	}
	decision, pkg, err := server.service.SubmitReleaseDecision(ctx, governanceservice.SubmitReleaseDecisionInput{
		ReleaseDecisionPackageID: packageID,
		GateDecisionID:           gateDecisionID,
		Outcome:                  releaseDecisionOutcome(req.GetOutcome()),
		DecisionActorRef:         req.GetDecisionActorRef(),
		DecisionPolicyRef:        req.GetDecisionPolicyRef(),
		Reason:                   req.GetReason(),
		ConditionsSummary:        req.GetConditionsSummary(),
		Meta:                     meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ReleaseDecisionResponse{ReleaseDecision: toReleaseDecision(decision), ReleaseDecisionPackage: toReleaseDecisionPackage(pkg)}, nil
}

// GetReleaseDecision returns one release decision.
func (server *Server) GetReleaseDecision(ctx context.Context, req *governancev1.GetReleaseDecisionRequest) (*governancev1.ReleaseDecisionResponse, error) {
	meta, id, err := queryMetaAndID(req.GetMeta(), req.GetReleaseDecisionId())
	if err != nil {
		return nil, err
	}
	return server.releaseDecisionResponse(ctx, id, meta)
}

func (server *Server) releaseDecisionResponse(ctx context.Context, id uuid.UUID, meta governanceservice.QueryMeta) (*governancev1.ReleaseDecisionResponse, error) {
	decision, err := server.service.GetReleaseDecision(ctx, governanceservice.GetReleaseDecisionInput{ReleaseDecisionID: id, Meta: meta})
	if err != nil {
		return nil, err
	}
	return &governancev1.ReleaseDecisionResponse{ReleaseDecision: toReleaseDecision(decision)}, nil
}

// ListReleaseDecisions returns release decisions by package or project context.
func (server *Server) ListReleaseDecisions(ctx context.Context, req *governancev1.ListReleaseDecisionsRequest) (*governancev1.ListReleaseDecisionsResponse, error) {
	meta, packageID, err := queryMetaAndOptionalID(req.GetMeta(), req.GetReleaseDecisionPackageId())
	if err != nil {
		return nil, err
	}
	items, page, err := server.service.ListReleaseDecisions(ctx, governanceservice.ListReleaseDecisionsInput{
		Filter: query.ReleaseDecisionFilter{
			ReleaseDecisionPackageID: packageID,
			ProjectContext:           projectContext(req.GetProjectContext()),
			Status:                   releaseDecisionStatus(req.GetStatus()),
			Outcome:                  releaseDecisionOutcome(req.GetOutcome()),
			Page:                     pageRequest(req.GetPage()),
		},
		Meta: meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ListReleaseDecisionsResponse{ReleaseDecisions: toReleaseDecisions(items), Page: pageResponse(page)}, nil
}

// RecordBlockingSignal records a safe blocking signal.
func (server *Server) RecordBlockingSignal(ctx context.Context, req *governancev1.RecordBlockingSignalRequest) (*governancev1.BlockingSignalResponse, error) {
	meta, err := commandMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	signal, err := server.service.RecordBlockingSignal(ctx, governanceservice.RecordBlockingSignalInput{
		Target:     targetRef(req.GetTarget()),
		SourceType: blockingSignalSourceType(req.GetSourceType()),
		SourceRef:  req.GetSourceRef(),
		Severity:   signalSeverity(req.GetSeverity()),
		Summary:    req.GetSummary(),
		Meta:       meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.BlockingSignalResponse{BlockingSignal: toBlockingSignal(signal)}, nil
}

// ResolveBlockingSignal resolves or dismisses a blocking signal.
func (server *Server) ResolveBlockingSignal(ctx context.Context, req *governancev1.ResolveBlockingSignalRequest) (*governancev1.BlockingSignalResponse, error) {
	meta, id, err := commandMetaAndID(req.GetMeta(), req.GetBlockingSignalId())
	if err != nil {
		return nil, err
	}
	signal, err := server.service.ResolveBlockingSignal(ctx, governanceservice.ResolveBlockingSignalInput{
		BlockingSignalID:  id,
		TerminalStatus:    blockingSignalStatus(req.GetTerminalStatus()),
		ResolutionSummary: req.GetResolutionSummary(),
		Meta:              meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.BlockingSignalResponse{BlockingSignal: toBlockingSignal(signal)}, nil
}

// ListBlockingSignals returns blocking signals by target.
func (server *Server) ListBlockingSignals(ctx context.Context, req *governancev1.ListBlockingSignalsRequest) (*governancev1.ListBlockingSignalsResponse, error) {
	meta, err := queryMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	items, page, err := server.service.ListBlockingSignals(ctx, governanceservice.ListBlockingSignalsInput{
		Filter: query.BlockingSignalFilter{
			Target:   targetRef(req.GetTarget()),
			Status:   blockingSignalStatus(req.GetStatus()),
			Severity: signalSeverity(req.GetSeverity()),
			Page:     pageRequest(req.GetPage()),
		},
		Meta: meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ListBlockingSignalsResponse{BlockingSignals: toBlockingSignals(items), Page: pageResponse(page)}, nil
}

// RecordReleaseSafetyState records current safety-loop state.
func (server *Server) RecordReleaseSafetyState(ctx context.Context, req *governancev1.RecordReleaseSafetyStateRequest) (*governancev1.ReleaseSafetyStateResponse, error) {
	meta, packageID, err := commandMetaAndID(req.GetMeta(), req.GetReleaseDecisionPackageId())
	if err != nil {
		return nil, err
	}
	state, err := server.service.RecordReleaseSafetyState(ctx, governanceservice.RecordReleaseSafetyStateInput{
		ReleaseDecisionPackageID: packageID,
		CurrentState:             releaseSafetyStateKind(req.GetCurrentState()),
		RuntimeJobRef:            req.GetRuntimeJobRef(),
		LastStateReason:          req.GetLastStateReason(),
		Meta:                     meta,
	})
	if err != nil {
		return nil, err
	}
	return &governancev1.ReleaseSafetyStateResponse{ReleaseSafetyState: toReleaseSafetyState(state)}, nil
}

// GetReleaseSafetyState returns current safety-loop state.
func (server *Server) GetReleaseSafetyState(ctx context.Context, req *governancev1.GetReleaseSafetyStateRequest) (*governancev1.ReleaseSafetyStateResponse, error) {
	meta, packageID, err := queryMetaAndID(req.GetMeta(), req.GetReleaseDecisionPackageId())
	if err != nil {
		return nil, err
	}
	state, err := server.service.GetReleaseSafetyState(ctx, governanceservice.GetReleaseSafetyStateInput{ReleaseDecisionPackageID: packageID, Meta: meta})
	if err != nil {
		return nil, err
	}
	response := &governancev1.ReleaseSafetyStateResponse{}
	response.ReleaseSafetyState = toReleaseSafetyState(state)
	return response, nil
}

// GetGovernanceSummary returns a safe owner/staff read model for a scoped governance target.
func (server *Server) GetGovernanceSummary(ctx context.Context, req *governancev1.GetGovernanceSummaryRequest) (*governancev1.GovernanceSummaryResponse, error) {
	meta, err := queryMeta(req.GetMeta())
	if err != nil {
		return nil, err
	}
	scope, err := governanceSummaryScope(req.GetScope())
	if err != nil {
		return nil, err
	}
	summary, err := server.service.GetGovernanceSummary(ctx, governanceservice.GetGovernanceSummaryInput{Scope: scope, Meta: meta})
	if err != nil {
		return nil, err
	}
	return &governancev1.GovernanceSummaryResponse{Summary: toGovernanceSummary(summary)}, nil
}
