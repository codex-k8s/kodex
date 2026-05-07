package catalog

import (
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
)

var packageHubErrorSentinels = newErrorSentinels()

func wrapError(operation string, err error) error {
	return postgreslib.WrapError(operation, err, packageHubErrorSentinels)
}

func newErrorSentinels() postgreslib.ErrorSentinels {
	sentinels := postgreslib.ErrorSentinels{}
	sentinels.AlreadyExists = errs.ErrAlreadyExists
	sentinels.Conflict = errs.ErrConflict
	sentinels.InvalidArgument = errs.ErrInvalidArgument
	sentinels.NotFound = errs.ErrNotFound
	sentinels.PreconditionFailed = errs.ErrPreconditionFailed
	return sentinels
}
