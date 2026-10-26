package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ClaimSSHComposeInvocations validates a single independent worker slot without executing SSH.
// ClaimSSHComposeInvocations проверяет один отдельный worker slot без выполнения SSH.
func (service *Service) ClaimSSHComposeInvocations(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if validatePlatformClaimBatch(instance, limit) != nil || limit != 1 {
		return nil, errs.ErrInvalid
	}
	repository, ok := service.repository.(platformrepo.SSHComposeRepository)
	if !ok {
		return nil, errs.ErrUnavailable
	}
	return repository.ClaimSSHComposeInvocations(ctx, p, instance, limit)
}

// RenewSSHComposeInvocation preserves exact instance/fence/generation and invokes only the SSH owner port.
// RenewSSHComposeInvocation сохраняет exact instance/fence/generation и вызывает только SSH owner port.
func (service *Service) RenewSSHComposeInvocation(ctx context.Context, p value.Principal, binding platformrepo.SSHComposeLeaseBinding) (map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if !validSSHComposeLeaseBinding(binding) || binding.InvocationRef == "" || binding.ConnectionTestRef != "" {
		return nil, errs.ErrInvalid
	}
	repository, ok := service.repository.(platformrepo.SSHComposeRepository)
	if !ok {
		return nil, errs.ErrUnavailable
	}
	return repository.RenewSSHComposeInvocation(ctx, p, binding)
}

// ResolveSSHComposeCredentialProjection resolves a secret descriptor without transporting its value through CP.
// ResolveSSHComposeCredentialProjection разрешает descriptor без передачи secret value через CP.
func (service *Service) ResolveSSHComposeCredentialProjection(ctx context.Context, p value.Principal, binding platformrepo.SSHComposeLeaseBinding) (map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if !validSSHComposeLeaseBinding(binding) {
		return nil, errs.ErrInvalid
	}
	repository, ok := service.repository.(platformrepo.SSHComposeRepository)
	if !ok {
		return nil, errs.ErrUnavailable
	}
	return repository.ResolveSSHComposeCredentialProjection(ctx, p, binding)
}

// validSSHComposeLeaseBinding rejects ambiguous locators and incomplete fenced leases.
// validSSHComposeLeaseBinding отклоняет неоднозначные locators и неполные fenced leases.
func validSSHComposeLeaseBinding(binding platformrepo.SSHComposeLeaseBinding) bool {
	return (binding.InvocationRef == "") != (binding.ConnectionTestRef == "") && binding.LeaseRef != "" && binding.Fence != "" && binding.Generation > 0 &&
		validatePlatformClaimBatch(binding.WorkloadInstance, 1) == nil
}
