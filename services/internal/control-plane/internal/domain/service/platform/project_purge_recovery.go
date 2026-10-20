package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) GetProjectPurgeRecovery(ctx context.Context, p value.Principal, ref string) (entity.ProjectPurgeRecovery, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ProjectPurgeRecovery{}, err
	}
	return service.repository.GetProjectPurgeRecovery(ctx, p, ref)
}
