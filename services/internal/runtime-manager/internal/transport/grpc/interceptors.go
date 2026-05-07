package grpc

import (
	"log/slog"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// UnaryErrorInterceptor maps domain errors to the public gRPC error boundary.
func UnaryErrorInterceptor(logger *slog.Logger) grpcruntime.UnaryServerInterceptor {
	return grpcserver.UnaryBoundaryErrorInterceptor("runtime-manager", logger, runtimeErrorMapper())
}

func runtimeErrorMapper() grpcserver.DomainErrorMapper {
	rules := make([]grpcserver.DomainErrorRule, 0, 7)
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrInvalidArgument, Code: codes.InvalidArgument, Message: "invalid request"})
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrForbidden, Code: codes.PermissionDenied, Message: "permission denied"})
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrNotFound, Code: codes.NotFound, Message: "not found"})
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrAlreadyExists, Code: codes.AlreadyExists, Message: "already exists"})
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrConflict, Code: codes.Aborted, Message: "version conflict"})
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrPreconditionFailed, Code: codes.FailedPrecondition, Message: "precondition failed"})
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrDependencyUnavailable, Code: codes.Unavailable, Message: "dependency unavailable"})
	return grpcserver.NewDomainErrorMapper(rules...)
}
