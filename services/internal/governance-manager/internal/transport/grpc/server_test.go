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

func TestReevaluateRiskRoutesToDomainBacklog(t *testing.T) {
	t.Parallel()

	service := &fakeBacklogService{}
	_, err := NewServer(service).ReevaluateRisk(context.Background(), &governancev1.ReevaluateRiskRequest{})
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Fatalf("ReevaluateRisk() error = %v, want ErrNotImplemented", err)
	}
	if service.operation != enum.OperationReevaluateRisk {
		t.Fatalf("operation = %q, want %q", service.operation, enum.OperationReevaluateRisk)
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
	operation enum.Operation
}

func (service *fakeBacklogService) BacklogOperation(_ context.Context, input governanceservice.BacklogOperationInput) error {
	service.operation = input.Operation
	return errs.ErrNotImplemented
}
