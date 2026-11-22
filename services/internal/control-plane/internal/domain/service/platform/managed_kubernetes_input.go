package platform

import (
	"bytes"
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ClaimManagedKubernetesInputFrame delegates one original-binding claim to the purpose owner.
// A missing port or malformed delivery cannot fall back to generic artifact reads or repeat payload.
// ClaimManagedKubernetesInputFrame передаёт один original-binding claim purpose owner.
// Missing port/malformed delivery не заменяется generic artifact read и не повторяет payload.
func (service *Service) ClaimManagedKubernetesInputFrame(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, streamRef string) (entity.ManagedKubernetesInputFrameAdmission, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ManagedKubernetesInputFrameAdmission{}, err
	}
	if p.CallerWorkload != "integration-gateway" || p.Permission != "platform.runtime.kubernetes.input.claim" || p.ProjectRef != "" {
		return entity.ManagedKubernetesInputFrameAdmission{}, errs.ErrForbidden
	}
	if binding.Validate() != nil || binding.InvocationRef == "" || binding.ConnectionTestRef != "" || !managedkubernetes.ValidLocator(streamRef) || len(streamRef) > 96 {
		return entity.ManagedKubernetesInputFrameAdmission{}, errs.ErrInvalid
	}
	repository, exists := service.repository.(platformrepo.ManagedKubernetesInputRepository)
	if !exists {
		return entity.ManagedKubernetesInputFrameAdmission{}, errs.ErrUnavailable
	}
	result, err := repository.ClaimManagedKubernetesInputFrame(ctx, p, binding, streamRef)
	if err != nil {
		if result.Frame != nil {
			clear(result.Frame.Payload)
		}
		return entity.ManagedKubernetesInputFrameAdmission{}, err
	}
	if result.Frame != nil {
		original := result.Frame.Payload
		if len(original) > managedkubernetes.MaximumFrameBytes {
			clear(original)
			return entity.ManagedKubernetesInputFrameAdmission{}, errs.ErrUnavailable
		}
		frame := *result.Frame
		frame.Payload = bytes.Clone(original)
		clear(original)
		result.Frame = &frame
	}
	if !result.Disposition.Known() || result.Disposition == enum.KubernetesInputFrameDeliver && (result.Frame == nil || result.Frame.Validate(binding, streamRef, time.Now()) != nil) || result.Disposition != enum.KubernetesInputFrameDeliver && result.Frame != nil {
		if result.Frame != nil {
			clear(result.Frame.Payload)
		}
		return entity.ManagedKubernetesInputFrameAdmission{}, errs.ErrUnavailable
	}
	return result, nil
}

// CheckManagedKubernetesOutputContract reports only actual optional owner output compatibility.
// Missing support stays revision zero; it never downgrades to generic artifact authority or full readiness.
// CheckManagedKubernetesOutputContract сообщает лишь actual optional owner output compatibility.
// Missing support остаётся revision0; generic artifact authority либо full readiness не подменяются.
func (service *Service) CheckManagedKubernetesOutputContract(ctx context.Context, p value.Principal) (uint32, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return 0, err
	}
	return service.checkManagedKubernetesOutputContractResolved(ctx, p)
}

// checkManagedKubernetesOutputContractResolved reuses an already verified principal without resolving public refs twice.
// checkManagedKubernetesOutputContractResolved использует already verified principal без повторного разрешения public refs.
func (service *Service) checkManagedKubernetesOutputContractResolved(ctx context.Context, p value.Principal) (uint32, error) {
	if p.CallerWorkload != "secret-broker" || p.Permission != "platform.runtime.kubernetes.readiness" || p.ProjectRef != "" {
		return 0, errs.ErrForbidden
	}
	repository, exists := service.repository.(platformrepo.ManagedKubernetesOutputReadinessRepository)
	if !exists {
		return 0, nil
	}
	revision, err := repository.ManagedKubernetesOutputContract(ctx, p)
	if err != nil {
		return 0, err
	}
	if revision != 0 && revision != managedkubernetes.OutputContractRevision {
		return 0, errs.ErrUnavailable
	}
	return revision, nil
}
