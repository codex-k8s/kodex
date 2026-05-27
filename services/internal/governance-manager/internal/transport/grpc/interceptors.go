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
	return grpcserver.UnaryBoundaryErrorInterceptor("governance-manager", logger, governanceErrorMapper())
}

func governanceErrorMapper() grpcserver.DomainErrorMapper {
	rules := make([]grpcserver.DomainErrorRule, 0, 8)
	rules = append(rules, grpcserver.DomainRule(errs.ErrAlreadyExists, codes.AlreadyExists, "resource already exists"))
	rules = append(rules, grpcserver.DomainRule(errs.ErrConflict, codes.Aborted, "concurrent change conflict"))
	rules = append(rules, grpcserver.DomainRule(errs.ErrForbidden, codes.PermissionDenied, "permission denied"))
	rules = append(rules, grpcserver.DomainRule(errs.ErrInvalidArgument, codes.InvalidArgument, "invalid request"))
	rules = append(rules, grpcserver.DomainRule(errs.ErrNotFound, codes.NotFound, "resource not found"))
	rules = append(rules, grpcserver.DomainRule(errs.ErrPreconditionFailed, codes.FailedPrecondition, "precondition failed"))
	rules = append(rules, grpcserver.DomainRule(errs.ErrDependencyUnavailable, codes.Unavailable, "dependency unavailable"))
	rules = append(rules, grpcserver.DomainRule(errs.ErrNotImplemented, codes.Unimplemented, "operation remains backlog in governance-manager skeleton"))
	return grpcserver.NewDomainErrorMapper(rules...)
}
