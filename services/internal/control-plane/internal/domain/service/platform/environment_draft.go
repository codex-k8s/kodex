package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) GetRuntimeEnvironmentDraft(ctx context.Context, principal value.Principal, ref string) (entity.RuntimeEnvironmentDraft, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.RuntimeEnvironmentDraft{}, err
	}
	return service.repository.GetRuntimeEnvironmentDraft(ctx, principal, ref)
}
