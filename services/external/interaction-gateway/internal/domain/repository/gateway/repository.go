// Package gateway задаёт gateway-owned PostgreSQL port.
package gateway

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/security/readbackgrant"
)

var ErrNotFound = errors.New("gateway repository record not found")

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
	CompleteInbound(context.Context, entity.InboundEvent, string, string, string) error
	RetryInbound(context.Context, entity.InboundEvent, string, string, string, time.Time, bool) error
	ClaimWaitingInbound(context.Context, time.Duration) (entity.InboundEvent, bool, error)
	LoadCursors(context.Context, []entity.Boundary) (map[string]int64, error)
	AdvanceCursor(context.Context, entity.Boundary, string, int64) error
	HasDeletionPending(context.Context, string, string, string, string) (bool, error)
	CancelDeletion(context.Context, string, string, string, string, string) error
	ResolveThreadSession(context.Context, string, string, string, string) (string, error)
	ListKnownThreads(context.Context, []entity.Boundary, int) (map[string]string, error)
	SaveDownloadGrant(context.Context, entity.DownloadGrant) error
	GetDownloadGrant(context.Context, string) (entity.DownloadGrant, error)
	ConsumeDownloadGrant(context.Context, entity.DownloadGrant, string) error
	RevokeDownloadGrants(context.Context, string, string, string, string) error
	AdmitDeliveryReadbackKeyset(context.Context, uint64, uint64, uint64, string, []readbackgrant.KeyIdentity) error

	EnqueueDelivery(context.Context, entity.Delivery) (entity.Delivery, bool, error)
	ClaimDelivery(context.Context, string, string, time.Duration) (entity.Delivery, bool, error)
	GetUploadReceipt(context.Context, entity.Delivery, string) (entity.UploadReceipt, bool, error)
	SaveUploadReceipt(context.Context, entity.Delivery, entity.UploadReceipt) error
	MarkProviderAccepted(context.Context, entity.Delivery, string, string, string) error
	CompleteDelivery(context.Context, entity.Delivery) error
	RetryDelivery(context.Context, entity.Delivery, string, time.Time, bool) error
	GetDelivery(context.Context, string) (entity.Delivery, error)
	GetDeliveryScoped(context.Context, string, string, string) (entity.Delivery, error)
	GetDeliveryByProviderPost(context.Context, string) (entity.Delivery, error)
	ListPendingReactionPosts(context.Context, []entity.Boundary, int) (map[string]string, error)
	MarkOwnerGateDecided(context.Context, entity.Delivery) error
	ClaimOwnerGateRequest(context.Context) (string, bool, error)
	SaveOwnerGateClaim(context.Context, string, entity.Delivery) error
	CompleteOwnerGateClaim(context.Context, string) error
}
