package grpc

import (
	"log/slog"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// UnaryErrorInterceptor maps provider-hub domain errors to stable gRPC statuses.
func UnaryErrorInterceptor(logger *slog.Logger) grpcruntime.UnaryServerInterceptor {
	return grpcserver.UnaryBoundaryErrorInterceptor("provider-hub", logger, providerErrorMapper())
}

var providerErrorRules = []grpcserver.DomainErrorRule{
	{Target: errs.ErrInvalidArgument, Code: codes.InvalidArgument, Message: "invalid request"},
	{Target: errs.ErrForbidden, Code: codes.PermissionDenied, Message: "permission denied"},
	{Target: errs.ErrNotFound, Code: codes.NotFound, Message: "not found"},
	{Target: errs.ErrAlreadyExists, Code: codes.AlreadyExists, Message: "already exists"},
	{Target: errs.ErrConflict, Code: codes.Aborted, Message: "version conflict"},
	{Target: errs.ErrPreconditionFailed, Code: codes.FailedPrecondition, Message: "precondition failed"},
	{Target: errs.ErrDependencyUnavailable, Code: codes.Unavailable, Message: "dependency unavailable"},
}

func providerErrorMapper() grpcserver.DomainErrorMapper {
	return grpcserver.NewDomainErrorMapper(providerErrorRules...)
}
