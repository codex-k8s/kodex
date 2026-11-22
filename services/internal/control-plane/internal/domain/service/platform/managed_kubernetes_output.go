package platform

import (
	"bytes"
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// managedKubernetesOutputOwner requires the fixed gateway method and invocation-only binding.
// Missing owner support fails closed and never falls back to generic artifact upload authority.
// managedKubernetesOutputOwner требует fixed gateway method и invocation-only binding.
// Missing owner support закрыт, без перехода к generic artifact upload authority.
func (service *Service) managedKubernetesOutputOwner(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, permission string) (platformrepo.ManagedKubernetesOutputRepository, value.Principal, error) {
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
	owner, ok := service.repository.(platformrepo.ManagedKubernetesOutputRepository)
	if !ok {
		return nil, p, errs.ErrUnavailable
	}
	return owner, p, nil
}

// AcceptManagedKubernetesOutputFrame snapshots and clears a bounded binary chunk without publishing it.
// The repository must still prove current frame tuple, ordinal, native deadline and durable one-write receipt.
// AcceptManagedKubernetesOutputFrame копирует и очищает bounded binary chunk без его публикации.
// Repository проверяет current frame tuple/ordinal/native deadline и durable one-write receipt.
func (service *Service) AcceptManagedKubernetesOutputFrame(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesAcceptOutputFrame) (entity.ManagedKubernetesOutputFrameReceipt, error) {
	if len(input.Payload) > int(managedkubernetes.MaximumFrameBytes) {
		return entity.ManagedKubernetesOutputFrameReceipt{}, errs.ErrInvalid
	}
	input.Payload = bytes.Clone(input.Payload)
	defer clear(input.Payload)
	owner, p, err := service.managedKubernetesOutputOwner(ctx, p, binding, "platform.runtime.kubernetes.output.accept")
	if err != nil {
		return entity.ManagedKubernetesOutputFrameReceipt{}, err
	}
	if !input.Valid() {
		return entity.ManagedKubernetesOutputFrameReceipt{}, errs.ErrInvalid
	}
	return owner.AcceptManagedKubernetesOutputFrame(ctx, p, binding, input)
}

// GetManagedKubernetesOutputFrame reads only an original stage receipt, including after execution closure.
// It neither retries a PUT nor updates credit, discloses bytes or publishes an artifact.
// GetManagedKubernetesOutputFrame читает лишь original stage receipt, включая closed execution.
// Не повторяет PUT, не продлевает credit, не выдаёт bytes и не публикует artifact.
func (service *Service) GetManagedKubernetesOutputFrame(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesOutputFrameLookup) (entity.ManagedKubernetesOutputFrameReceipt, error) {
	owner, p, err := service.managedKubernetesOutputOwner(ctx, p, binding, "platform.runtime.kubernetes.output.read")
	if err != nil {
		return entity.ManagedKubernetesOutputFrameReceipt{}, err
	}
	if !input.Valid() {
		return entity.ManagedKubernetesOutputFrameReceipt{}, errs.ErrInvalid
	}
	return owner.GetManagedKubernetesOutputFrame(ctx, p, binding, input)
}
