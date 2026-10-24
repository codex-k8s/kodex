package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetRuntimeEnvironmentDraft resolves principal and reads the exact environment draft through the
// owner repository without inferring owner scope from its reference.
// GetRuntimeEnvironmentDraft разрешает principal и читает точный черновик окружения через
// repository владельца без вывода области владельца из ссылки.
func (service *Service) GetRuntimeEnvironmentDraft(ctx context.Context, principal value.Principal, ref string) (entity.RuntimeEnvironmentDraft, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.RuntimeEnvironmentDraft{}, err
	}
	return service.repository.GetRuntimeEnvironmentDraft(ctx, principal, ref)
}
