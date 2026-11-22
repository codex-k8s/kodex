package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ClaimManagedKubernetesInvocations admits one bounded long-operation lane from the exact worker.
// The owner still derives root actor, selected identity and current grant from its stored graph.
// ClaimManagedKubernetesInvocations допускает bounded long-operation lane от exact worker.
// Owner выводит root actor, selected identity и current grant из своего stored graph.
func (service *Service) ClaimManagedKubernetesInvocations(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if p.CallerWorkload != "integration-gateway" || p.Permission != "platform.runtime.kubernetes.claim" || p.ProjectRef != "" {
		return nil, errs.ErrForbidden
	}
	if validatePlatformClaimBatch(instance, limit) != nil || limit > 4 {
		return nil, errs.ErrInvalid
	}
	repository, exists := service.repository.(platformrepo.ManagedKubernetesInvocationRepository)
	if !exists {
		return nil, errs.ErrUnavailable
	}
	return repository.ClaimManagedKubernetesInvocations(ctx, p, instance, limit)
}

// RenewManagedKubernetesInvocation rechecks the original binding without issuing a new attempt.
// A terminated, revoked or expired owner graph cannot be revived by renewal or a caller instance.
// RenewManagedKubernetesInvocation перепроверяет original binding без выдачи новой attempt.
// Terminated, revoked либо expired owner graph не восстанавливается renewal либо caller instance.
func (service *Service) RenewManagedKubernetesInvocation(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding) (entity.ManagedKubernetesInvocationLease, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ManagedKubernetesInvocationLease{}, err
	}
	if p.CallerWorkload != "integration-gateway" || p.Permission != "platform.runtime.kubernetes.renew" || p.ProjectRef != "" {
		return entity.ManagedKubernetesInvocationLease{}, errs.ErrForbidden
	}
	if binding.Validate() != nil || binding.InvocationRef == "" || binding.ConnectionTestRef != "" {
		return entity.ManagedKubernetesInvocationLease{}, errs.ErrInvalid
	}
	repository, exists := service.repository.(platformrepo.ManagedKubernetesInvocationRepository)
	if !exists {
		return entity.ManagedKubernetesInvocationLease{}, errs.ErrUnavailable
	}
	return repository.RenewManagedKubernetesInvocation(ctx, p, binding)
}
