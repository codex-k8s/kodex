package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ClaimIntegrationInvocationsWithCapacity keeps the existing Runtime claim operation and original principal.
// Mixed requests require an explicit owner port; unavailable support cannot downgrade to a legacy claim.
// ClaimIntegrationInvocationsWithCapacity сохраняет existing Runtime claim operation и original principal.
// Mixed request требует explicit owner port; unavailable support не переходит в legacy claim.
func (service *Service) ClaimIntegrationInvocationsWithCapacity(ctx context.Context, p value.Principal, instance string, limit int32, capacity value.IntegrationInvocationDeliveryCapacity) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if p.CallerWorkload != "integration-gateway" || p.Permission != "platform.runtime.integrations.claim" || p.ProjectRef != "" {
		return nil, errs.ErrForbidden
	}
	if validatePlatformClaimBatch(instance, limit) != nil || !capacity.Valid(limit) {
		return nil, errs.ErrInvalid
	}
	owner, ok := service.repository.(platformrepo.IntegrationInvocationCapacityRepository)
	if !ok {
		return nil, errs.ErrUnavailable
	}
	return owner.ClaimIntegrationInvocationsWithCapacity(ctx, p, instance, limit, capacity)
}
