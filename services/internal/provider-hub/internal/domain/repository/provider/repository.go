package provider

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

// Repository is the storage boundary owned by provider-hub.
//
// Business methods are added together with concrete provider workflows. The
// initial scaffold keeps only the readiness contract needed by the process.
type Repository interface {
	Ping(context.Context) error
	StoreWebhookEvent(context.Context, entity.WebhookEvent, []entity.ProviderEvent, []entity.OutboxEvent) (entity.WebhookEvent, []entity.ProviderEvent, error)
	ProcessWebhookEvent(context.Context, entity.WebhookEvent, []entity.ProviderEvent, []entity.OutboxEvent) (entity.WebhookEvent, error)
	GetWebhookEvent(context.Context, uuid.UUID) (entity.WebhookEvent, error)
	ListWebhookEvents(context.Context, query.WebhookEventFilter) ([]entity.WebhookEvent, query.PageResult, error)
	ListProviderEvents(context.Context, query.ProviderEventFilter) ([]entity.ProviderEvent, query.PageResult, error)
	UpsertAccountRuntimeState(context.Context, entity.ProviderAccountRuntimeState) (entity.ProviderAccountRuntimeState, error)
	GetAccountRuntimeState(context.Context, query.AccountRuntimeStateLookup) (entity.ProviderAccountRuntimeState, error)
	ListAccountRuntimeStates(context.Context, query.AccountRuntimeStateFilter) ([]entity.ProviderAccountRuntimeState, query.PageResult, error)
	RecordLimitSnapshot(context.Context, entity.ProviderLimitSnapshot, entity.ProviderAccountRuntimeState) (entity.ProviderLimitSnapshot, error)
	ListLimitSnapshots(context.Context, query.LimitSnapshotFilter) ([]entity.ProviderLimitSnapshot, query.PageResult, error)
	RecordProviderOperation(context.Context, entity.ProviderOperation) (entity.ProviderOperation, error)
	ListProviderOperations(context.Context, query.ProviderOperationFilter) ([]entity.ProviderOperation, query.PageResult, error)
	ClaimOutboxEvents(context.Context, int, time.Time, time.Time) ([]entity.OutboxEvent, error)
	MarkOutboxEventPublished(context.Context, uuid.UUID, int, time.Time) error
	MarkOutboxEventFailed(context.Context, uuid.UUID, int, time.Time, string) error
	MarkOutboxEventPermanentlyFailed(context.Context, uuid.UUID, int, time.Time, string) error
}

// WebhookNormalizer isolates provider-specific webhook payload parsing from the domain service.
type WebhookNormalizer interface {
	ProviderSlug() enum.ProviderSlug
	NormalizeWebhook(entity.WebhookEvent) (value.ProviderWebhookFacts, bool, error)
}

// Clock provides deterministic time for domain commands and tests.
type Clock interface {
	Now() time.Time
}

// IDGenerator provides aggregate identifiers for domain commands.
type IDGenerator interface {
	New() uuid.UUID
}
