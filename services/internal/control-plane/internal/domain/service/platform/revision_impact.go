package platform

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

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
