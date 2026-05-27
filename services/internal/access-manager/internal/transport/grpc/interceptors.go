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
		grpcserver.DomainRule(errs.ErrInvalidArgument, codes.InvalidArgument, "invalid request"),
		grpcserver.DomainRule(errs.ErrUnauthorizedSubject, codes.Unauthenticated, "unauthenticated"),
		grpcserver.DomainRule(errs.ErrForbidden, codes.PermissionDenied, "permission denied"),
		grpcserver.DomainRule(errs.ErrNotFound, codes.NotFound, "not found"),
		grpcserver.DomainRule(errs.ErrAlreadyExists, codes.AlreadyExists, "already exists"),
		grpcserver.DomainRule(errs.ErrConflict, codes.Aborted, "version conflict"),
		grpcserver.DomainRule(errs.ErrOwnerOrgImmutable, codes.FailedPrecondition, "precondition failed"),
		grpcserver.DomainRule(errs.ErrPreconditionFailed, codes.FailedPrecondition, "precondition failed"),
	)
}
