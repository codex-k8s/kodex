package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) ListConfigOverlayRevisions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.ConfigOverlayVersion, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	return service.repository.ListConfigOverlayRevisions(ctx, p, filter)
}
func (service *Service) GetConfigOverlayRevision(ctx context.Context, p value.Principal, agentRef, revisionRef string) (entity.ConfigOverlayVersion, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ConfigOverlayVersion{}, err
	}
	return service.repository.GetConfigOverlayRevision(ctx, p, agentRef, revisionRef)
}
