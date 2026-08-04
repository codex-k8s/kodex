// Package gateway задаёт gateway-owned PostgreSQL port.
package gateway

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
)

type InboundDisposition uint8

const (
	InboundClaimed InboundDisposition = iota + 1
	InboundReplay
	InboundBusy
)

type Repository interface {
	Check(context.Context) error
	ClaimInbound(context.Context, entity.InboundEvent, time.Duration) (entity.InboundEvent, InboundDisposition, error)
	SaveInboundProgress(context.Context, entity.InboundEvent) error
	CompleteInbound(context.Context, string, string, string) error
	RetryInbound(context.Context, string, string, time.Time, bool) error
	ClaimWaitingInbound(context.Context, time.Duration) (entity.InboundEvent, bool, error)
	LoadCursors(context.Context, []string) (map[string]int64, error)
	AdvanceCursor(context.Context, string, int64) error

	EnqueueDelivery(context.Context, entity.Delivery) (entity.Delivery, bool, error)
	ClaimDelivery(context.Context, string, string, time.Duration) (entity.Delivery, bool, error)
	MarkProviderAccepted(context.Context, string, uint64, string, string, string, string) error
	CompleteDelivery(context.Context, string, uint64) error
	RetryDelivery(context.Context, string, uint64, string, time.Time, bool) error
	GetDelivery(context.Context, string) (entity.Delivery, error)
	GetDeliveryByProviderPost(context.Context, string) (entity.Delivery, error)
	ListPendingReactionPosts(context.Context, int) (map[string]string, error)
	MarkOwnerGateDecided(context.Context, string) error

	ClaimOwnerGateRequest(context.Context) (string, bool, error)
	SaveOwnerGateClaim(context.Context, string, entity.Delivery) error
	CompleteOwnerGateClaim(context.Context, string) error
}
