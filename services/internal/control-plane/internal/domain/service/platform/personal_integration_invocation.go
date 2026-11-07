package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// BeginPersonalIntegrationInvocation resolves the signed worker identity before owner admission.
// The returned descriptor is bound to a committed one-time permit, never to a browser user selector.
// BeginPersonalIntegrationInvocation разрешает подписанную worker identity перед owner admission.
// Descriptor связан с committed одноразовым допуском, не с browser user selector.
func (service *Service) BeginPersonalIntegrationInvocation(ctx context.Context, principal value.Principal, input command.PersonalIntegrationInvocationAdmission) (entity.PersonalIntegrationInvocationAdmissionResult, error) {
	work, ok := service.repository.(port.PersonalIntegrationInvocationWork)
	if !ok {
		return entity.PersonalIntegrationInvocationAdmissionResult{}, errs.ErrUnavailable
	}
	resolved, err := service.principal(ctx, principal)
	if err != nil {
		return entity.PersonalIntegrationInvocationAdmissionResult{}, err
	}
	return work.BeginPersonalIntegrationInvocation(ctx, resolved, input)
}
