package fleet

import (
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
)

var postgresErrors = postgreslib.ErrorSentinels{
	AlreadyExists:      errs.ErrAlreadyExists,
	Conflict:           errs.ErrConflict,
	InvalidArgument:    errs.ErrInvalidArgument,
	NotFound:           errs.ErrNotFound,
	PreconditionFailed: errs.ErrPreconditionFailed,
}

func wrapError(operation string, err error) error {
	return postgreslib.WrapError(operation, err, postgresErrors)
}
