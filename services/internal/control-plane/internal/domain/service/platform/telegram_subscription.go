package platform

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// telegramOwner resolves the verified transport principal once and requires the specialized owner port; no generic command fallback is possible.
// telegramOwner единожды разрешает проверенный transport principal и требует специализированный owner port; generic command fallback невозможен.
func (service *Service) telegramOwner(ctx context.Context, principal value.Principal) (port.TelegramSubscriptionOwner, value.Principal, error) {
	owner, ok := service.repository.(port.TelegramSubscriptionOwner)
	if !ok {
		return nil, value.Principal{}, errs.ErrUnavailable
	}
	resolved, err := service.principal(ctx, principal)
	return owner, resolved, err
}

// ConfigureTelegramSubscription validates bounded user intent then delegates ownership, exact scope, receipt replay and OCC to the specialized owner transaction.
// ConfigureTelegramSubscription проверяет ограниченный user intent и передаёт ownership, exact scope, receipt replay и OCC специализированной owner transaction.
func (service *Service) ConfigureTelegramSubscription(ctx context.Context, principal value.Principal, mutation value.Mutation, input port.TelegramSubscriptionConfigure) (entity.TelegramSubscription, error) {
	owner, resolved, err := service.telegramOwner(ctx, principal)
	if err != nil {
		return entity.TelegramSubscription{}, err
	}
	if input.ProjectRef == "" || len(mutation.IdempotencyKey) < 8 || len(mutation.IdempotencyKey) > 128 || mutation.ExpectedVersion != nil && *mutation.ExpectedVersion < 1 {
		return entity.TelegramSubscription{}, errs.ErrInvalid
	}
	if input.Enabled && (input.ConnectionRef == "" || input.DurationSeconds < 60 || input.DurationSeconds > 604800 || input.MaxEffects < 1 || input.MaxEffects > 10000 || len(input.EventTypes) < 1 || len(input.EventTypes) > 3) {
		return entity.TelegramSubscription{}, errs.ErrInvalid
	}
	if input.Enabled {
		input.EventTypes, err = integrationpackage.NormalizeTelegramEvents(input.EventTypes)
		if err != nil {
			return entity.TelegramSubscription{}, errs.ErrInvalid
		}
	} else if input.ConnectionRef != "" || len(input.EventTypes) > 0 || input.DurationSeconds != 0 || input.MaxEffects != 0 {
		return entity.TelegramSubscription{}, errs.ErrInvalid
	}
	return owner.ConfigureTelegramSubscription(ctx, resolved, mutation, input)
}

// GetTelegramSubscription resolves project eligibility through its authoritative owner; GET cannot enable, extend or transfer a subscription.
// GetTelegramSubscription разрешает project eligibility через авторитетного владельца; GET не включает, не продлевает и не передаёт подписку.
func (service *Service) GetTelegramSubscription(ctx context.Context, principal value.Principal, projectRef string) (*entity.TelegramSubscription, bool, error) {
	owner, resolved, err := service.telegramOwner(ctx, principal)
	if err != nil {
		return nil, false, err
	}
	if projectRef == "" {
		return nil, false, errs.ErrInvalid
	}
	return owner.GetTelegramSubscription(ctx, resolved, projectRef)
}

// ListInteractionDeliveries carries bounded page locators to the owner; each returned Run must remain eligible for this same principal.
// ListInteractionDeliveries передаёт ограниченные page locators владельцу; каждый возвращённый Run должен оставаться доступным тому же principal.
func (service *Service) ListInteractionDeliveries(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.InteractionDeliveryView, string, error) {
	owner, resolved, err := service.telegramOwner(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if filter.ProjectRef == "" || filter.Page.Size < 0 || filter.Page.Size > 100 || len(filter.Page.Token) > 1024 {
		return nil, "", errs.ErrInvalid
	}
	return owner.ListInteractionDeliveries(ctx, resolved, filter)
}

// BeginInteractionDelivery accepts only a full fenced lease and immutable digest; the owner supplies the destination and current permission decision.
// BeginInteractionDelivery принимает лишь полную fenced lease и immutable digest; destination и текущий permission decision задаёт owner.
func (service *Service) BeginInteractionDelivery(ctx context.Context, principal value.Principal, input port.InteractionDeliveryBeginInput) (entity.InteractionDeliveryBegin, error) {
	owner, resolved, err := service.telegramOwner(ctx, principal)
	if err != nil {
		return entity.InteractionDeliveryBegin{}, err
	}
	if input.DeliveryRef == "" || input.LeaseRef == "" || input.Fence == "" || input.Generation < 1 || !validIntegrationCredentialDigest(input.InputDigest) {
		return entity.InteractionDeliveryBegin{}, errs.ErrInvalid
	}
	return owner.BeginInteractionDelivery(ctx, resolved, input)
}
