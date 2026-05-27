package grpc

import (
	"log/slog"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// UnaryErrorInterceptor maps interaction-hub domain errors to transport-safe gRPC statuses.
func UnaryErrorInterceptor(logger *slog.Logger) grpcruntime.UnaryServerInterceptor {
	mapper := interactionDomainErrorMapper()
	return grpcserver.UnaryBoundaryErrorInterceptor(
		"interaction-hub",
		logger,
		mapper,
	)
}

func interactionDomainErrorMapper() grpcserver.DomainErrorMapper {
	rules := []grpcserver.DomainErrorRule{
		{Target: errs.ErrAlreadyExists, Code: codes.AlreadyExists, Message: "interaction already exists"},
		{Target: errs.ErrConflict, Code: codes.FailedPrecondition, Message: "interaction conflict"},
		{Target: errs.ErrInvalidArgument, Code: codes.InvalidArgument, Message: "invalid request"},
		{Target: errs.ErrNotFound, Code: codes.NotFound, Message: "interaction not found"},
		{Target: errs.ErrNotImplemented, Code: codes.Unimplemented, Message: "operation is not implemented in interaction-hub skeleton"},
		{Target: errs.ErrUnavailable, Code: codes.Unavailable, Message: "interaction dependency unavailable"},
	}
	return grpcserver.NewDomainErrorMapper(
		rules...,
	)
}
