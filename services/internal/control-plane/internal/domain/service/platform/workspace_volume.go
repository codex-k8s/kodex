package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// RenewWorkspaceVolumeTask requires the exact private worker method before principal resolution; payload references never become actor authority.
// RenewWorkspaceVolumeTask требует exact private worker method до principal resolution; payload references не становятся actor authority.
func (service *Service) RenewWorkspaceVolumeTask(ctx context.Context, principal value.Principal, input command.WorkspaceVolumeTaskInput) (map[string]any, error) {
	if principal.CallerWorkload != "session-archive" || principal.Permission != command.WorkspaceVolumeRenewPermission {
		return nil, errs.ErrForbidden
	}
	resolved, err := service.principal(ctx, principal)
	if err != nil {
		return nil, err
	}
	return service.repository.RenewWorkspaceVolumeTask(ctx, resolved, input)
}
