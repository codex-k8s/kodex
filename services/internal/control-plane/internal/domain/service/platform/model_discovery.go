package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/modeldiscovery"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ListModelDiscovery resolves the transport principal and reuses authoritative organization-view eligibility before searching static metadata; no model provider is contacted.
// ListModelDiscovery разрешает transport principal и повторно использует авторитетный organization-view допуск до поиска статических metadata; обращения к model provider нет.
func (service *Service) ListModelDiscovery(ctx context.Context, principal value.Principal, filter query.Filter, selectors query.ModelDiscoveryFilter) (entity.ModelDiscoveryPage, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.ModelDiscoveryPage{}, err
	}
	definitions, next, err := service.repository.ListProviderDefinitions(ctx, principal, query.Filter{Page: query.Page{Size: 100}})
	if err != nil {
		return entity.ModelDiscoveryPage{}, err
	}
	if next != "" {
		return entity.ModelDiscoveryPage{}, errs.ErrUnavailable
	}
	size := int(filter.Page.Size)
	if size == 0 {
		size = 20
	}
	result, err := modeldiscovery.SearchFiltered(filter.Query, filter.Page.Token, size, selectors, definitions)
	if errors.Is(err, modeldiscovery.ErrQuery) {
		return entity.ModelDiscoveryPage{}, errs.ErrInvalid
	}
	if errors.Is(err, modeldiscovery.ErrCatalog) {
		return entity.ModelDiscoveryPage{}, errs.ErrUnavailable
	}
	return result, err
}
