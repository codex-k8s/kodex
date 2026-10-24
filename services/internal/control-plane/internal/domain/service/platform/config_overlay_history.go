package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ListConfigOverlayRevisions resolves principal and delegates overlay history filters, totals and
// pagination to the owning repository.
// ListConfigOverlayRevisions разрешает principal и передаёт фильтры, счётчики и пагинацию истории
// overlay repository владельца.
func (service *Service) ListConfigOverlayRevisions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.ConfigOverlayVersion, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	return service.repository.ListConfigOverlayRevisions(ctx, p, filter)
}

// GetConfigOverlayRevision resolves principal and reads an exact agent/revision pair through the
// owner repository without selecting a latest revision locally.
// GetConfigOverlayRevision разрешает principal и читает точную пару агента и ревизии через
// repository владельца без локального выбора последней ревизии.
func (service *Service) GetConfigOverlayRevision(ctx context.Context, p value.Principal, agentRef, revisionRef string) (entity.ConfigOverlayVersion, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ConfigOverlayVersion{}, err
	}
	return service.repository.GetConfigOverlayRevision(ctx, p, agentRef, revisionRef)
}
