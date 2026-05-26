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
	return grpcserver.UnaryBoundaryErrorInterceptor(
		"interaction-hub",
		logger,
		grpcserver.NewDomainErrorMapper(
			grpcserver.DomainErrorRule{Target: errs.ErrInvalidArgument, Code: codes.InvalidArgument, Message: "invalid request"},
			grpcserver.DomainErrorRule{Target: errs.ErrNotImplemented, Code: codes.Unimplemented, Message: "operation is not implemented in interaction-hub skeleton"},
			grpcserver.DomainErrorRule{Target: errs.ErrUnavailable, Code: codes.Unavailable, Message: "interaction dependency unavailable"},
		),
	)
}
