package grpcserver

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DomainErrorRule maps one domain sentinel error to one public gRPC status.
type DomainErrorRule struct {
	Target  error
	Code    codes.Code
	Message string
}

// DomainErrorMapper maps service-local errors to transport-safe gRPC errors.
type DomainErrorMapper func(error) error

var transportErrorRules = []DomainErrorRule{
	{Target: context.Canceled, Code: codes.Canceled, Message: "request canceled"},
	{Target: context.DeadlineExceeded, Code: codes.DeadlineExceeded, Message: "request deadline exceeded"},
}

// NewDomainErrorMapper creates a deterministic mapper for service-local domain sentinels.
func NewDomainErrorMapper(rules ...DomainErrorRule) DomainErrorMapper {
	allRules := make([]DomainErrorRule, 0, len(transportErrorRules)+len(rules))
	allRules = append(allRules, transportErrorRules...)
	allRules = append(allRules, rules...)
	return func(err error) error {
		if err == nil {
			return nil
		}
		if _, ok := status.FromError(err); ok {
			return err
		}
		for _, rule := range allRules {
			if errors.Is(err, rule.Target) {
				return status.Error(rule.Code, rule.Message)
			}
		}
		return status.Error(codes.Internal, "internal error")
	}
}

// UnaryBoundaryErrorInterceptor applies a service mapper and logs only boundary-relevant failures.
func UnaryBoundaryErrorInterceptor(serviceName string, logger *slog.Logger, mapper DomainErrorMapper) UnaryInterceptor {
	logger = defaultLogger(logger)
	if mapper == nil {
		mapper = NewDomainErrorMapper()
	}
	return func(ctx context.Context, req any, info *UnaryServerInfo, handler UnaryHandler) (any, error) {
		response, err := handler(ctx, req)
		if err == nil {
			return response, nil
		}
		mapped := mapper(err)
		logBoundaryStatus(ctx, logger, serviceName, info.FullMethod, status.Code(mapped))
		return nil, mapped
	}
}

func logBoundaryStatus(ctx context.Context, logger *slog.Logger, serviceName string, method string, code codes.Code) {
	attrs := append(LogAttrsFromContext(ctx), logKeyMethod, method, "code", code.String())
	switch code {
	case codes.Internal, codes.DataLoss, codes.Unknown:
		logger.ErrorContext(ctx, serviceName+" grpc request failed", attrs...)
	case codes.DeadlineExceeded, codes.Unavailable, codes.ResourceExhausted:
		logger.WarnContext(ctx, serviceName+" grpc request degraded", attrs...)
	default:
	}
}
