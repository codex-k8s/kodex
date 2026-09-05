package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) GetAgentEffectiveCapabilities(ctx context.Context, p value.Principal, agentRef, workflowRef, stepKey string, filter query.Filter) (entity.AgentEffectiveCapabilities, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.AgentEffectiveCapabilities{}, err
	}
	return service.repository.GetAgentEffectiveCapabilities(ctx, p, agentRef, workflowRef, stepKey, filter)
}
