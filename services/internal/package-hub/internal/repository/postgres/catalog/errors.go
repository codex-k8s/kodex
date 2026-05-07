package catalog

import (
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
)

var packageHubErrorSentinels = postgreslib.ErrorSentinels{
	NotFound:           errs.ErrNotFound,
	Conflict:           errs.ErrConflict,
	AlreadyExists:      errs.ErrAlreadyExists,
	InvalidArgument:    errs.ErrInvalidArgument,
	PreconditionFailed: errs.ErrPreconditionFailed,
}

func wrapError(operation string, err error) error {
	return postgreslib.WrapError(operation, err, packageHubErrorSentinels)
}
