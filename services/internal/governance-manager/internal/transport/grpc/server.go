package grpc

import (
	"context"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	grpcruntime "google.golang.org/grpc"
)

var _ governancev1.GovernanceManagerServiceServer = (*Server)(nil)

// Server exposes GovernanceManagerService over gRPC.
type Server struct {
	governancev1.UnimplementedGovernanceManagerServiceServer

	service backlogService
}

type backlogService interface {
	BacklogOperation(context.Context, governanceservice.BacklogOperationInput) error
}

// NewServer creates a governance-manager gRPC server boundary.
func NewServer(service backlogService) *Server {
	if service == nil {
		panic("governance-manager grpc service is required")
	}
	return &Server{service: service}
}

// RegisterGovernanceManagerService registers governance-manager handlers in a gRPC runtime.
func RegisterGovernanceManagerService(registrar grpcruntime.ServiceRegistrar, service backlogService) {
	governancev1.RegisterGovernanceManagerServiceServer(registrar, NewServer(service))
}

// CreateRiskProfile is a stable contract handler reserved for GOV-3+ policy storage.
func (server *Server) CreateRiskProfile(ctx context.Context, _ *governancev1.CreateRiskProfileRequest) (*governancev1.RiskProfileResponse, error) {
	return nil, server.backlog(ctx, enum.OperationCreateRiskProfile)
}

// CreateRiskProfileVersion is a stable contract handler reserved for GOV-3+ policy storage.
func (server *Server) CreateRiskProfileVersion(ctx context.Context, _ *governancev1.CreateRiskProfileVersionRequest) (*governancev1.RiskProfileVersionResponse, error) {
	return nil, server.backlog(ctx, enum.OperationCreateRiskProfileVersion)
}

// ActivateRiskProfileVersion is a stable contract handler reserved for GOV-3+ policy activation.
func (server *Server) ActivateRiskProfileVersion(ctx context.Context, _ *governancev1.ActivateRiskProfileVersionRequest) (*governancev1.RiskProfileVersionResponse, error) {
	return nil, server.backlog(ctx, enum.OperationActivateRiskProfileVersion)
}

// ArchiveRiskProfile is a stable contract handler reserved for GOV-3+ policy storage.
func (server *Server) ArchiveRiskProfile(ctx context.Context, _ *governancev1.ArchiveRiskProfileRequest) (*governancev1.RiskProfileResponse, error) {
	return nil, server.backlog(ctx, enum.OperationArchiveRiskProfile)
}

// GetRiskProfile is a stable contract handler reserved for GOV-3+ policy reads.
func (server *Server) GetRiskProfile(ctx context.Context, _ *governancev1.GetRiskProfileRequest) (*governancev1.RiskProfileResponse, error) {
	return nil, server.backlog(ctx, enum.OperationGetRiskProfile)
}

// GetRiskProfileVersion is a stable contract handler reserved for GOV-3+ policy reads.
func (server *Server) GetRiskProfileVersion(ctx context.Context, _ *governancev1.GetRiskProfileVersionRequest) (*governancev1.RiskProfileVersionResponse, error) {
	return nil, server.backlog(ctx, enum.OperationGetRiskProfileVersion)
}

// ListRiskProfiles is a stable contract handler reserved for GOV-3+ policy reads.
func (server *Server) ListRiskProfiles(ctx context.Context, _ *governancev1.ListRiskProfilesRequest) (*governancev1.ListRiskProfilesResponse, error) {
	return nil, server.backlog(ctx, enum.OperationListRiskProfiles)
}

// ListRiskRules is a stable contract handler reserved for GOV-3+ policy reads.
func (server *Server) ListRiskRules(ctx context.Context, _ *governancev1.ListRiskRulesRequest) (*governancev1.ListRiskRulesResponse, error) {
	return nil, server.backlog(ctx, enum.OperationListRiskRules)
}

// ListGatePolicies is a stable contract handler reserved for GOV-3+ policy reads.
func (server *Server) ListGatePolicies(ctx context.Context, _ *governancev1.ListGatePoliciesRequest) (*governancev1.ListGatePoliciesResponse, error) {
	return nil, server.backlog(ctx, enum.OperationListGatePolicies)
}

// EvaluateRisk is a stable contract handler reserved for GOV-4 risk classification.
func (server *Server) EvaluateRisk(ctx context.Context, _ *governancev1.EvaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error) {
	return nil, server.backlog(ctx, enum.OperationEvaluateRisk)
}

// ReevaluateRisk is a stable contract handler reserved for GOV-4 risk classification.
func (server *Server) ReevaluateRisk(ctx context.Context, _ *governancev1.ReevaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error) {
	return nil, server.backlog(ctx, enum.OperationReevaluateRisk)
}

// GetRiskAssessment is a stable contract handler reserved for GOV-4 risk reads.
func (server *Server) GetRiskAssessment(ctx context.Context, _ *governancev1.GetRiskAssessmentRequest) (*governancev1.RiskAssessmentResponse, error) {
	return nil, server.backlog(ctx, enum.OperationGetRiskAssessment)
}

// ListRiskAssessments is a stable contract handler reserved for GOV-4 risk reads.
func (server *Server) ListRiskAssessments(ctx context.Context, _ *governancev1.ListRiskAssessmentsRequest) (*governancev1.ListRiskAssessmentsResponse, error) {
	return nil, server.backlog(ctx, enum.OperationListRiskAssessments)
}

// ListRiskFactors is a stable contract handler reserved for GOV-4 factor reads.
func (server *Server) ListRiskFactors(ctx context.Context, _ *governancev1.ListRiskFactorsRequest) (*governancev1.ListRiskFactorsResponse, error) {
	return nil, server.backlog(ctx, enum.OperationListRiskFactors)
}

// RecordReviewSignal is a stable contract handler reserved for GOV-5 review signals.
func (server *Server) RecordReviewSignal(ctx context.Context, _ *governancev1.RecordReviewSignalRequest) (*governancev1.ReviewSignalResponse, error) {
	return nil, server.backlog(ctx, enum.OperationRecordReviewSignal)
}

// ListReviewSignals is a stable contract handler reserved for GOV-5 review signal reads.
func (server *Server) ListReviewSignals(ctx context.Context, _ *governancev1.ListReviewSignalsRequest) (*governancev1.ListReviewSignalsResponse, error) {
	return nil, server.backlog(ctx, enum.OperationListReviewSignals)
}

// RequestGate is a stable contract handler reserved for GOV-5 gate lifecycle.
func (server *Server) RequestGate(ctx context.Context, _ *governancev1.RequestGateRequest) (*governancev1.GateRequestResponse, error) {
	return nil, server.backlog(ctx, enum.OperationRequestGate)
}

// SubmitGateDecision is a stable contract handler reserved for GOV-5 gate decisions.
func (server *Server) SubmitGateDecision(ctx context.Context, _ *governancev1.SubmitGateDecisionRequest) (*governancev1.GateDecisionResponse, error) {
	return nil, server.backlog(ctx, enum.OperationSubmitGateDecision)
}

// GetGateDecision is a stable contract handler reserved for GOV-5 gate decision reads.
func (server *Server) GetGateDecision(ctx context.Context, _ *governancev1.GetGateDecisionRequest) (*governancev1.GateDecisionResponse, error) {
	return nil, server.backlog(ctx, enum.OperationGetGateDecision)
}

// ListGateDecisions is a stable contract handler reserved for GOV-5 gate decision reads.
func (server *Server) ListGateDecisions(ctx context.Context, _ *governancev1.ListGateDecisionsRequest) (*governancev1.ListGateDecisionsResponse, error) {
	return nil, server.backlog(ctx, enum.OperationListGateDecisions)
}

// GetGateRequest is a stable contract handler reserved for GOV-5 gate request reads.
func (server *Server) GetGateRequest(ctx context.Context, _ *governancev1.GetGateRequestRequest) (*governancev1.GateRequestResponse, error) {
	return nil, server.backlog(ctx, enum.OperationGetGateRequest)
}

// ListGateRequests is a stable contract handler reserved for GOV-5 gate request reads.
func (server *Server) ListGateRequests(ctx context.Context, _ *governancev1.ListGateRequestsRequest) (*governancev1.ListGateRequestsResponse, error) {
	return nil, server.backlog(ctx, enum.OperationListGateRequests)
}

// BuildReleaseDecisionPackage is a stable contract handler reserved for GOV-6 release evidence.
func (server *Server) BuildReleaseDecisionPackage(ctx context.Context, _ *governancev1.BuildReleaseDecisionPackageRequest) (*governancev1.ReleaseDecisionPackageResponse, error) {
	return nil, server.backlog(ctx, enum.OperationBuildReleaseDecisionPackage)
}

// GetReleaseDecisionPackage is a stable contract handler reserved for GOV-6 release evidence reads.
func (server *Server) GetReleaseDecisionPackage(ctx context.Context, _ *governancev1.GetReleaseDecisionPackageRequest) (*governancev1.ReleaseDecisionPackageResponse, error) {
	return nil, server.backlog(ctx, enum.OperationGetReleaseDecisionPackage)
}

// ListReleaseDecisionPackages is a stable contract handler reserved for GOV-6 release evidence reads.
func (server *Server) ListReleaseDecisionPackages(ctx context.Context, _ *governancev1.ListReleaseDecisionPackagesRequest) (*governancev1.ListReleaseDecisionPackagesResponse, error) {
	return nil, server.backlog(ctx, enum.OperationListReleaseDecisionPackages)
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
