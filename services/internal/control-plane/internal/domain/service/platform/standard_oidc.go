package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

// ResolveStandardOIDCIdentity delegates verified standard evidence to the configured durable owner port, without accepting caller-selected authority.
// ResolveStandardOIDCIdentity передаёт verified standard evidence настроенному durable owner port без caller-selected authority.
func (service *Service) ResolveStandardOIDCIdentity(ctx context.Context, input platformrepo.StandardOIDCIdentity) (platformrepo.StandardOIDCBinding, error) {
	owner, ok := service.repository.(platformrepo.StandardOIDCIdentityOwner)
	if !ok {
		return platformrepo.StandardOIDCBinding{}, errs.ErrUnavailable
	}
	return owner.ResolveStandardOIDCIdentity(ctx, input)
}

// RevokeStandardOIDCSession delegates an already owner-resolved self-session tuple; this wrapper does not derive it from a business payload.
// RevokeStandardOIDCSession передаёт уже owner-resolved self-session tuple; wrapper не выводит его из business payload.
func (service *Service) RevokeStandardOIDCSession(ctx context.Context, organizationID, actorID string, revision uint64) error {
	owner, ok := service.repository.(platformrepo.StandardOIDCIdentityOwner)
	if !ok {
		return errs.ErrUnavailable
	}
	return owner.RevokeStandardOIDCSession(ctx, organizationID, actorID, revision)
}
