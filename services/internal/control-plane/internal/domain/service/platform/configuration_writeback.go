package platform

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) writeBackOwner(ctx context.Context, p value.Principal, workAction string) (value.Principal, port.ConfigurationWriteBackRepository, error) {
	if workAction != "" && (p.CallerWorkload != "integration-gateway" || p.Permission != "platform.configuration-writebacks.work."+workAction || p.ProjectRef != "") {
		return value.Principal{}, nil, errs.ErrForbidden
	}
	p, err := service.principal(ctx, p)
	if err != nil {
		return value.Principal{}, nil, err
	}
	owner, ok := service.repository.(port.ConfigurationWriteBackRepository)
	if !ok {
		return value.Principal{}, nil, errs.ErrUnavailable
	}
	return p, owner, nil
}

func (service *Service) GetConfigurationWriteBack(ctx context.Context, p value.Principal, ref string) (entity.ConfigurationWriteBackView, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "")
	if err != nil {
		return entity.ConfigurationWriteBackView{}, err
	}
	if len(ref) == 0 || len(ref) > 128 {
		return entity.ConfigurationWriteBackView{}, errs.ErrInvalid
	}
	return owner.GetConfigurationWriteBack(ctx, p, ref)
}
func (service *Service) ListConfigurationWriteBacks(ctx context.Context, p value.Principal, ref string, filter query.Filter) ([]entity.ConfigurationWriteBack, string, int64, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "")
	if err != nil {
		return nil, "", 0, err
	}
	if len(ref) == 0 || len(ref) > 128 || len(filter.Page.Token) > 2048 {
		return nil, "", 0, errs.ErrInvalid
	}
	return owner.ListConfigurationWriteBacks(ctx, p, ref, filter)
}
func (service *Service) ClaimConfigurationWriteBackWork(ctx context.Context, p value.Principal, claimant string, limit int32) ([]entity.ConfigurationWriteBackWork, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "claim")
	if err != nil {
		return nil, err
	}
	if len(claimant) == 0 || len(claimant) > 128 || strings.ContainsAny(claimant, "\x00\r\n") || limit < 1 || limit > 16 {
		return nil, errs.ErrInvalid
	}
	return owner.ClaimConfigurationWriteBackWork(ctx, p, claimant, limit)
}
func (service *Service) RenewConfigurationWriteBackWork(ctx context.Context, p value.Principal, lease entity.ConfigurationWriteBackLease) (entity.ConfigurationWriteBackLease, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "renew")
	if err != nil {
		return entity.ConfigurationWriteBackLease{}, err
	}
	return owner.RenewConfigurationWriteBackWork(ctx, p, lease)
}
func (service *Service) BeginConfigurationWriteBackEffect(ctx context.Context, p value.Principal, input port.ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, bool, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "begin")
	if err != nil {
		return entity.ConfigurationWriteBack{}, false, err
	}
	return owner.BeginConfigurationWriteBackEffect(ctx, p, input)
}
func (service *Service) CompleteConfigurationWriteBackEffect(ctx context.Context, p value.Principal, input port.ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "complete")
	if err != nil {
		return entity.ConfigurationWriteBack{}, err
	}
	return owner.CompleteConfigurationWriteBackEffect(ctx, p, input)
}
func (service *Service) FailConfigurationWriteBackWork(ctx context.Context, p value.Principal, lease entity.ConfigurationWriteBackLease, failure string) (entity.ConfigurationWriteBack, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "fail")
	if err != nil {
		return entity.ConfigurationWriteBack{}, err
	}
	return owner.FailConfigurationWriteBackWork(ctx, p, lease, failure)
}
