package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ListSkillBundles resolves the principal, defaults an empty state to ACTIVE and permits only ACTIVE/ARCHIVED/PURGED with a query no longer than two hundred fifty-six bytes. Repository paging returns items, total and cursor without publishing or installing a bundle.
// ListSkillBundles разрешает субъекта, заменяет пустое состояние на ACTIVE и допускает только ACTIVE/ARCHIVED/PURGED с запросом не длиннее двухсот пятидесяти шести байт. Чтение страниц репозитория возвращает элементы, общее число и курсор без публикации или установки пакета.
func (service *Service) ListSkillBundles(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.SkillBundle, int64, string, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	if filter.State == "" {
		filter.State = "ACTIVE"
	}
	if (filter.State != "ACTIVE" && filter.State != "ARCHIVED" && filter.State != "PURGED") || len(filter.Query) > 256 {
		return nil, 0, "", errs.ErrInvalid
	}
	return service.repository.ListSkillBundles(ctx, principal, filter)
}

// ListSkillBundleRevisions resolves the principal and delegates exact bundle reference and page to repository revision lookup, leaving eligibility checks with the owner.
// ListSkillBundleRevisions разрешает субъекта и передаёт точную ссылку пакета и страницу чтению ревизий в репозитории, оставляя проверки допустимости владельцу.
func (service *Service) ListSkillBundleRevisions(ctx context.Context, principal value.Principal, ref string, page query.Page) ([]entity.SkillBundleRevision, int64, string, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	return service.repository.ListSkillBundleRevisions(ctx, principal, ref, page)
}

// GetSkillBundle resolves the principal and reads one bundle through the repository boundary without executing its instructions or granting its tools.
// GetSkillBundle разрешает субъекта и читает пакет через границу репозитория без исполнения его инструкций или выдачи его инструментов.
func (service *Service) GetSkillBundle(ctx context.Context, principal value.Principal, ref string) (entity.SkillBundle, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.SkillBundle{}, err
	}
	return service.repository.GetSkillBundle(ctx, principal, ref)
}
