package agent

import (
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
)

func wrapError(operation string, err error) error {
	return postgreslib.WrapError(operation, err, agentSentinels())
}

func agentSentinels() postgreslib.ErrorSentinels {
	return postgreslib.ErrorSentinels{
		PreconditionFailed: errs.ErrPreconditionFailed,
		InvalidArgument:    errs.ErrInvalidArgument,
		AlreadyExists:      errs.ErrAlreadyExists,
		Conflict:           errs.ErrConflict,
		NotFound:           errs.ErrNotFound,
	}
}
