package grpc

import (
	"log/slog"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// UnaryErrorInterceptor maps domain errors to the public gRPC error boundary.
func UnaryErrorInterceptor(logger *slog.Logger) grpcruntime.UnaryServerInterceptor {
	return grpcserver.UnaryBoundaryErrorInterceptor("project-catalog", logger, projectErrorMapper())
}

func projectErrorMapper() grpcserver.DomainErrorMapper {
	return grpcserver.NewDomainErrorMapper(projectErrorRules()...)
}

func projectErrorRules() []grpcserver.DomainErrorRule {
	return []grpcserver.DomainErrorRule{
		projectRule(errs.ErrInvalidArgument, codes.InvalidArgument, "invalid request"),
		projectRule(errs.ErrForbidden, codes.PermissionDenied, "permission denied"),
		projectRule(errs.ErrNotFound, codes.NotFound, "not found"),
		projectRule(errs.ErrAlreadyExists, codes.AlreadyExists, "already exists"),
		projectRule(errs.ErrConflict, codes.Aborted, "version conflict"),
		projectRule(errs.ErrPreconditionFailed, codes.FailedPrecondition, "precondition failed"),
		projectRule(errs.ErrDependencyUnavailable, codes.Unavailable, "dependency unavailable"),
	}
}

func projectRule(target error, code codes.Code, message string) grpcserver.DomainErrorRule {
	return grpcserver.DomainErrorRule{Target: target, Code: code, Message: message}
}
