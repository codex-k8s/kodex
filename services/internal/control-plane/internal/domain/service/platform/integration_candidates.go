package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ListIntegrationGrantConnectionCandidates resolves the principal before asking the owner repository for connection candidates; listing does not grant access to a returned connection.
// ListIntegrationGrantConnectionCandidates разрешает субъекта перед запросом кандидатов соединений у репозитория владельца; чтение списка не даёт доступа к возвращённому соединению.
func (service *Service) ListIntegrationGrantConnectionCandidates(ctx context.Context, p value.Principal, input query.IntegrationCandidates) (entity.IntegrationConnectionCandidates, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.IntegrationConnectionCandidates{}, err
	}
	return service.repository.ListIntegrationGrantConnectionCandidates(ctx, p, input)
}

// ListIntegrationGrantProjectCandidates resolves the principal and delegates the typed project-candidate query; supplied project references are not accepted as authority by this wrapper.
// ListIntegrationGrantProjectCandidates разрешает субъекта и передаёт типизированный запрос проектов-кандидатов; переданные ссылки проектов не принимаются обёрткой за полномочия.
func (service *Service) ListIntegrationGrantProjectCandidates(ctx context.Context, p value.Principal, input query.IntegrationCandidates) (entity.IntegrationProjectCandidates, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.IntegrationProjectCandidates{}, err
	}
	return service.repository.ListIntegrationGrantProjectCandidates(ctx, p, input)
}

// ListIntegrationGrantRecipientCandidates resolves the principal and reads eligible recipient candidates through the repository, without creating a recipient grant.
// ListIntegrationGrantRecipientCandidates разрешает субъекта и читает допустимых получателей-кандидатов через репозиторий без создания полномочия получателя.
func (service *Service) ListIntegrationGrantRecipientCandidates(ctx context.Context, p value.Principal, input query.IntegrationCandidates) (entity.IntegrationRecipientCandidates, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.IntegrationRecipientCandidates{}, err
	}
	return service.repository.ListIntegrationGrantRecipientCandidates(ctx, p, input)
}

// ListIntegrationGrantCapabilityCandidates resolves the principal and reads capability candidates with the exact typed input; candidate visibility does not enable a capability.
// ListIntegrationGrantCapabilityCandidates разрешает субъекта и читает кандидатов возможностей по точному типизированному входу; видимость кандидата не включает возможность.
func (service *Service) ListIntegrationGrantCapabilityCandidates(ctx context.Context, p value.Principal, input query.IntegrationCandidates) (entity.IntegrationCapabilityCandidates, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.IntegrationCapabilityCandidates{}, err
	}
	return service.repository.ListIntegrationGrantCapabilityCandidates(ctx, p, input)
}

// GetSystemAssistantIntegrationGrantCandidates resolves the principal and forwards connection, search and page to the system-assistant candidate read, separately from project-assistant scope.
// GetSystemAssistantIntegrationGrantCandidates разрешает субъекта и передаёт соединение, поиск и страницу чтению кандидатов системного помощника отдельно от области проектного помощника.
func (service *Service) GetSystemAssistantIntegrationGrantCandidates(ctx context.Context, p value.Principal, connectionRef, search string, page query.Page) (entity.SystemAssistantIntegrationGrantCandidates, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.SystemAssistantIntegrationGrantCandidates{}, err
	}
	return service.repository.GetSystemAssistantIntegrationGrantCandidates(ctx, p, connectionRef, search, page)
}

// GetProjectAssistantIntegrationGrantCandidates resolves the principal and retains both project and connection selectors for the repository's project-assistant candidate boundary; it does not reuse system-assistant authority.
// GetProjectAssistantIntegrationGrantCandidates разрешает субъекта и сохраняет селекторы проекта и соединения для границы кандидатов проектного помощника в репозитории; полномочия системного помощника не переиспользуются.
func (service *Service) GetProjectAssistantIntegrationGrantCandidates(ctx context.Context, p value.Principal, projectRef, connectionRef, search string, page query.Page) (entity.ProjectAssistantIntegrationGrantCandidates, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ProjectAssistantIntegrationGrantCandidates{}, err
	}
	return service.repository.GetProjectAssistantIntegrationGrantCandidates(ctx, p, projectRef, connectionRef, search, page)
}
