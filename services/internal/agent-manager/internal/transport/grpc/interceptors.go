package grpc

import (
	"log/slog"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// UnaryErrorInterceptor maps agent-manager domain errors to the public gRPC boundary.
func UnaryErrorInterceptor(logger *slog.Logger) grpcruntime.UnaryServerInterceptor {
	return grpcserver.UnaryBoundaryErrorInterceptor("agent-manager", logger, grpcserver.NewDomainErrorMapper(
		grpcserver.DomainErrorRule{Target: errs.ErrInvalidArgument, Code: codes.InvalidArgument, Message: "invalid request"},
		grpcserver.DomainErrorRule{Target: errs.ErrNotFound, Code: codes.NotFound, Message: "not found"},
		grpcserver.DomainErrorRule{Target: errs.ErrAlreadyExists, Code: codes.AlreadyExists, Message: "already exists"},
		grpcserver.DomainErrorRule{Target: errs.ErrConflict, Code: codes.Aborted, Message: "version conflict"},
		grpcserver.DomainErrorRule{Target: errs.ErrPreconditionFailed, Code: codes.FailedPrecondition, Message: "precondition failed"},
	))
}
