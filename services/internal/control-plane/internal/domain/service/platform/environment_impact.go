package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetRuntimeEnvironmentImpact resolves principal and delegates exact environment/version impact
// search and pagination to owner state.
// GetRuntimeEnvironmentImpact разрешает principal и передаёт точные окружение и версию, поиск
// влияния и пагинацию состоянию владельца.
func (service *Service) GetRuntimeEnvironmentImpact(ctx context.Context, principal value.Principal, ref, version, search string, page query.Page) (entity.RuntimeEnvironmentImpact, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.RuntimeEnvironmentImpact{}, err
	}
	return service.repository.GetRuntimeEnvironmentImpact(ctx, principal, ref, version, search, page)
}
