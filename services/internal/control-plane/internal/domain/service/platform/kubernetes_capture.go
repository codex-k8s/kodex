package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// managedKubernetesCaptureOwner admits only the fixed artifact-retention peer and exact method.
// The independently fenced task is resolved by the repository; no user actor is reconstructed.
// managedKubernetesCaptureOwner допускает только fixed artifact-retention peer и exact method.
// Independently fenced task разрешает repository; user actor не реконструируется.
func (service *Service) managedKubernetesCaptureOwner(ctx context.Context, p value.Principal, permission string) (platformrepo.ManagedKubernetesCaptureRepository, value.Principal, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, p, err
	}
	if p.CallerWorkload != "artifact-retention" || p.Permission != permission || p.ProjectRef != "" {
		return nil, p, errs.ErrForbidden
	}
	repository, ok := service.repository.(platformrepo.ManagedKubernetesCaptureRepository)
	if !ok {
		return nil, p, errs.ErrUnavailable
	}
	return repository, p, nil
}

// FinalizeManagedKubernetesCapture delegates owner-derived normalization, preparation and adoption.
// It never accepts a success outcome or bytes from the task consumer.
// FinalizeManagedKubernetesCapture передаёт owner-derived normalization/preparation/adoption.
// Не принимает success outcome либо bytes от task consumer.
func (service *Service) FinalizeManagedKubernetesCapture(ctx context.Context, p value.Principal, binding value.ManagedKubernetesCaptureTaskBinding) (entity.ManagedKubernetesCaptureReceipt, error) {
	repository, p, err := service.managedKubernetesCaptureOwner(ctx, p, "platform.artifacts.kubernetes-capture.finalize")
	if err != nil {
		return entity.ManagedKubernetesCaptureReceipt{}, err
	}
	if binding.Validate() != nil {
		return entity.ManagedKubernetesCaptureReceipt{}, errs.ErrInvalid
	}
	return repository.FinalizeManagedKubernetesCapture(ctx, p, binding)
}

// GetManagedKubernetesCapture reconciles exact task metadata without renewing credit or returning bodies.
// GetManagedKubernetesCapture сверяет exact task metadata без продления credit либо выдачи bodies.
func (service *Service) GetManagedKubernetesCapture(ctx context.Context, p value.Principal, binding value.ManagedKubernetesCaptureTaskBinding) (entity.ManagedKubernetesCaptureReceipt, error) {
	repository, p, err := service.managedKubernetesCaptureOwner(ctx, p, "platform.artifacts.kubernetes-capture.get")
	if err != nil {
		return entity.ManagedKubernetesCaptureReceipt{}, err
	}
	if binding.Validate() != nil {
		return entity.ManagedKubernetesCaptureReceipt{}, errs.ErrInvalid
	}
	return repository.GetManagedKubernetesCapture(ctx, p, binding)
}

// RenewManagedKubernetesCapture renews one current artifact attempt, never its source or generation.
// RenewManagedKubernetesCapture продлевает один current artifact attempt, не source либо generation.
func (service *Service) RenewManagedKubernetesCapture(ctx context.Context, p value.Principal, binding value.ManagedKubernetesCaptureTaskBinding) (time.Time, error) {
	repository, p, err := service.managedKubernetesCaptureOwner(ctx, p, "platform.artifacts.kubernetes-capture.renew")
	if err != nil {
		return time.Time{}, err
	}
	if binding.Validate() != nil {
		return time.Time{}, errs.ErrInvalid
	}
	return repository.RenewManagedKubernetesCapture(ctx, p, binding)
}

// CheckManagedKubernetesCaptureReadiness checks the same protected producer path used by the job.
// It does not depend on a worker heartbeat and therefore cannot create a startup cycle.
// CheckManagedKubernetesCaptureReadiness проверяет тот же protected producer path, что использует job.
// Не зависит от worker heartbeat и не создаёт startup cycle.
func (service *Service) CheckManagedKubernetesCaptureReadiness(ctx context.Context, p value.Principal) (uint32, error) {
	repository, p, err := service.managedKubernetesCaptureOwner(ctx, p, "platform.artifacts.kubernetes-capture.readiness")
	if err != nil {
		return 0, err
	}
	return repository.CheckManagedKubernetesCaptureReadiness(ctx, p)
}
