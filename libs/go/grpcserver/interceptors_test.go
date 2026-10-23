package grpcserver

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestUnexpectedCodesAreClosed fixes the exact observer code set and ensures
// expected client and cancellation errors are not counted as server failures.
// RU: TestUnexpectedCodesAreClosed закрепляет точный набор кодов наблюдения
// и исключает клиентские ошибки и отмену из серверных сбоев.
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

// TestErrorBoundaryPreservesCanceledContext prevents a late Internal error
// from overriding cancellation or producing an unexpected-error event.
// RU: TestErrorBoundaryPreservesCanceledContext не даёт поздней ошибке
// Internal заменить отмену и породить событие неожиданного сбоя.
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

// TestStreamErrorBoundaryPreservesDeadlineContext keeps an expired streaming
// deadline authoritative over a later Unavailable result.
// RU: TestStreamErrorBoundaryPreservesDeadlineContext сохраняет приоритет
// истёкшего срока потока над поздним статусом Unavailable.
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

// TestErrorBoundaryObservesLiveInternalError verifies one observer callback
// with the exact method and Internal code while the request is still active.
// RU: TestErrorBoundaryObservesLiveInternalError проверяет один вызов
// наблюдателя с точным методом и кодом Internal для активного запроса.
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

// contextServerStream supplies a controlled request context to stream
// interceptor tests without a live gRPC transport.
// RU: contextServerStream предоставляет управляемый контекст тестам
// interceptor потока без действующего транспорта gRPC.
type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the test-controlled stream context, including cancellation.
// RU: Context возвращает управляемый тестом контекст потока, включая отмену.
func (stream *contextServerStream) Context() context.Context {
	return stream.ctx
}
