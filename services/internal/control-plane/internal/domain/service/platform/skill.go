package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

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

func (service *Service) ListSkillBundleRevisions(ctx context.Context, principal value.Principal, ref string, page query.Page) ([]entity.SkillBundleRevision, int64, string, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	return service.repository.ListSkillBundleRevisions(ctx, principal, ref, page)
}

func (service *Service) GetSkillBundle(ctx context.Context, principal value.Principal, ref string) (entity.SkillBundle, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.SkillBundle{}, err
	}
	return service.repository.GetSkillBundle(ctx, principal, ref)
}
