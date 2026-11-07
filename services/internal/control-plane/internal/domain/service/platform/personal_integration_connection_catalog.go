package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ListPersonalIntegrationConnections resolves the verified browser principal exactly once before
// reading its visible catalog and own optional bindings. It never creates a binding during GET.
// ListPersonalIntegrationConnections единожды разрешает проверенный browser principal перед чтением
// видимого каталога и его собственных optional bindings. GET никогда не создаёт привязку.
func (service *Service) ListPersonalIntegrationConnections(ctx context.Context, principal value.Principal, filter query.PersonalIntegrationConnectionFilter) ([]entity.PersonalIntegrationConnection, string, error) {
	reader, ok := service.repository.(platformrepo.PersonalIntegrationConnectionCatalog)
	if !ok {
		return nil, "", errs.ErrUnavailable
	}
	resolved, err := service.principal(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	return reader.ListPersonalIntegrationConnections(ctx, resolved, filter)
}
