package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetExecutionIntegrationOutcomes requires the exact runtime-controller operation
// and bounded page pins; the repository independently resolves live graph authority.
// GetExecutionIntegrationOutcomes требует точную операцию runtime-controller и
// ограниченные pins страницы; repository независимо разрешает live graph authority.
func (service *Service) GetExecutionIntegrationOutcomes(ctx context.Context, p value.Principal, input query.ExecutionIntegrationOutcomes) (entity.ExecutionIntegrationOutcomes, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ExecutionIntegrationOutcomes{}, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.execution.integration.outcomes.read" {
		return entity.ExecutionIntegrationOutcomes{}, errs.ErrForbidden
	}
	if input.Generation < 1 || len(input.Fence) < 8 || len(input.Fence) > 256 || !workflowCatalogRefPattern.MatchString(input.LeaseRef) || !workflowCatalogRefPattern.MatchString(input.SourceInvocationRef) || !workflowCatalogDigestPattern.MatchString(input.SourceSHA256) || input.OffsetBytes < 0 || input.OffsetBytes > 64<<20 || input.MaximumBytes < 4 || input.MaximumBytes > 4096 {
		return entity.ExecutionIntegrationOutcomes{}, errs.ErrInvalid
	}
	return service.repository.GetExecutionIntegrationOutcomes(ctx, p, input)
}
