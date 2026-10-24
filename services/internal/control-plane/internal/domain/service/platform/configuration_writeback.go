package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// writeBackOwner resolves principal and the write-back repository; worker actions additionally
// require integration-gateway, the exact action permission and no project scope.
// writeBackOwner разрешает principal и repository write-back; действия worker дополнительно
// требуют integration-gateway, точное permission действия и отсутствие области проекта.
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

// GetConfigurationWriteBack resolves write-back authority, checks the bounded proposal reference
// and delegates the authoritative view including any permitted content to its owner.
// GetConfigurationWriteBack разрешает полномочия write-back, проверяет ограниченную ссылку
// предложения и передаёт авторитетное чтение, включая допустимое содержимое, владельцу.
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

// ListConfigurationWriteBacks resolves the write-back owner and bounds the configuration reference
// and cursor without replacing owner filtering or pagination.
// ListConfigurationWriteBacks разрешает владельца write-back и ограничивает ссылку конфигурации и
// курсор без замены фильтрации либо пагинации владельца.
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

// ClaimConfigurationWriteBackWork requires exact claim authority, bounds claimant identity and a
// one-to-sixteen batch and delegates transactional lease selection to the write-back owner.
// ClaimConfigurationWriteBackWork требует точные полномочия claim, ограничивает identity claimant
// и пакет от одного до шестнадцати и передаёт транзакционный выбор leases владельцу write-back.
func (service *Service) ClaimConfigurationWriteBackWork(ctx context.Context, p value.Principal, claimant string, limit int32) ([]entity.ConfigurationWriteBackWork, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "claim")
	if err != nil {
		return nil, err
	}
	if !validConfigurationWorkClaim(claimant, limit) {
		return nil, errs.ErrInvalid
	}
	return owner.ClaimConfigurationWriteBackWork(ctx, p, claimant, limit)
}

// RenewConfigurationWriteBackWork requires the distinct renew worker permission and forwards the
// exact lease for authoritative fencing and renewal.
// RenewConfigurationWriteBackWork требует отдельное permission worker для renew и передаёт точный
// lease для авторитетной проверки fencing и продления.
func (service *Service) RenewConfigurationWriteBackWork(ctx context.Context, p value.Principal, lease entity.ConfigurationWriteBackLease) (entity.ConfigurationWriteBackLease, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "renew")
	if err != nil {
		return entity.ConfigurationWriteBackLease{}, err
	}
	return owner.RenewConfigurationWriteBackWork(ctx, p, lease)
}

// BeginConfigurationWriteBackEffect requires exact begin worker authority and delegates effect
// admission and its boolean result to the owner without starting an external write here.
// BeginConfigurationWriteBackEffect требует точные полномочия worker для begin и передаёт
// владельцу допуск эффекта и его bool-результат без запуска внешней записи здесь.
func (service *Service) BeginConfigurationWriteBackEffect(ctx context.Context, p value.Principal, input port.ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, bool, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "begin")
	if err != nil {
		return entity.ConfigurationWriteBack{}, false, err
	}
	return owner.BeginConfigurationWriteBackEffect(ctx, p, input)
}

// CompleteConfigurationWriteBackEffect requires exact complete worker authority and forwards
// immutable effect input for owner-side receipt/lifecycle completion.
// CompleteConfigurationWriteBackEffect требует точные полномочия worker для complete и передаёт
// immutable-ввод эффекта для завершения receipt и lifecycle владельцем.
func (service *Service) CompleteConfigurationWriteBackEffect(ctx context.Context, p value.Principal, input port.ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "complete")
	if err != nil {
		return entity.ConfigurationWriteBack{}, err
	}
	return owner.CompleteConfigurationWriteBackEffect(ctx, p, input)
}

// FailConfigurationWriteBackWork requires exact fail worker authority and forwards the lease and
// failure code so the owner decides the durable outcome.
// FailConfigurationWriteBackWork требует точные полномочия worker для fail и передаёт lease и код
// ошибки владельцу для определения устойчивого результата.
func (service *Service) FailConfigurationWriteBackWork(ctx context.Context, p value.Principal, lease entity.ConfigurationWriteBackLease, failure string) (entity.ConfigurationWriteBack, error) {
	p, owner, err := service.writeBackOwner(ctx, p, "fail")
	if err != nil {
		return entity.ConfigurationWriteBack{}, err
	}
	return owner.FailConfigurationWriteBackWork(ctx, p, lease, failure)
}
