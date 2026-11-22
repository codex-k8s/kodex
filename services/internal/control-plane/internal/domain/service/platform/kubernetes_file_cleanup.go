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

// kubernetesFileCleanupOwner verifies the real caller before the optional closure-only port.
// It neither borrows source visibility nor disables the independent core metadata port when absent.
// kubernetesFileCleanupOwner проверяет real caller до optional closure-only port.
// Не заимствует source visibility и не отключает независимый core metadata port при отсутствии.
func (service *Service) kubernetesFileCleanupOwner(ctx context.Context, p value.Principal, permission string) (platformrepo.KubernetesFileCleanupRepository, value.Principal, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, p, err
	}
	if p.CallerWorkload != "control-api-gateway" || p.Permission != permission {
		return nil, p, errs.ErrForbidden
	}
	owner, ok := service.repository.(platformrepo.KubernetesFileCleanupRepository)
	if !ok {
		return nil, p, errs.ErrUnavailable
	}
	return owner, p, nil
}

// ListKubernetesFileCleanup forwards minimal discovery under original-root/run.view, never artifact-byte authority.
// ListKubernetesFileCleanup передаёт minimal discovery под original-root/run.view, не artifact-byte authority.
func (service *Service) ListKubernetesFileCleanup(ctx context.Context, p value.Principal, invocationRef string) ([]entity.KubernetesFileCleanupView, error) {
	owner, p, err := service.kubernetesFileCleanupOwner(ctx, p, "platform.integrations.kubernetes.file.list")
	if err != nil {
		return nil, err
	}
	if !validKubernetesSetupRef(invocationRef, "inv_") {
		return nil, errs.ErrInvalid
	}
	return owner.ListKubernetesFileCleanup(ctx, p, invocationRef)
}

// GetKubernetesFileCleanup reads only the exact actor/source original-key acknowledgement without reissuing repair.
// GetKubernetesFileCleanup читает лишь exact actor/source original-key acknowledgement без повторного repair.
func (service *Service) GetKubernetesFileCleanup(ctx context.Context, p value.Principal, input query.KubernetesFileCleanupRead) (entity.KubernetesFileCleanupReadback, error) {
	owner, p, err := service.kubernetesFileCleanupOwner(ctx, p, "platform.integrations.kubernetes.file.get")
	if err != nil {
		return entity.KubernetesFileCleanupReadback{}, err
	}
	if !validKubernetesSetupRef(input.InvocationRef, "inv_") || !validKubernetesSetupRef(input.SourceRef, "kfs_") || input.CleanupIdempotencyKey != "" && !validKubernetesSetupIdempotency(input.CleanupIdempotencyKey) {
		return entity.KubernetesFileCleanupReadback{}, errs.ErrInvalid
	}
	return owner.GetKubernetesFileCleanup(ctx, p, input)
}

// RepairKubernetesFileCleanup forwards only one bounded cleanup repair, retaining OCC and idempotency as concurrency guards.
// The owner must require current scoped upload allowance before replay/OCC and preserve its first bound through commit.
// RepairKubernetesFileCleanup передаёт лишь bounded cleanup repair, сохраняя OCC и idempotency как concurrency guards.
// Owner требует current scoped upload allowance до replay/OCC и сохраняет first bound через commit.
func (service *Service) RepairKubernetesFileCleanup(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesFileCleanupRepair) (entity.KubernetesFileCleanupReadback, error) {
	owner, p, err := service.kubernetesFileCleanupOwner(ctx, p, "platform.integrations.kubernetes.file.cleanup.repair")
	if err != nil {
		return entity.KubernetesFileCleanupReadback{}, err
	}
	if !validKubernetesSetupRef(input.InvocationRef, "inv_") || !validKubernetesSetupRef(input.SourceRef, "kfs_") || !validKubernetesSetupMutation(mutation, false) {
		return entity.KubernetesFileCleanupReadback{}, errs.ErrInvalid
	}
	return owner.RepairKubernetesFileCleanup(ctx, p, mutation, input)
}
