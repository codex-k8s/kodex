package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetProjectPurgeRecovery resolves principal and reads the owner recovery receipt, leaving
// project-presence and recovery visibility rules to the authoritative repository.
// GetProjectPurgeRecovery разрешает principal и читает квитанцию восстановления владельца,
// оставляя правила наличия проекта и видимости восстановления авторитетному repository.
func (service *Service) GetProjectPurgeRecovery(ctx context.Context, p value.Principal, ref string) (entity.ProjectPurgeRecovery, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ProjectPurgeRecovery{}, err
	}
	return service.repository.GetProjectPurgeRecovery(ctx, p, ref)
}
