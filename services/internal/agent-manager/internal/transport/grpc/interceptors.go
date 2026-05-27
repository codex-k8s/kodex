package grpc

import (
	"log/slog"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

const agentManagerServiceName = "agent-manager"

// UnaryErrorInterceptor maps agent-manager domain errors to the public gRPC boundary.
func UnaryErrorInterceptor(logger *slog.Logger) grpcruntime.UnaryServerInterceptor {
	return grpcserver.UnaryBoundaryErrorInterceptor(agentManagerServiceName, logger, agentManagerErrorMapper())
}

func agentManagerErrorMapper() grpcserver.DomainErrorMapper {
	return grpcserver.NewDomainErrorMapper(agentManagerErrorRules()...)
}

func agentManagerErrorRules() []grpcserver.DomainErrorRule {
	rules := make([]grpcserver.DomainErrorRule, 0, 6)
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrInvalidArgument, Code: codes.InvalidArgument, Message: "invalid request"})
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrNotFound, Code: codes.NotFound, Message: "not found"})
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrAlreadyExists, Code: codes.AlreadyExists, Message: "already exists"})
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrConflict, Code: codes.Aborted, Message: "version conflict"})
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrPreconditionFailed, Code: codes.FailedPrecondition, Message: "precondition failed"})
	rules = append(rules, grpcserver.DomainErrorRule{Target: errs.ErrDependencyUnavailable, Code: codes.Unavailable, Message: "dependency unavailable"})
	return rules
}
