package runtime

import (
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
)

func wrapError(operation string, err error) error {
	sentinels := postgreslib.ErrorSentinels{
		AlreadyExists:      errs.ErrAlreadyExists,
		Conflict:           errs.ErrConflict,
		InvalidArgument:    errs.ErrInvalidArgument,
		NotFound:           errs.ErrNotFound,
		PreconditionFailed: errs.ErrPreconditionFailed,
	}
	return postgreslib.WrapError(operation, err, sentinels)
}
