package grpc

import (
	"context"
	"errors"
	"testing"

	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
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

func TestEvaluateRiskRoutesToDomainBacklog(t *testing.T) {
	t.Parallel()

	service := &fakeBacklogService{}
	_, err := NewServer(service).EvaluateRisk(context.Background(), &governancev1.EvaluateRiskRequest{})
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Fatalf("EvaluateRisk() error = %v, want ErrNotImplemented", err)
	}
	if service.operation != enum.OperationEvaluateRisk {
		t.Fatalf("operation = %q, want %q", service.operation, enum.OperationEvaluateRisk)
	}
}

func TestSubmitGateDecisionRoutesToDomainBacklog(t *testing.T) {
	t.Parallel()

	service := &fakeBacklogService{}
	_, err := NewServer(service).SubmitGateDecision(context.Background(), &governancev1.SubmitGateDecisionRequest{})
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Fatalf("SubmitGateDecision() error = %v, want ErrNotImplemented", err)
	}
	if service.operation != enum.OperationSubmitGateDecision {
		t.Fatalf("operation = %q, want %q", service.operation, enum.OperationSubmitGateDecision)
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

type fakeBacklogService struct {
	operation enum.Operation
}

func (service *fakeBacklogService) BacklogOperation(_ context.Context, input governanceservice.BacklogOperationInput) error {
	service.operation = input.Operation
	return errs.ErrNotImplemented
}
