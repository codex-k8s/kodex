package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/google/uuid"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServerEmbedsGeneratedUnimplementedContract(t *testing.T) {
	t.Parallel()

	server := NewServer(fakeService{})
	_, err := server.GetWorkItemProjection(context.Background(), &providersv1.GetWorkItemProjectionRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("GetWorkItemProjection() code = %s, want unimplemented", status.Code(err))
	}
}

func TestRecordProviderLimitSnapshotMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	accountID := uuid.New()
	capturedAt := time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC)
	remaining := int64(42)
	response, err := NewServer(fakeService{}).RecordProviderLimitSnapshot(context.Background(), &providersv1.RecordProviderLimitSnapshotRequest{
		ExternalAccountId: accountID.String(),
		ProviderSlug:      "github",
		LimitClass:        "core",
		Remaining:         &remaining,
		CapturedAt:        capturedAt.Format(time.RFC3339Nano),
		Source:            "provider_hub",
		Meta:              &providersv1.CommandMeta{RequestId: "req-1"},
	})
	if err != nil {
		t.Fatalf("RecordProviderLimitSnapshot(): %v", err)
	}
	if response.GetLimitSnapshot().GetExternalAccountId() != accountID.String() {
		t.Fatalf("external account = %s, want %s", response.GetLimitSnapshot().GetExternalAccountId(), accountID)
	}
	if response.GetLimitSnapshot().GetRemaining() != remaining {
		t.Fatalf("remaining = %d, want %d", response.GetLimitSnapshot().GetRemaining(), remaining)
	}
}

func TestNewServerPanicsWithoutService(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("NewServer() did not panic")
		}
	}()
	_ = NewServer(nil)
}

func TestUnaryErrorInterceptorMapsDomainErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "invalid argument", err: errs.ErrInvalidArgument, code: codes.InvalidArgument},
		{name: "forbidden", err: errs.ErrForbidden, code: codes.PermissionDenied},
		{name: "not found", err: errs.ErrNotFound, code: codes.NotFound},
		{name: "already exists", err: errs.ErrAlreadyExists, code: codes.AlreadyExists},
		{name: "conflict", err: errs.ErrConflict, code: codes.Aborted},
		{name: "precondition", err: errs.ErrPreconditionFailed, code: codes.FailedPrecondition},
		{name: "dependency", err: errs.ErrDependencyUnavailable, code: codes.Unavailable},
		{name: "unknown", err: errors.New("boom"), code: codes.Internal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			interceptor := UnaryErrorInterceptor(nil)
			info := &grpcruntime.UnaryServerInfo{FullMethod: "/kodex.providers.v1.ProviderHubService/Test"}
			_, err := interceptor(context.Background(), nil, info, func(context.Context, any) (any, error) {
				return nil, tc.err
			})
			if status.Code(err) != tc.code {
				t.Fatalf("status code = %s, want %s", status.Code(err), tc.code)
			}
		})
	}
}

type fakeService struct{}

func (fakeService) GetProviderAccountRuntimeState(context.Context, providerservice.GetProviderAccountRuntimeStateInput) (entity.ProviderAccountRuntimeState, error) {
	return entity.ProviderAccountRuntimeState{
		Base:              entity.Base{ID: uuid.New(), Version: 1},
		ExternalAccountID: uuid.New(),
		ProviderSlug:      enum.ProviderSlugGitHub,
		Status:            enum.ProviderAccountRuntimeStatusActive,
	}, nil
}

func (fakeService) ListProviderAccountRuntimeStates(context.Context, providerservice.ListProviderAccountRuntimeStatesInput) (providerservice.ListProviderAccountRuntimeStatesResult, error) {
	return providerservice.ListProviderAccountRuntimeStatesResult{
		RuntimeStates: []entity.ProviderAccountRuntimeState{{
			Base:              entity.Base{ID: uuid.New(), Version: 1},
			ExternalAccountID: uuid.New(),
			ProviderSlug:      enum.ProviderSlugGitHub,
			Status:            enum.ProviderAccountRuntimeStatusActive,
		}},
		Page: query.PageResult{},
	}, nil
}

func (fakeService) RecordProviderLimitSnapshot(_ context.Context, input providerservice.RecordProviderLimitSnapshotInput) (entity.ProviderLimitSnapshot, error) {
	return entity.ProviderLimitSnapshot{
		ID:                uuid.New(),
		ExternalAccountID: input.ExternalAccountID,
		ProviderSlug:      input.ProviderSlug,
		LimitClass:        input.LimitClass,
		Remaining:         input.Remaining,
		LimitValue:        input.LimitValue,
		ResetAt:           input.ResetAt,
		CapturedAt:        input.CapturedAt,
		Source:            input.Source,
	}, nil
}

func (fakeService) ListProviderLimitSnapshots(context.Context, providerservice.ListProviderLimitSnapshotsInput) (providerservice.ListProviderLimitSnapshotsResult, error) {
	return providerservice.ListProviderLimitSnapshotsResult{Page: query.PageResult{}}, nil
}

func (fakeService) ListProviderOperations(context.Context, providerservice.ListProviderOperationsInput) (providerservice.ListProviderOperationsResult, error) {
	return providerservice.ListProviderOperationsResult{Page: query.PageResult{}}, nil
}
