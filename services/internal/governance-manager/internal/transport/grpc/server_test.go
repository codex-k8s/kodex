package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/query"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRegisterGovernanceManagerService(t *testing.T) {
	t.Parallel()

	server := grpcruntime.NewServer()
	RegisterGovernanceManagerService(server, &fakeBacklogService{})
}

func TestNewServerRequiresService(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("NewServer(nil) did not panic")
		}
	}()
	_ = NewServer(nil)
}

func TestReevaluateRiskRoutesSafeSummaryToDomainService(t *testing.T) {
	t.Parallel()

	service := &fakeBacklogService{}
	_, err := NewServer(service).ReevaluateRisk(context.Background(), &governancev1.ReevaluateRiskRequest{
		RiskAssessmentId: "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
		EvaluationSummary: &governancev1.RiskEvaluationSummary{
			Summary: "bounded release summary",
			Factors: []*governancev1.RiskEvaluationFactor{{
				SourceType: governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_RELEASE,
				Ref:        "release:stable",
				Summary:    "release policy changed",
				Tags:       []string{"production"},
			}},
		},
		Meta: &governancev1.CommandMeta{Actor: &governancev1.Actor{Type: "service", Id: "provider-hub"}},
	})
	if err != nil {
		t.Fatalf("ReevaluateRisk(): %v", err)
	}
	if service.reevaluateRiskInput.EvaluationSummary.Summary != "bounded release summary" {
		t.Fatalf("summary = %q, want routed summary", service.reevaluateRiskInput.EvaluationSummary.Summary)
	}
	if len(service.reevaluateRiskInput.EvaluationSummary.Factors) != 1 || service.reevaluateRiskInput.EvaluationSummary.Factors[0].SourceType != string(enum.RiskFactorSourceTypeRelease) {
		t.Fatalf("factors = %+v, want one release factor", service.reevaluateRiskInput.EvaluationSummary.Factors)
	}
}

func TestGetRiskAssessmentIncludesFactorsAndReviewSignals(t *testing.T) {
	t.Parallel()

	assessmentID := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	service := &fakeBacklogService{}
	response, err := NewServer(service).GetRiskAssessment(context.Background(), &governancev1.GetRiskAssessmentRequest{
		RiskAssessmentId:     assessmentID,
		IncludeFactors:       true,
		IncludeReviewSignals: true,
		Meta:                 &governancev1.QueryMeta{Actor: &governancev1.Actor{Type: "service", Id: "provider-hub"}},
	})
	if err != nil {
		t.Fatalf("GetRiskAssessment(): %v", err)
	}
	if response.GetRiskAssessment().GetId() != assessmentID {
		t.Fatalf("risk assessment id = %q, want %q", response.GetRiskAssessment().GetId(), assessmentID)
	}
	if len(response.GetRiskFactors()) != 1 {
		t.Fatalf("risk factors = %d, want 1", len(response.GetRiskFactors()))
	}
	if len(response.GetReviewSignals()) != 1 {
		t.Fatalf("review signals = %d, want 1", len(response.GetReviewSignals()))
	}
	if service.riskFactorsInput.Filter.RiskAssessmentID != service.riskAssessmentID {
		t.Fatalf("risk factor filter id = %s, want %s", service.riskFactorsInput.Filter.RiskAssessmentID, service.riskAssessmentID)
	}
	if service.reviewSignalsInput.Filter.RiskAssessmentID == nil || *service.reviewSignalsInput.Filter.RiskAssessmentID != service.riskAssessmentID {
		t.Fatalf("review signal filter id = %v, want %s", service.reviewSignalsInput.Filter.RiskAssessmentID, service.riskAssessmentID)
	}
	if service.riskFactorsInput.Meta.Actor.ID != "provider-hub" || service.reviewSignalsInput.Meta.Actor.ID != "provider-hub" {
		t.Fatalf("meta was not propagated to include queries")
	}
}

func TestRequestReleaseDecisionRoutesToDomainBacklog(t *testing.T) {
	t.Parallel()

	service := &fakeBacklogService{}
	_, err := NewServer(service).RequestReleaseDecision(context.Background(), &governancev1.RequestReleaseDecisionRequest{})
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Fatalf("RequestReleaseDecision() error = %v, want ErrNotImplemented", err)
	}
	if service.operation != enum.OperationRequestReleaseDecision {
		t.Fatalf("operation = %q, want %q", service.operation, enum.OperationRequestReleaseDecision)
	}
}

func TestUnaryErrorInterceptorMapsBacklogToUnimplemented(t *testing.T) {
	t.Parallel()

	interceptor := UnaryErrorInterceptor(nil)
	_, err := interceptor(context.Background(), nil, &grpcruntime.UnaryServerInfo{FullMethod: "/test"}, func(context.Context, any) (any, error) {
		return nil, errs.ErrNotImplemented
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("status code = %s, want %s", status.Code(err), codes.Unimplemented)
	}
}

func TestUnaryErrorInterceptorMapsRepositoryDomainErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{name: "not found", err: errs.ErrNotFound, want: codes.NotFound},
		{name: "already exists", err: errs.ErrAlreadyExists, want: codes.AlreadyExists},
		{name: "conflict", err: errs.ErrConflict, want: codes.Aborted},
		{name: "forbidden", err: errs.ErrForbidden, want: codes.PermissionDenied},
		{name: "precondition failed", err: errs.ErrPreconditionFailed, want: codes.FailedPrecondition},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			interceptor := UnaryErrorInterceptor(nil)
			_, err := interceptor(context.Background(), nil, &grpcruntime.UnaryServerInfo{FullMethod: "/test"}, func(context.Context, any) (any, error) {
				return nil, tt.err
			})
			if status.Code(err) != tt.want {
				t.Fatalf("status code = %s, want %s", status.Code(err), tt.want)
			}
		})
	}
}

type fakeBacklogService struct {
	governanceService
	operation           enum.Operation
	reevaluateRiskInput governanceservice.ReevaluateRiskInput
	riskAssessmentID    uuid.UUID
	riskFactorsInput    governanceservice.ListRiskFactorsInput
	reviewSignalsInput  governanceservice.ListReviewSignalsInput
}

func (service *fakeBacklogService) BacklogOperation(_ context.Context, input governanceservice.BacklogOperationInput) error {
	service.operation = input.Operation
	return errs.ErrNotImplemented
}

func (service *fakeBacklogService) ReevaluateRisk(_ context.Context, input governanceservice.ReevaluateRiskInput) (entity.RiskAssessment, error) {
	service.reevaluateRiskInput = input
	return entity.RiskAssessment{VersionedBase: entity.VersionedBase{ID: input.RiskAssessmentID}}, nil
}

func (service *fakeBacklogService) GetRiskAssessment(_ context.Context, input governanceservice.GetRiskAssessmentInput) (entity.RiskAssessment, error) {
	service.riskAssessmentID = input.RiskAssessmentID
	return entity.RiskAssessment{VersionedBase: entity.VersionedBase{ID: input.RiskAssessmentID}}, nil
}

func (service *fakeBacklogService) ListRiskFactors(_ context.Context, input governanceservice.ListRiskFactorsInput) ([]entity.RiskFactor, query.PageResult, error) {
	service.riskFactorsInput = input
	return []entity.RiskFactor{{
		ID:               uuid.MustParse("bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"),
		RiskAssessmentID: input.Filter.RiskAssessmentID,
		SourceType:       enum.RiskFactorSourceTypeDatabase,
		RiskClass:        enum.RiskClassR2,
		Summary:          "migration risk",
	}}, query.PageResult{}, nil
}

func (service *fakeBacklogService) ListReviewSignals(_ context.Context, input governanceservice.ListReviewSignalsInput) ([]entity.ReviewSignal, query.PageResult, error) {
	service.reviewSignalsInput = input
	riskAssessmentID := input.Filter.RiskAssessmentID
	return []entity.ReviewSignal{{
		ID:               uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
		RiskAssessmentID: riskAssessmentID,
		RoleKind:         enum.ReviewRoleKindSecurity,
		Outcome:          enum.ReviewSignalOutcomePass,
		Summary:          "approved",
	}}, query.PageResult{}, nil
}
