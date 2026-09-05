package platform

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

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

func (service *Service) ClaimConfigurationSourceWork(ctx context.Context, p value.Principal, claimant string, limit int32) ([]entity.ManagedConfigurationSourceWork, error) {
	p, owner, err := service.sourceWorker(ctx, p, "claim")
	if err != nil {
		return nil, err
	}
	if len(claimant) == 0 || len(claimant) > 128 || strings.ContainsAny(claimant, "\x00\r\n") || limit < 1 || limit > 16 {
		return nil, errs.ErrInvalid
	}
	return owner.ClaimConfigurationSourceWork(ctx, p, claimant, limit)
}

func (service *Service) RenewConfigurationSourceWork(ctx context.Context, p value.Principal, lease entity.ManagedConfigurationSourceLease) (entity.ManagedConfigurationSourceLease, error) {
	p, owner, err := service.sourceWorker(ctx, p, "renew")
	if err != nil {
		return entity.ManagedConfigurationSourceLease{}, err
	}
	return owner.RenewConfigurationSourceWork(ctx, p, lease)
}

func (service *Service) CompleteConfigurationSourceWork(ctx context.Context, p value.Principal, input platformrepo.ConfigurationSourceCompletion) (entity.ManagedConfigurationGitSource, error) {
	p, owner, err := service.sourceWorker(ctx, p, "complete")
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	return owner.CompleteConfigurationSourceWork(ctx, p, input)
}

func (service *Service) FailConfigurationSourceWork(ctx context.Context, p value.Principal, lease entity.ManagedConfigurationSourceLease, failure string) (entity.ManagedConfigurationGitSource, error) {
	p, owner, err := service.sourceWorker(ctx, p, "fail")
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	return owner.FailConfigurationSourceWork(ctx, p, lease, failure)
}
