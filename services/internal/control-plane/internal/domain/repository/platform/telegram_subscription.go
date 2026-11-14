package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TelegramSubscriptionConfigure carries owner proposals only; the project/connection boundary, bot/chat/topic and grant identity are resolved by CP.
// TelegramSubscriptionConfigure содержит лишь owner proposals; границы project/connection, bot/chat/topic и grant identity разрешает CP.
type TelegramSubscriptionConfigure struct {
	ProjectRef, ConnectionRef   string
	Enabled                     bool
	EventTypes                  []string
	DurationSeconds, MaxEffects int32
}

// InteractionDeliveryBeginInput identifies already claimed owner work and its immutable input, never a caller-chosen destination or body.
// InteractionDeliveryBeginInput именует уже арендованную owner работу и immutable input, но не выбранные caller destination либо body.
type InteractionDeliveryBeginInput struct {
	DeliveryRef, LeaseRef, Fence, InputDigest string
	Generation                                int64
}

// TelegramSubscriptionOwner keeps project subscription commands and status reads separate from generic grants or arbitrary transitions.
// TelegramSubscriptionOwner отделяет команды проектной подписки и чтение статуса от generic grants либо произвольных transitions.
type TelegramSubscriptionOwner interface {
	// ConfigureTelegramSubscription commits exact project configuration and its internal grant after current authority, receipt and OCC checks; it does not approve a Human Gate.
	// ConfigureTelegramSubscription фиксирует exact project configuration и internal grant после текущих authority, receipt и OCC checks, не одобряя Human Gate.
	ConfigureTelegramSubscription(context.Context, value.Principal, value.Mutation, TelegramSubscriptionConfigure) (entity.TelegramSubscription, error)
	// GetTelegramSubscription reads only current eligible project configuration; an absent subscription remains distinguishable from hidden project state.
	// GetTelegramSubscription читает лишь текущую доступную project configuration; отсутствие подписки отличается от скрытого состояния проекта.
	GetTelegramSubscription(context.Context, value.Principal, string) (*entity.TelegramSubscription, bool, error)
	// ListInteractionDeliveries applies the same project/run eligibility on every bounded page and excludes private transport metadata.
	// ListInteractionDeliveries применяет одинаковую project/run eligibility каждой ограниченной страницы и исключает private transport metadata.
	ListInteractionDeliveries(context.Context, value.Principal, query.Filter) ([]entity.InteractionDeliveryView, string, error)
	// BeginInteractionDelivery linearizes one send permission under the full current subscription, grant, root and lease tuple; replay cannot authorize another send.
	// BeginInteractionDelivery фиксирует одно send permission под полной текущей связкой subscription, grant, root и lease; replay не разрешает ещё один send.
	BeginInteractionDelivery(context.Context, value.Principal, InteractionDeliveryBeginInput) (entity.InteractionDeliveryBegin, error)
}
