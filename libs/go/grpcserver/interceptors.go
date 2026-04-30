package grpcserver

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const logKeyMethod = "method"

// UnaryRecoveryInterceptor converts panics into safe gRPC internal errors.
func UnaryRecoveryInterceptor(logger *slog.Logger) UnaryInterceptor {
	logger = defaultLogger(logger)
	return func(ctx context.Context, req any, info *grpcruntime.UnaryServerInfo, handler grpcruntime.UnaryHandler) (response any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(ctx, "grpc panic recovered", logKeyMethod, info.FullMethod, "panic", recovered, "stack", string(debug.Stack()))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// UnaryMetricsInterceptor records active RPCs, terminal codes and durations.
func UnaryMetricsInterceptor(metrics *Metrics) UnaryInterceptor {
	return func(ctx context.Context, req any, info *grpcruntime.UnaryServerInfo, handler grpcruntime.UnaryHandler) (any, error) {
		if metrics == nil {
			return handler(ctx, req)
		}
		startedAt := time.Now()
		metrics.activeRPCs.WithLabelValues(info.FullMethod).Inc()
		defer metrics.activeRPCs.WithLabelValues(info.FullMethod).Dec()
		response, err := handler(ctx, req)
		code := status.Code(err).String()
		metrics.requestsTotal.WithLabelValues(info.FullMethod, code).Inc()
		metrics.requestDuration.WithLabelValues(info.FullMethod, code).Observe(time.Since(startedAt).Seconds())
		return response, err
	}
}

// UnaryInFlightLimitInterceptor rejects requests when the replica is already saturated.
func UnaryInFlightLimitInterceptor(limit int, metrics *Metrics) UnaryInterceptor {
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
			return nil, status.Error(codes.ResourceExhausted, "grpc in-flight limit exceeded")
		}
	}
}

// UnaryDeadlineInterceptor applies a configured deadline when the caller did not provide one.
func UnaryDeadlineInterceptor(timeout time.Duration) UnaryInterceptor {
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

func defaultLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
