package grpc

import (
	"log/slog"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// UnaryErrorInterceptor maps domain errors to the public gRPC error boundary.
func UnaryErrorInterceptor(logger *slog.Logger) grpcruntime.UnaryServerInterceptor {
	return grpcserver.UnaryBoundaryErrorInterceptor("fleet-manager", logger, fleetErrorMapper())
}

func fleetErrorMapper() grpcserver.DomainErrorMapper {
	rules := fleetErrorRules()
	return grpcserver.NewDomainErrorMapper(rules...)
}

func fleetErrorRules() []grpcserver.DomainErrorRule {
	descriptors := []struct {
		target  error
		code    codes.Code
		message string
	}{
		{target: errs.ErrInvalidArgument, code: codes.InvalidArgument, message: "invalid request"},
		{target: errs.ErrForbidden, code: codes.PermissionDenied, message: "permission denied"},
		{target: errs.ErrNotFound, code: codes.NotFound, message: "not found"},
		{target: errs.ErrAlreadyExists, code: codes.AlreadyExists, message: "already exists"},
		{target: errs.ErrConflict, code: codes.Aborted, message: "version conflict"},
		{target: errs.ErrPreconditionFailed, code: codes.FailedPrecondition, message: "precondition failed"},
		{target: errs.ErrDependencyUnavailable, code: codes.Unavailable, message: "dependency unavailable"},
	}
	rules := make([]grpcserver.DomainErrorRule, 0, len(descriptors))
	for _, descriptor := range descriptors {
		rules = append(rules, grpcserver.DomainErrorRule{
			Target:  descriptor.target,
			Code:    descriptor.code,
			Message: descriptor.message,
		})
	}
	return rules
}
