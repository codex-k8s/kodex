package grpc

import (
	"context"
	"errors"
	"log/slog"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const logKeyMethod = "method"

// UnaryErrorInterceptor maps domain errors to the public gRPC error boundary.
func UnaryErrorInterceptor(logger *slog.Logger) grpcruntime.UnaryServerInterceptor {
	logger = defaultLogger(logger)
	return func(ctx context.Context, req any, info *grpcruntime.UnaryServerInfo, handler grpcruntime.UnaryHandler) (any, error) {
		response, err := handler(ctx, req)
		if err == nil {
			return response, nil
		}
		mapped := errorToStatus(err)
		logBoundaryError(ctx, logger, info.FullMethod, mapped)
		return nil, mapped
	}
}

func errorToStatus(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded")
	case errors.Is(err, errs.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, "invalid request")
	case errors.Is(err, errs.ErrUnauthorizedSubject):
		return status.Error(codes.Unauthenticated, "unauthenticated")
	case errors.Is(err, errs.ErrForbidden):
		return status.Error(codes.PermissionDenied, "permission denied")
	case errors.Is(err, errs.ErrNotFound):
		return status.Error(codes.NotFound, "not found")
	case errors.Is(err, errs.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, "already exists")
	case errors.Is(err, errs.ErrConflict):
		return status.Error(codes.Aborted, "version conflict")
	case errors.Is(err, errs.ErrOwnerOrgImmutable), errors.Is(err, errs.ErrPreconditionFailed):
		return status.Error(codes.FailedPrecondition, "precondition failed")
	default:
		return status.Error(codes.Internal, "internal error")
	}
}

func logBoundaryError(ctx context.Context, logger *slog.Logger, method string, err error) {
	code := status.Code(err)
	attrs := append(grpcserver.LogAttrsFromContext(ctx), logKeyMethod, method, "code", code.String())
	switch code {
	case codes.Internal, codes.DataLoss, codes.Unknown:
		logger.ErrorContext(ctx, "access-manager grpc request failed", attrs...)
	case codes.DeadlineExceeded, codes.Unavailable, codes.ResourceExhausted:
		logger.WarnContext(ctx, "access-manager grpc request degraded", attrs...)
	default:
	}
}

func defaultLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return logger
}
