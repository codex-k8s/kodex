package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesCaptureRepairOwner resolves the authenticated public caller and exact operation before
// reaching the optional owner port. Absence never falls back to a generic retry or artifact command.
// kubernetesCaptureRepairOwner разрешает authenticated public caller и exact operation до optional
// owner port. Отсутствие не включает generic retry либо artifact command.
func (service *Service) kubernetesCaptureRepairOwner(ctx context.Context, principal value.Principal, permission string) (platformrepo.KubernetesCaptureRepairRepository, value.Principal, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, principal, err
	}
	if principal.CallerWorkload != "control-api-gateway" || principal.Permission != permission {
		return nil, principal, errs.ErrForbidden
	}
	repository, available := service.repository.(platformrepo.KubernetesCaptureRepairRepository)
	if !available {
		return nil, principal, errs.ErrUnavailable
	}
	return repository, principal, nil
}

// GetKubernetesCaptureStatus forwards an exact original-key metadata read; owner visibility is
// current and no bytes, private stage locator or task admission can be recovered by this method.
// GetKubernetesCaptureStatus передаёт exact original-key metadata read; owner visibility остаётся
// current, bytes, private stage locator либо task admission не восстанавливаются.
func (service *Service) GetKubernetesCaptureStatus(ctx context.Context, principal value.Principal, input query.KubernetesCaptureStatusRead) (entity.KubernetesCaptureStatusReadback, error) {
	repository, principal, err := service.kubernetesCaptureRepairOwner(ctx, principal, "platform.integrations.kubernetes.capture.get")
	if err != nil {
		return entity.KubernetesCaptureStatusReadback{}, err
	}
	if !validKubernetesSetupRef(input.CaptureRef, "koc_") || input.RepairIdempotencyKey != "" && !validKubernetesSetupIdempotency(input.RepairIdempotencyKey) || input.CleanupIdempotencyKey != "" && !validKubernetesSetupIdempotency(input.CleanupIdempotencyKey) {
		return entity.KubernetesCaptureStatusReadback{}, errs.ErrInvalid
	}
	return repository.GetKubernetesCaptureStatus(ctx, principal, input)
}

// RepairKubernetesCapture forwards one bounded owner command after transport validation. The
// repository must separately require original-root visibility and current artifact.upload; a view
// permission, idempotency key or expected version can never authorize this mutation by itself.
// RepairKubernetesCapture передаёт bounded owner command после transport validation. Repository
// отдельно требует original-root visibility и current artifact.upload; view permission, idempotency
// key либо expected version сами по себе не разрешают mutation.
func (service *Service) RepairKubernetesCapture(ctx context.Context, principal value.Principal, mutation value.Mutation, input command.KubernetesCaptureRepair) (entity.KubernetesCaptureStatusReadback, error) {
	repository, principal, err := service.kubernetesCaptureRepairOwner(ctx, principal, "platform.integrations.kubernetes.capture.repair")
	if err != nil {
		return entity.KubernetesCaptureStatusReadback{}, err
	}
	if !validKubernetesSetupRef(input.CaptureRef, "koc_") || !validKubernetesSetupMutation(mutation, false) {
		return entity.KubernetesCaptureStatusReadback{}, errs.ErrInvalid
	}
	return repository.RepairKubernetesCapture(ctx, principal, mutation, input)
}

// RepairKubernetesStagingCleanup admits only original-owner cleanup repair through a closed port.
// The storage owner independently checks current upload authority, OCC and immutable exhaustion.
// RepairKubernetesStagingCleanup допускает только original-owner cleanup repair через closed port.
// Storage owner отдельно проверяет current upload authority, OCC и immutable exhaustion.
func (service *Service) RepairKubernetesStagingCleanup(ctx context.Context, principal value.Principal, mutation value.Mutation, input command.KubernetesCaptureRepair) (entity.KubernetesCaptureStatusReadback, error) {
	_, principal, err := service.kubernetesCaptureRepairOwner(ctx, principal, "platform.integrations.kubernetes.capture.cleanup.repair")
	if err != nil {
		return entity.KubernetesCaptureStatusReadback{}, err
	}
	if !validKubernetesSetupRef(input.CaptureRef, "koc_") || !validKubernetesSetupMutation(mutation, false) {
		return entity.KubernetesCaptureStatusReadback{}, errs.ErrInvalid
	}
	repository, available := service.repository.(platformrepo.KubernetesStagingCleanupRepository)
	if !available {
		return entity.KubernetesCaptureStatusReadback{}, errs.ErrUnavailable
	}
	return repository.RepairKubernetesStagingCleanup(ctx, principal, mutation, input)
}
