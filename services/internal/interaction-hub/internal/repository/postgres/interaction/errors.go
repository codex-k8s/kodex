package interaction

import (
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
)

var interactionHubErrorSentinels = postgreslib.ErrorSentinels{
	AlreadyExists:   errs.ErrAlreadyExists,
	Conflict:        errs.ErrConflict,
	InvalidArgument: errs.ErrInvalidArgument,
	NotFound:        errs.ErrNotFound,
}

func wrapError(operation string, err error) error {
	return postgreslib.WrapError(operation, err, interactionHubErrorSentinels)
}
