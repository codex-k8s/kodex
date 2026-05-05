package project

import (
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
)

func wrapError(operation string, err error) error {
	sentinels := postgreslib.ErrorSentinels{}
	sentinels.NotFound = errs.ErrNotFound
	sentinels.Conflict = errs.ErrConflict
	sentinels.AlreadyExists = errs.ErrAlreadyExists
	sentinels.InvalidArgument = errs.ErrInvalidArgument
	sentinels.PreconditionFailed = errs.ErrPreconditionFailed
	return postgreslib.WrapError(operation, err, sentinels)
}
