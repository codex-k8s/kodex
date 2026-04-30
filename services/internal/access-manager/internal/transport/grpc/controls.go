package grpc

import (
	"context"
	"time"

	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryInFlightLimitInterceptor rejects requests when the replica is already saturated.
func UnaryInFlightLimitInterceptor(limit int, metrics *ServerMetrics) grpcruntime.UnaryServerInterceptor {
	if limit < 1 {
		limit = 1
	}
	slots := make(chan struct{}, limit)
	return func(ctx context.Context, req any, info *grpcruntime.UnaryServerInfo, handler grpcruntime.UnaryHandler) (any, error) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			return handler(ctx, req)
		default:
			metrics.recordInFlightRejection(info.FullMethod)
			return nil, status.Error(codes.ResourceExhausted, "gRPC in-flight limit exceeded")
		}
	}
}

// UnaryDeadlineInterceptor applies a configured deadline when the caller did not provide one.
func UnaryDeadlineInterceptor(timeout time.Duration) grpcruntime.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpcruntime.UnaryServerInfo, handler grpcruntime.UnaryHandler) (any, error) {
		if timeout <= 0 {
			return handler(ctx, req)
		}
		if _, ok := ctx.Deadline(); ok {
			return handler(ctx, req)
		}
		deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return handler(deadlineCtx, req)
	}
}
