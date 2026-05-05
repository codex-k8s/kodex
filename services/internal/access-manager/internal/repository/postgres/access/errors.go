package access

import (
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
)

func wrapError(operation string, err error) error {
	return postgreslib.WrapError(operation, err, postgreslib.ErrorSentinels{
		AlreadyExists:      errs.ErrAlreadyExists,
		Conflict:           errs.ErrConflict,
		InvalidArgument:    errs.ErrInvalidArgument,
		NotFound:           errs.ErrNotFound,
		PreconditionFailed: errs.ErrPreconditionFailed,
	})
}
