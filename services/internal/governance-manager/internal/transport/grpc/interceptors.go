package grpc

import (
	"log/slog"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// UnaryErrorInterceptor maps governance-manager domain errors to the public gRPC boundary.
func UnaryErrorInterceptor(logger *slog.Logger) grpcruntime.UnaryServerInterceptor {
	return grpcserver.UnaryBoundaryErrorInterceptor("governance-manager", logger, grpcserver.NewDomainErrorMapper(
		grpcserver.DomainErrorRule{Target: errs.ErrAlreadyExists, Code: codes.AlreadyExists, Message: "resource already exists"},
		grpcserver.DomainErrorRule{Target: errs.ErrConflict, Code: codes.Aborted, Message: "concurrent change conflict"},
		grpcserver.DomainErrorRule{Target: errs.ErrInvalidArgument, Code: codes.InvalidArgument, Message: "invalid request"},
		grpcserver.DomainErrorRule{Target: errs.ErrNotFound, Code: codes.NotFound, Message: "resource not found"},
		grpcserver.DomainErrorRule{Target: errs.ErrPreconditionFailed, Code: codes.FailedPrecondition, Message: "precondition failed"},
		grpcserver.DomainErrorRule{Target: errs.ErrDependencyUnavailable, Code: codes.Unavailable, Message: "dependency unavailable"},
		grpcserver.DomainErrorRule{Target: errs.ErrNotImplemented, Code: codes.Unimplemented, Message: "operation remains backlog in governance-manager skeleton"},
	))
}
