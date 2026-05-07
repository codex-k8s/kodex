package grpc

import (
	"log/slog"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// UnaryErrorInterceptor maps domain errors to the public gRPC error boundary.
func UnaryErrorInterceptor(logger *slog.Logger) grpcruntime.UnaryServerInterceptor {
	return grpcserver.UnaryBoundaryErrorInterceptor("access-manager", logger, accessErrorMapper())
}

func accessErrorMapper() grpcserver.DomainErrorMapper {
	return grpcserver.NewDomainErrorMapper(
		grpcserver.DomainErrorRule{Target: errs.ErrInvalidArgument, Code: codes.InvalidArgument, Message: "invalid request"},
		grpcserver.DomainErrorRule{Target: errs.ErrUnauthorizedSubject, Code: codes.Unauthenticated, Message: "unauthenticated"},
		grpcserver.DomainErrorRule{Target: errs.ErrForbidden, Code: codes.PermissionDenied, Message: "permission denied"},
		grpcserver.DomainErrorRule{Target: errs.ErrNotFound, Code: codes.NotFound, Message: "not found"},
		grpcserver.DomainErrorRule{Target: errs.ErrAlreadyExists, Code: codes.AlreadyExists, Message: "already exists"},
		grpcserver.DomainErrorRule{Target: errs.ErrConflict, Code: codes.Aborted, Message: "version conflict"},
		grpcserver.DomainErrorRule{Target: errs.ErrOwnerOrgImmutable, Code: codes.FailedPrecondition, Message: "precondition failed"},
		grpcserver.DomainErrorRule{Target: errs.ErrPreconditionFailed, Code: codes.FailedPrecondition, Message: "precondition failed"},
	)
}
