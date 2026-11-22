package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesFileOwner validates the real transport principal and purpose before the optional port.
// Absence fails closed; a public token, artifact locator or different worker never substitutes authority.
// kubernetesFileOwner проверяет real transport principal и purpose до optional port.
// Отсутствие закрывается; public token, artifact locator либо иной worker не заменяет authority.
func (service *Service) kubernetesFileOwner(ctx context.Context, p value.Principal, workload, permission string) (platformrepo.KubernetesFileInputRepository, value.Principal, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, p, err
	}
	if p.CallerWorkload != workload || p.Permission != permission {
		return nil, p, errs.ErrForbidden
	}
	owner, ok := service.repository.(platformrepo.KubernetesFileInputRepository)
	if !ok {
		return nil, p, errs.ErrUnavailable
	}
	return owner, p, nil
}

// ListKubernetesFileInputs forwards bounded discovery after transport identity validation.
// The repository must prove current original-root visibility and cannot infer it from the locator.
// ListKubernetesFileInputs передаёт bounded discovery после transport identity validation.
// Repository доказывает current original-root visibility, не выводит её из locator.
func (service *Service) ListKubernetesFileInputs(ctx context.Context, p value.Principal, invocationRef string) ([]entity.KubernetesFileInputView, error) {
	owner, p, err := service.kubernetesFileOwner(ctx, p, "control-api-gateway", "platform.integrations.kubernetes.file.list")
	if err != nil {
		return nil, err
	}
	if !validKubernetesSetupRef(invocationRef, "inv_") {
		return nil, errs.ErrInvalid
	}
	return owner.ListKubernetesFileInputs(ctx, p, invocationRef)
}

// GetKubernetesFileInput reads one safe original source descriptor without preparing or replaying it.
// GetKubernetesFileInput читает safe original source descriptor без подготовки либо replay.
func (service *Service) GetKubernetesFileInput(ctx context.Context, p value.Principal, invocationRef, sourceRef string) (entity.KubernetesFileInputView, error) {
	owner, p, err := service.kubernetesFileOwner(ctx, p, "control-api-gateway", "platform.integrations.kubernetes.file.get")
	if err != nil {
		return entity.KubernetesFileInputView{}, err
	}
	if !validKubernetesSetupRef(invocationRef, "inv_") || !validKubernetesSetupRef(sourceRef, "kfs_") {
		return entity.KubernetesFileInputView{}, errs.ErrInvalid
	}
	return owner.GetKubernetesFileInput(ctx, p, invocationRef, sourceRef)
}

// PrepareManagedKubernetesFileInput accepts only the protected broker's original claim and child.
// Source, actor, recipe and bytes are independently resolved from persisted owner state.
// PrepareManagedKubernetesFileInput допускает только protected broker original claim и child.
// Source, actor, recipe и bytes независимо разрешаются из persisted owner state.
func (service *Service) PrepareManagedKubernetesFileInput(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, childRef string) (entity.ManagedKubernetesFilePreparation, error) {
	owner, p, err := service.kubernetesFileOwner(ctx, p, "secret-broker", "platform.runtime.kubernetes.file.prepare")
	if err != nil {
		return entity.ManagedKubernetesFilePreparation{}, err
	}
	if binding.Validate() != nil || binding.InvocationRef == "" || binding.ConnectionTestRef != "" || !validKubernetesSetupRef(childRef, "kch_") {
		return entity.ManagedKubernetesFilePreparation{}, errs.ErrInvalid
	}
	return owner.PrepareManagedKubernetesFileInput(ctx, p, binding, childRef)
}
