package access

import (
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
)

func wrapError(operation string, err error) error {
	sentinels := postgreslib.CRUDSentinels(errs.ErrAlreadyExists, errs.ErrConflict, errs.ErrInvalidArgument, errs.ErrNotFound, errs.ErrPreconditionFailed)
	return postgreslib.WrapError(operation, err, sentinels)
}
