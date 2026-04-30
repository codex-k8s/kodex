package grpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
