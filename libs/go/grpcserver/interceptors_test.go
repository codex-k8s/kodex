package grpcserver

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUnexpectedCodesAreClosed(t *testing.T) {
	for _, code := range []codes.Code{
		codes.Internal,
		codes.Unavailable,
		codes.Unknown,
		codes.DataLoss,
	} {
		if !IsUnexpectedCode(code) {
			t.Fatalf("%s must be unexpected", code)
		}
	}
	for _, code := range []codes.Code{
		codes.InvalidArgument,
		codes.Unauthenticated,
		codes.PermissionDenied,
		codes.NotFound,
		codes.AlreadyExists,
		codes.FailedPrecondition,
		codes.Canceled,
	} {
		if IsUnexpectedCode(code) {
			t.Fatalf("%s must be expected", code)
		}
	}
}

func TestErrorBoundaryPreservesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observed := 0
	interceptor := ErrorBoundary(ErrorObserverFunc(func(context.Context, string, codes.Code, error) {
		observed++
	}))

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Read"}, func(context.Context, any) (any, error) {
		return nil, status.Error(codes.Internal, "repository query failed")
	})

	if code := status.Code(err); code != codes.Canceled {
		t.Fatalf("code = %s, want %s", code, codes.Canceled)
	}
	if observed != 0 {
		t.Fatalf("unexpected errors observed = %d, want 0", observed)
	}
}

func TestStreamErrorBoundaryPreservesDeadlineContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-ctx.Done()
	observed := 0
	interceptor := StreamErrorBoundary(ErrorObserverFunc(func(context.Context, string, codes.Code, error) {
		observed++
	}))
	stream := &contextServerStream{ServerStream: nil, ctx: ctx}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "/test.Service/Watch"}, func(any, grpc.ServerStream) error {
		return status.Error(codes.Unavailable, "repository stream failed")
	})

	if code := status.Code(err); code != codes.DeadlineExceeded {
		t.Fatalf("code = %s, want %s", code, codes.DeadlineExceeded)
	}
	if observed != 0 {
		t.Fatalf("unexpected errors observed = %d, want 0", observed)
	}
}

func TestErrorBoundaryObservesLiveInternalError(t *testing.T) {
	observed := 0
	interceptor := ErrorBoundary(ErrorObserverFunc(func(_ context.Context, method string, code codes.Code, _ error) {
		if method != "/test.Service/Read" || code != codes.Internal {
			t.Fatalf("observation = (%q, %s)", method, code)
		}
		observed++
	}))

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Read"}, func(context.Context, any) (any, error) {
		return nil, status.Error(codes.Internal, "repository query failed")
	})

	if code := status.Code(err); code != codes.Internal {
		t.Fatalf("code = %s, want %s", code, codes.Internal)
	}
	if observed != 1 {
		t.Fatalf("unexpected errors observed = %d, want 1", observed)
	}
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *contextServerStream) Context() context.Context {
	return stream.ctx
}
