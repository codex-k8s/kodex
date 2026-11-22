package platform

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// CheckManagedKubernetesInputContract reports actual private INPUT compatibility to the protected broker.
// No port or unknown revision is converted into success, even if core identity/OUTPUT is ready.
// CheckManagedKubernetesInputContract сообщает actual private INPUT compatibility protected broker.
// Missing port либо unknown revision не становятся success даже при готовом core identity/OUTPUT.
func (service *Service) CheckManagedKubernetesInputContract(ctx context.Context, p value.Principal) (uint32, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return 0, err
	}
	if p.CallerWorkload != "secret-broker" || p.Permission != "platform.runtime.kubernetes.readiness" || p.ProjectRef != "" {
		return 0, errs.ErrForbidden
	}
	if _, ok := service.repository.(platformrepo.ManagedKubernetesInputRepository); !ok {
		return 0, nil
	}
	if _, ok := service.repository.(platformrepo.KubernetesStreamInputRepository); !ok {
		return 0, nil
	}
	owner, ok := service.repository.(platformrepo.ManagedKubernetesInputReadinessRepository)
	if !ok {
		return 0, nil
	}
	revision, err := owner.ManagedKubernetesInputContract(ctx, p)
	if err != nil {
		return 0, err
	}
	if revision != 0 && revision != managedkubernetes.InputContractRevision {
		return 0, errs.ErrUnavailable
	}
	return revision, nil
}

// CheckManagedKubernetesFileInputContract requires the materializing port and real source readiness.
// It does not authorize an artifact, native write or source merely from a nonzero wire integer.
// CheckManagedKubernetesFileInputContract требует materializing port и real source readiness.
// Nonzero wire integer сам не разрешает artifact, native write либо source.
func (service *Service) CheckManagedKubernetesFileInputContract(ctx context.Context, p value.Principal) (uint32, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return 0, err
	}
	if p.CallerWorkload != "secret-broker" || p.Permission != "platform.runtime.kubernetes.readiness" || p.ProjectRef != "" {
		return 0, errs.ErrForbidden
	}
	if _, ok := service.repository.(platformrepo.KubernetesFileInputRepository); !ok {
		return 0, nil
	}
	if _, ok := service.repository.(platformrepo.KubernetesFileCleanupRepository); !ok {
		return 0, nil
	}
	owner, ok := service.repository.(platformrepo.ManagedKubernetesFileReadinessRepository)
	if !ok {
		return 0, nil
	}
	revision, err := owner.ManagedKubernetesFileInputContract(ctx, p)
	if err != nil {
		return 0, err
	}
	if revision != 0 && revision != managedkubernetes.FileInputContractRevision {
		return 0, errs.ErrUnavailable
	}
	return revision, nil
}
