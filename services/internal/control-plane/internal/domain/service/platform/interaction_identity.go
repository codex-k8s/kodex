package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) ListInteractionIdentities(ctx context.Context, principal value.Principal, connection string, page query.Page) ([]entity.InteractionIdentity, string, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListInteractionIdentities(ctx, principal, connection, page)
}
