package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetRuntimeSecretImpact resolves principal and forwards exact secret revision, search and
// pagination to the owner impact projection without reading secret values.
// GetRuntimeSecretImpact разрешает principal и передаёт точные ревизию секрета, поиск и пагинацию
// проекции влияния владельца без чтения значений секрета.
func (service *Service) GetRuntimeSecretImpact(ctx context.Context, principal value.Principal, ref string, revision int64, search string, page query.Page) (entity.RuntimeSecretImpact, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.RuntimeSecretImpact{}, err
	}
	return service.repository.GetRuntimeSecretImpact(ctx, principal, ref, revision, search, page)
}
