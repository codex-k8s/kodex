package team

import (
	"errors"

	domainerrs "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/errs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func invalidRequest() error {
	return status.Error(codes.InvalidArgument, "Mattermost team request is invalid")
}

func transportError(err error) error {
	switch {
	case errors.Is(err, domainerrs.ErrUnauthorized):
		return status.Error(codes.PermissionDenied, "Mattermost team operation is not allowed")
	case errors.Is(err, domainerrs.ErrNotFound):
		return status.Error(codes.NotFound, "Mattermost team resource is not found")
	case errors.Is(err, domainerrs.ErrConflict):
		return status.Error(codes.FailedPrecondition, "Mattermost team operation conflicts with current state")
	case errors.Is(err, domainerrs.ErrBusy):
		return status.Error(codes.Aborted, "Mattermost team operation is already processing")
	default:
		return status.Error(codes.Unavailable, "Mattermost team dependency is unavailable")
	}
}
