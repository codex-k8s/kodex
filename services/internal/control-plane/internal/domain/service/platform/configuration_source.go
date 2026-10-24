package platform

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// sourceWorker requires integration-gateway, the exact source-work action permission and no
// project scope, then resolves principal and the specialized repository port.
// sourceWorker требует integration-gateway, точное permission действия source work и отсутствие
// области проекта, затем разрешает principal и специализированный repository port.
func (service *Service) sourceWorker(ctx context.Context, p value.Principal, action string) (value.Principal, platformrepo.ConfigurationSourceWorkRepository, error) {
	if p.CallerWorkload != "integration-gateway" || p.Permission != "platform.configuration-sources.work."+action || p.ProjectRef != "" {
		return value.Principal{}, nil, errs.ErrForbidden
	}
	p, err := service.principal(ctx, p)
	if err != nil {
		return value.Principal{}, nil, err
	}
	owner, ok := service.repository.(platformrepo.ConfigurationSourceWorkRepository)
	if !ok {
		return value.Principal{}, nil, errs.ErrUnavailable
	}
	return p, owner, nil
}

// ClaimConfigurationSourceWork checks claim authority and bounded claimant/batch input before the
// owner selects exact source-work leases.
// ClaimConfigurationSourceWork проверяет полномочия claim и ограниченные ввод claimant и пакета до
// выбора точных leases работы источника владельцем.
func (service *Service) ClaimConfigurationSourceWork(ctx context.Context, p value.Principal, claimant string, limit int32) ([]entity.ManagedConfigurationSourceWork, error) {
	p, owner, err := service.sourceWorker(ctx, p, "claim")
	if err != nil {
		return nil, err
	}
	if !validConfigurationWorkClaim(claimant, limit) {
		return nil, errs.ErrInvalid
	}
	return owner.ClaimConfigurationSourceWork(ctx, p, claimant, limit)
}

// RenewConfigurationSourceWork requires the source-work renew permission and forwards the supplied
// exact lease for owner validation and renewal.
// RenewConfigurationSourceWork требует permission renew работы источника и передаёт точный lease
// для проверки и продления владельцем.
func (service *Service) RenewConfigurationSourceWork(ctx context.Context, p value.Principal, lease entity.ManagedConfigurationSourceLease) (entity.ManagedConfigurationSourceLease, error) {
	p, owner, err := service.sourceWorker(ctx, p, "renew")
	if err != nil {
		return entity.ManagedConfigurationSourceLease{}, err
	}
	return owner.RenewConfigurationSourceWork(ctx, p, lease)
}

// CompleteConfigurationSourceWork requires exact source-work completion authority and delegates
// accepted immutable source observations and durable state transition to the owner.
// CompleteConfigurationSourceWork требует точные полномочия завершения source work и передаёт
// принятые immutable-наблюдения источника и устойчивый переход состояния владельцу.
func (service *Service) CompleteConfigurationSourceWork(ctx context.Context, p value.Principal, input platformrepo.ConfigurationSourceCompletion) (entity.ManagedConfigurationGitSource, error) {
	p, owner, err := service.sourceWorker(ctx, p, "complete")
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	return owner.CompleteConfigurationSourceWork(ctx, p, input)
}

// FailConfigurationSourceWork requires the source-work fail permission and forwards the lease and
// failure code without locally deciding retry or terminal state.
// FailConfigurationSourceWork требует permission fail работы источника и передаёт lease и код
// ошибки без локального решения о retry либо terminal-состоянии.
func (service *Service) FailConfigurationSourceWork(ctx context.Context, p value.Principal, lease entity.ManagedConfigurationSourceLease, failure string) (entity.ManagedConfigurationGitSource, error) {
	p, owner, err := service.sourceWorker(ctx, p, "fail")
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	return owner.FailConfigurationSourceWork(ctx, p, lease, failure)
}

// validConfigurationWorkClaim applies the shared claimant shape and batch bounds for source and
// write-back work; caller authority and transactional lease selection remain with each owner.
// validConfigurationWorkClaim применяет общие ограничения формы claimant и размера пакета для
// source и write-back; полномочия caller и транзакционный выбор leases остаются у каждого владельца.
func validConfigurationWorkClaim(claimant string, limit int32) bool {
	return !(len(claimant) == 0 || len(claimant) > 128 || strings.ContainsAny(claimant, "\x00\r\n") || limit < 1 || limit > 16)
}
