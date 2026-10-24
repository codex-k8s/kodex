package platform

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetRevisionImpactPlan resolves principal, rejects a blank trimmed plan reference and delegates
// that normalized reference with unchanged search/page input to the owner.
// GetRevisionImpactPlan разрешает principal, отклоняет пустую после обрезки ссылку плана и
// передаёт нормализованную ссылку с неизменёнными поиском и страницей владельцу.
func (s *Service) GetRevisionImpactPlan(ctx context.Context, p value.Principal, ref, search string, page query.Page) (entity.RevisionImpactPage, error) {
	p, err := s.principal(ctx, p)
	if err != nil {
		return entity.RevisionImpactPage{}, err
	}
	if strings.TrimSpace(ref) == "" {
		return entity.RevisionImpactPage{}, errs.ErrInvalid
	}
	return s.repository.GetRevisionImpactPlan(ctx, p, strings.TrimSpace(ref), search, page)
}
