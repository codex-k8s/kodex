package grpc

import (
	"context"
	"errors"
	"log/slog"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type statusRule struct {
	match   error
	code    codes.Code
	message string
}

var providerStatusRules = []statusRule{
	{match: errs.ErrInvalidArgument, code: codes.InvalidArgument, message: "invalid request"},
	{match: errs.ErrForbidden, code: codes.PermissionDenied, message: "permission denied"},
	{match: errs.ErrNotFound, code: codes.NotFound, message: "not found"},
	{match: errs.ErrAlreadyExists, code: codes.AlreadyExists, message: "already exists"},
	{match: errs.ErrConflict, code: codes.Aborted, message: "version conflict"},
	{match: errs.ErrPreconditionFailed, code: codes.FailedPrecondition, message: "precondition failed"},
	{match: errs.ErrDependencyUnavailable, code: codes.Unavailable, message: "dependency unavailable"},
}

// UnaryErrorInterceptor maps provider-hub domain errors to stable gRPC statuses.
func UnaryErrorInterceptor(logger *slog.Logger) grpcruntime.UnaryServerInterceptor {
	boundary := errorBoundary{logger: defaultLogger(logger)}
	return boundary.intercept
}

type errorBoundary struct {
	logger *slog.Logger
}

func (b errorBoundary) intercept(ctx context.Context, req any, info *grpcruntime.UnaryServerInfo, handler grpcruntime.UnaryHandler) (any, error) {
	response, err := handler(ctx, req)
	if err == nil {
		return response, nil
	}
	statusErr := errorToStatus(err)
	logBoundaryError(ctx, b.logger, info.FullMethod, statusErr)
	return nil, statusErr
}

func errorToStatus(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "request canceled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	}
	for _, rule := range providerStatusRules {
		if errors.Is(err, rule.match) {
			return status.Error(rule.code, rule.message)
		}
	}
	return status.Error(codes.Internal, "internal error")
}

func logBoundaryError(ctx context.Context, logger *slog.Logger, method string, err error) {
	code := status.Code(err)
	if code == codes.OK {
		return
	}
	attrs := append(grpcserver.LogAttrsFromContext(ctx), "method", method, "code", code.String())
	if code == codes.Internal || code == codes.DataLoss || code == codes.Unknown {
		logger.ErrorContext(ctx, "provider-hub grpc request failed", attrs...)
		return
	}
	if code == codes.DeadlineExceeded || code == codes.Unavailable || code == codes.ResourceExhausted {
		logger.WarnContext(ctx, "provider-hub grpc request degraded", attrs...)
	}
}

func defaultLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
