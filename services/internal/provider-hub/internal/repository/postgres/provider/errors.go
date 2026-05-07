package provider

import (
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
)

func wrapError(operation string, err error) error {
	return postgreslib.WrapError(operation, err, providerErrorSentinels())
}

func providerErrorSentinels() postgreslib.ErrorSentinels {
	return postgreslib.ErrorSentinels{
		AlreadyExists:      errs.ErrAlreadyExists,
		Conflict:           errs.ErrConflict,
		InvalidArgument:    errs.ErrInvalidArgument,
		NotFound:           errs.ErrNotFound,
		PreconditionFailed: errs.ErrPreconditionFailed,
	}
}
