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

// modelDiscoveryDefinitionLimit bounds complete owner-registry reads to 4096 definitions; exhaustion rejects the response instead of returning incomplete route/account evidence.
// modelDiscoveryDefinitionLimit ограничивает полное чтение owner registry 4096 definitions; исчерпание отклоняет ответ вместо выдачи неполных route/account evidence.
const modelDiscoveryDefinitionLimit = 4096

// ListModelDiscovery resolves the transport principal and reuses authoritative organization-view eligibility before searching static metadata; no model provider is contacted.
// ListModelDiscovery разрешает transport principal и повторно использует авторитетный organization-view допуск до поиска статических metadata; обращения к model provider нет.
func (service *Service) ListModelDiscovery(ctx context.Context, principal value.Principal, filter query.Filter, selectors query.ModelDiscoveryFilter) (entity.ModelDiscoveryPage, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.ModelDiscoveryPage{}, err
	}
	definitions := []entity.ProviderDefinition{}
	cursor := ""
	seen := map[string]bool{}
	for {
		remaining := modelDiscoveryDefinitionLimit - len(definitions)
		pageSize := min(100, remaining)
		page, next, readErr := service.repository.ListProviderDefinitions(ctx, principal, query.Filter{Page: query.Page{Size: int32(pageSize), Token: cursor}})
		if readErr != nil {
			return entity.ModelDiscoveryPage{}, readErr
		}
		if len(page) > pageSize || next != "" && (next == cursor || len(page) == 0) {
			return entity.ModelDiscoveryPage{}, errs.ErrUnavailable
		}
		for _, definition := range page {
			if definition.Key == "" || seen[definition.Key] {
				return entity.ModelDiscoveryPage{}, errs.ErrUnavailable
			}
			seen[definition.Key] = true
		}
		definitions = append(definitions, page...)
		if next == "" {
			break
		}
		if len(definitions) == modelDiscoveryDefinitionLimit {
			return entity.ModelDiscoveryPage{}, errs.ErrUnavailable
		}
		cursor = next
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
