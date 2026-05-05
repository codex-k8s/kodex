package grpcserver

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryAuthInterceptorStoresVerifiedCaller(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		MetadataAuthorization, "Bearer shared-token",
		MetadataCallerType, "service",
		MetadataCallerID, "staff-gateway",
	))
	interceptor := UnaryAuthInterceptor(true, NewSharedTokenAuthenticator("shared-token"))
	_, err := interceptor(ctx, nil, unaryInfo(), func(ctx context.Context, _ any) (any, error) {
		identity, ok := CallerIdentityFromContext(ctx)
		if !ok {
			t.Fatal("caller identity is missing")
		}
		if identity.Type != "service" || identity.ID != "staff-gateway" {
			t.Fatalf("caller identity = %+v, want service/staff-gateway", identity)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("UnaryAuthInterceptor(): %v", err)
	}
}

func TestUnaryCorrelationInterceptorStoresSafeMetadata(t *testing.T) {
	t.Parallel()

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		MetadataTraceID, "trace-1",
		MetadataRequestID, "request-1",
		MetadataRequestSource, "staff-gateway",
	))
	_, err := UnaryCorrelationInterceptor()(ctx, nil, unaryInfo(), func(ctx context.Context, _ any) (any, error) {
		correlation, ok := RequestCorrelationFromContext(ctx)
		if !ok {
			t.Fatal("request correlation is missing")
		}
		if correlation.TraceID != "trace-1" || correlation.RequestID != "request-1" || correlation.Source != "staff-gateway" {
			t.Fatalf("request correlation = %+v", correlation)
		}
		if attrs := LogAttrsFromContext(ctx); len(attrs) != 6 {
			t.Fatalf("log attrs len = %d, want 6", len(attrs))
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("UnaryCorrelationInterceptor(): %v", err)
	}
}

func TestUnaryAuthInterceptorRejectsMissingToken(t *testing.T) {
	t.Parallel()

	_, err := UnaryAuthInterceptor(true, NewSharedTokenAuthenticator("shared-token"))(context.Background(), nil, unaryInfo(), func(context.Context, any) (any, error) {
		t.Fatal("handler must not be called")
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %s, want unauthenticated", status.Code(err))
	}
}

func TestUnaryInFlightLimitInterceptorRejectsSaturatedReplica(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	interceptor := UnaryInFlightLimitInterceptor(1, nil)
	go func() {
		_, _ = interceptor(context.Background(), nil, unaryInfo(), func(context.Context, any) (any, error) {
			close(entered)
			<-release
			return nil, nil
		})
		close(done)
	}()
	<-entered
	_, err := interceptor(context.Background(), nil, unaryInfo(), func(context.Context, any) (any, error) {
		t.Fatal("handler must not be called")
		return nil, nil
	})
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("code = %s, want resource exhausted", status.Code(err))
	}
	close(release)
	<-done
}

func TestUnaryDeadlineInterceptorAddsDeadline(t *testing.T) {
	t.Parallel()

	_, err := UnaryDeadlineInterceptor(time.Second)(context.Background(), nil, unaryInfo(), func(ctx context.Context, _ any) (any, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("deadline is missing")
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("UnaryDeadlineInterceptor(): %v", err)
	}
}

func TestUnaryRecoveryInterceptorMapsPanic(t *testing.T) {
	t.Parallel()

	_, err := UnaryRecoveryInterceptor(discardLogger())(context.Background(), nil, unaryInfo(), func(context.Context, any) (any, error) {
		panic("boom")
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %s, want internal", status.Code(err))
	}
}

func unaryInfo() *UnaryServerInfo {
	return &UnaryServerInfo{FullMethod: "/kodex.test.v1.TestService/Test"}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
