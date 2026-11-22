package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// managedKubernetesInvocationOwner accepts only the exact gateway method and original invocation variant.
// It does not infer originating human authority from transported references or technical caller identity.
// managedKubernetesInvocationOwner принимает лишь exact gateway method и original invocation variant.
// Не выводит originating human authority из transported refs либо technical caller identity.
func (service *Service) managedKubernetesInvocationOwner(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, permission string) (platformrepo.ManagedKubernetesInvocationCompletionRepository, value.Principal, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, p, err
	}
	if p.CallerWorkload != "integration-gateway" || p.Permission != permission || p.ProjectRef != "" {
		return nil, p, errs.ErrForbidden
	}
	if binding.Validate() != nil || binding.InvocationRef == "" || binding.ConnectionTestRef != "" {
		return nil, p, errs.ErrInvalid
	}
	repository, ok := service.repository.(platformrepo.ManagedKubernetesInvocationCompletionRepository)
	if !ok {
		return nil, p, errs.ErrUnavailable
	}
	return repository, p, nil
}

// CompleteManagedKubernetesInvocation delegates exact all-child finalization without caller results.
// NoPlan grammar is not proof: the repository must atomically prove no effects or open Gates.
// CompleteManagedKubernetesInvocation передаёт exact all-child finalization без caller results.
// NoPlan grammar не является proof: repository атомарно проверяет отсутствие effects/open Gates.
func (service *Service) CompleteManagedKubernetesInvocation(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesCompleteInvocation) (entity.ManagedKubernetesInvocationReceipt, error) {
	repository, p, err := service.managedKubernetesInvocationOwner(ctx, p, binding, "platform.runtime.kubernetes.complete")
	if err != nil {
		return entity.ManagedKubernetesInvocationReceipt{}, err
	}
	if !input.ValidPlanPins() {
		return entity.ManagedKubernetesInvocationReceipt{}, errs.ErrInvalid
	}
	return repository.CompleteManagedKubernetesInvocation(ctx, p, binding, input)
}

// GetManagedKubernetesInvocation exposes only original claim metadata for status and lost ACK recovery.
// It cannot broaden an organization locator into cached result delivery or a fresh execution grant.
// GetManagedKubernetesInvocation выдаёт лишь original claim metadata для status/lost ACK recovery.
// Не превращает organization locator в cached result delivery либо fresh execution grant.
func (service *Service) GetManagedKubernetesInvocation(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding) (entity.ManagedKubernetesInvocationReceipt, error) {
	repository, p, err := service.managedKubernetesInvocationOwner(ctx, p, binding, "platform.runtime.kubernetes.read")
	if err != nil {
		return entity.ManagedKubernetesInvocationReceipt{}, err
	}
	return repository.GetManagedKubernetesInvocation(ctx, p, binding)
}

// CheckManagedKubernetesDeliveryContract reports typed mixed delivery without optional target readiness.
// Missing support returns revision zero; infrastructure failure remains an error, never ready.
// CheckManagedKubernetesDeliveryContract сообщает typed mixed delivery без optional target readiness.
// Missing support возвращает revision zero; infrastructure failure остаётся error, не ready.
func (service *Service) CheckManagedKubernetesDeliveryContract(ctx context.Context, p value.Principal) (uint32, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return 0, err
	}
	broker := p.CallerWorkload == "secret-broker" && p.Permission == "platform.runtime.kubernetes.readiness"
	gateway := p.CallerWorkload == "integration-gateway" && p.Permission == "platform.runtime.integrations.delivery.check"
	if p.ProjectRef != "" || !broker && !gateway {
		return 0, errs.ErrForbidden
	}
	repository := service.repository
	delivery, ok := repository.(platformrepo.ManagedIntegrationInvocationDeliveryRepository)
	if !ok {
		return 0, nil
	}
	revision, err := delivery.ManagedIntegrationInvocationDeliveryContract(ctx, p)
	if err != nil {
		return 0, err
	}
	if revision != 1 {
		return 0, nil
	}
	return revision, nil
}

// CancelManagedKubernetesInvocation closes the original owner graph without caller-selected outcome.
// Revocation does not prevent cleanup of the exact old execution, but never authorizes fresh work.
// CancelManagedKubernetesInvocation закрывает original owner graph без caller-selected outcome.
// Revoke не блокирует cleanup exact old execution, но не разрешает fresh work.
func (service *Service) CancelManagedKubernetesInvocation(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding) (entity.ManagedKubernetesInvocationReceipt, error) {
	completion, p, err := service.managedKubernetesInvocationOwner(ctx, p, binding, "platform.runtime.kubernetes.cancel")
	if err != nil {
		return entity.ManagedKubernetesInvocationReceipt{}, err
	}
	owner, ok := completion.(platformrepo.ManagedKubernetesInvocationCancellationRepository)
	if !ok {
		return entity.ManagedKubernetesInvocationReceipt{}, errs.ErrUnavailable
	}
	return owner.CancelManagedKubernetesInvocation(ctx, p, binding)
}
