package provider

import (
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
)

var errorSentinels = postgreslib.ErrorSentinels{
	AlreadyExists:      errs.ErrAlreadyExists,
	Conflict:           errs.ErrConflict,
	InvalidArgument:    errs.ErrInvalidArgument,
	NotFound:           errs.ErrNotFound,
	PreconditionFailed: errs.ErrPreconditionFailed,
}

func wrapError(operation string, err error) error {
	return postgreslib.WrapError(operation, err, errorSentinels)
}
