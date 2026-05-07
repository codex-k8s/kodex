package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
)

// Service is the domain entrypoint for provider-native work item workflows.
type Service struct {
	repository providerrepo.Repository
	clock      providerrepo.Clock
	ids        providerrepo.IDGenerator
}

// New creates a provider-hub domain service.
func New(repository providerrepo.Repository) *Service {
	return NewWithRuntime(repository, systemClock{}, uuidGenerator{})
}

// NewWithRuntime creates a provider-hub domain service with deterministic runtime dependencies.
func NewWithRuntime(repository providerrepo.Repository, clock providerrepo.Clock, ids providerrepo.IDGenerator) *Service {
	if repository == nil {
		panic("provider-hub repository is required")
	}
	if clock == nil {
		panic("provider-hub clock is required")
	}
	if ids == nil {
		panic("provider-hub id generator is required")
	}
	return &Service{repository: repository, clock: clock, ids: ids}
}

// Ping checks whether the service can reach its owned storage.
func (s *Service) Ping(ctx context.Context) error {
	return s.repository.Ping(ctx)
}

// IngestWebhookEvent stores a verified webhook and performs the first normalization pass.
func (s *Service) IngestWebhookEvent(ctx context.Context, input IngestWebhookEventInput) (entity.WebhookEvent, error) {
	if !validCommandIdentity(input.Meta) {
		return entity.WebhookEvent{}, errs.ErrInvalidArgument
	}
	providerSlug := enum.ProviderSlug(strings.TrimSpace(string(input.ProviderSlug)))
	eventName := strings.TrimSpace(input.EventName)
	deliveryID := strings.TrimSpace(input.DeliveryID)
	repositoryProviderID := strings.TrimSpace(input.RepositoryProviderID)
	if !validProviderSlug(providerSlug) || deliveryID == "" || eventName == "" || input.ReceivedAt.IsZero() {
		return entity.WebhookEvent{}, errs.ErrInvalidArgument
	}
	payload, err := canonicalJSONObject(input.PayloadJSON)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	webhook := entity.WebhookEvent{
		ID:                   s.ids.New(),
		ProviderSlug:         providerSlug,
		DeliveryID:           deliveryID,
		EventName:            eventName,
		RepositoryProviderID: repositoryProviderID,
		ReceivedAt:           input.ReceivedAt.UTC(),
		ProcessingStatus:     enum.WebhookProcessingStatusPending,
		PayloadJSON:          payload,
		RetainUntil:          input.ReceivedAt.UTC().Add(webhookPayloadRetention),
	}
	normalization, err := s.normalizeWebhook(webhook)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	webhook.ProcessingStatus = normalization.status
	webhook.LastError = normalization.lastError
	stored, _, err := s.repository.StoreWebhookEvent(ctx, webhook, normalization.providerEvents, normalization.outboxEvents)
	return stored, err
}

// GetWebhookEvent returns one stored webhook event for diagnostics.
func (s *Service) GetWebhookEvent(ctx context.Context, input GetWebhookEventInput) (entity.WebhookEvent, error) {
	if input.WebhookEventID == uuid.Nil {
		return entity.WebhookEvent{}, errs.ErrInvalidArgument
	}
	return s.repository.GetWebhookEvent(ctx, input.WebhookEventID)
}

// ListWebhookEvents returns stored webhook events by supported filters.
func (s *Service) ListWebhookEvents(ctx context.Context, input ListWebhookEventsInput) (ListWebhookEventsResult, error) {
	if input.ProviderSlug != "" && !validProviderSlug(input.ProviderSlug) {
		return ListWebhookEventsResult{}, errs.ErrInvalidArgument
	}
	if hasBlankStrings(input.EventNames) || !validWebhookStatuses(input.ProcessingStatuses) {
		return ListWebhookEventsResult{}, errs.ErrInvalidArgument
	}
	if input.ReceivedSince != nil && input.ReceivedUntil != nil && input.ReceivedUntil.Before(*input.ReceivedSince) {
		return ListWebhookEventsResult{}, errs.ErrInvalidArgument
	}
	webhooks, page, err := s.repository.ListWebhookEvents(ctx, query.WebhookEventFilter{
		ProviderSlug:         input.ProviderSlug,
		DeliveryID:           strings.TrimSpace(input.DeliveryID),
		EventNames:           trimStrings(input.EventNames),
		ProcessingStatuses:   input.ProcessingStatuses,
		RepositoryProviderID: strings.TrimSpace(input.RepositoryProviderID),
		ReceivedSince:        input.ReceivedSince,
		ReceivedUntil:        input.ReceivedUntil,
		Page:                 input.Page,
	})
	if err != nil {
		return ListWebhookEventsResult{}, err
	}
	return ListWebhookEventsResult{WebhookEvents: webhooks, Page: page}, nil
}

// RetryWebhookEventProcessing repeats normalization for a failed or pending webhook.
func (s *Service) RetryWebhookEventProcessing(ctx context.Context, input RetryWebhookEventProcessingInput) (entity.WebhookEvent, error) {
	if !validCommandIdentity(input.Meta) || input.WebhookEventID == uuid.Nil {
		return entity.WebhookEvent{}, errs.ErrInvalidArgument
	}
	webhook, err := s.repository.GetWebhookEvent(ctx, input.WebhookEventID)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	switch webhook.ProcessingStatus {
	case enum.WebhookProcessingStatusProcessed, enum.WebhookProcessingStatusIgnored:
		return webhook, nil
	case enum.WebhookProcessingStatusPending, enum.WebhookProcessingStatusFailed:
	default:
		return entity.WebhookEvent{}, errs.ErrInvalidArgument
	}
	normalization, err := s.normalizeWebhook(webhook)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	webhook.ProcessingStatus = normalization.status
	webhook.LastError = normalization.lastError
	stored, err := s.repository.ProcessWebhookEvent(ctx, webhook, normalization.providerEvents, normalization.outboxEvents[1:])
	if errors.Is(err, errs.ErrConflict) {
		return s.repository.GetWebhookEvent(ctx, input.WebhookEventID)
	}
	return stored, err
}

// GetProviderAccountRuntimeState returns runtime state by id or external account identity.
func (s *Service) GetProviderAccountRuntimeState(ctx context.Context, input GetProviderAccountRuntimeStateInput) (entity.ProviderAccountRuntimeState, error) {
	if input.ProviderAccountRuntimeStateID == nil && input.ExternalAccountID == nil {
		return entity.ProviderAccountRuntimeState{}, errs.ErrInvalidArgument
	}
	if input.ProviderAccountRuntimeStateID != nil && (input.ExternalAccountID != nil || input.ProviderSlug != "") {
		return entity.ProviderAccountRuntimeState{}, errs.ErrInvalidArgument
	}
	if input.ProviderAccountRuntimeStateID != nil && *input.ProviderAccountRuntimeStateID == uuid.Nil {
		return entity.ProviderAccountRuntimeState{}, errs.ErrInvalidArgument
	}
	if input.ExternalAccountID != nil && *input.ExternalAccountID == uuid.Nil {
		return entity.ProviderAccountRuntimeState{}, errs.ErrInvalidArgument
	}
	if input.ProviderSlug != "" && !validProviderSlug(input.ProviderSlug) {
		return entity.ProviderAccountRuntimeState{}, errs.ErrInvalidArgument
	}
	return s.repository.GetAccountRuntimeState(ctx, query.AccountRuntimeStateLookup{
		ID:                input.ProviderAccountRuntimeStateID,
		ExternalAccountID: input.ExternalAccountID,
		ProviderSlug:      input.ProviderSlug,
	})
}

// ListProviderAccountRuntimeStates returns runtime states for the supported filters.
func (s *Service) ListProviderAccountRuntimeStates(ctx context.Context, input ListProviderAccountRuntimeStatesInput) (ListProviderAccountRuntimeStatesResult, error) {
	if input.ProjectID != nil || input.OrganizationID != nil {
		return ListProviderAccountRuntimeStatesResult{}, errs.ErrInvalidArgument
	}
	if input.ProviderSlug != "" && !validProviderSlug(input.ProviderSlug) {
		return ListProviderAccountRuntimeStatesResult{}, errs.ErrInvalidArgument
	}
	if hasNilUUID(input.ExternalAccountIDs) || !validRuntimeStatuses(input.Statuses) {
		return ListProviderAccountRuntimeStatesResult{}, errs.ErrInvalidArgument
	}
	states, page, err := s.repository.ListAccountRuntimeStates(ctx, query.AccountRuntimeStateFilter{
		ProviderSlug:       input.ProviderSlug,
		ExternalAccountIDs: input.ExternalAccountIDs,
		Statuses:           input.Statuses,
		Page:               input.Page,
	})
	if err != nil {
		return ListProviderAccountRuntimeStatesResult{}, err
	}
	return ListProviderAccountRuntimeStatesResult{RuntimeStates: states, Page: page}, nil
}

// RecordProviderLimitSnapshot records a known provider limit state and updates account runtime state.
func (s *Service) RecordProviderLimitSnapshot(ctx context.Context, input RecordProviderLimitSnapshotInput) (entity.ProviderLimitSnapshot, error) {
	if !validCommandIdentity(input.Meta) {
		return entity.ProviderLimitSnapshot{}, errs.ErrInvalidArgument
	}
	if input.ExternalAccountID == uuid.Nil || !validProviderSlug(input.ProviderSlug) {
		return entity.ProviderLimitSnapshot{}, errs.ErrInvalidArgument
	}
	if strings.TrimSpace(input.LimitClass) == "" || !validLimitSource(input.Source) || input.CapturedAt.IsZero() {
		return entity.ProviderLimitSnapshot{}, errs.ErrInvalidArgument
	}
	if input.Remaining != nil && *input.Remaining < 0 {
		return entity.ProviderLimitSnapshot{}, errs.ErrInvalidArgument
	}
	if input.LimitValue != nil && *input.LimitValue < 0 {
		return entity.ProviderLimitSnapshot{}, errs.ErrInvalidArgument
	}
	now := s.clock.Now().UTC()
	snapshot := entity.ProviderLimitSnapshot{
		ID:                s.ids.New(),
		ExternalAccountID: input.ExternalAccountID,
		ProviderSlug:      input.ProviderSlug,
		LimitClass:        strings.TrimSpace(input.LimitClass),
		Remaining:         int64Ptr(input.Remaining),
		LimitValue:        int64Ptr(input.LimitValue),
		ResetAt:           utcTimePtr(input.ResetAt),
		CapturedAt:        input.CapturedAt.UTC(),
		Source:            input.Source,
	}
	checkedAt := snapshot.CapturedAt
	state := entity.ProviderAccountRuntimeState{
		Base:              entity.Base{ID: s.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ExternalAccountID: input.ExternalAccountID,
		ProviderSlug:      input.ProviderSlug,
		Status:            runtimeStatusFromLimit(input.Remaining),
		LastCheckedAt:     &checkedAt,
		LastSuccessAt:     &checkedAt,
	}
	return s.repository.RecordLimitSnapshot(ctx, snapshot, state)
}

// ListProviderLimitSnapshots returns recorded provider limit snapshots.
func (s *Service) ListProviderLimitSnapshots(ctx context.Context, input ListProviderLimitSnapshotsInput) (ListProviderLimitSnapshotsResult, error) {
	if input.ExternalAccountID != nil && *input.ExternalAccountID == uuid.Nil {
		return ListProviderLimitSnapshotsResult{}, errs.ErrInvalidArgument
	}
	if input.ProviderSlug != "" && !validProviderSlug(input.ProviderSlug) {
		return ListProviderLimitSnapshotsResult{}, errs.ErrInvalidArgument
	}
	if hasBlankStrings(input.LimitClasses) {
		return ListProviderLimitSnapshotsResult{}, errs.ErrInvalidArgument
	}
	snapshots, page, err := s.repository.ListLimitSnapshots(ctx, query.LimitSnapshotFilter{
		ExternalAccountID: input.ExternalAccountID,
		ProviderSlug:      input.ProviderSlug,
		LimitClasses:      trimStrings(input.LimitClasses),
		CapturedSince:     input.CapturedSince,
		Page:              input.Page,
	})
	if err != nil {
		return ListProviderLimitSnapshotsResult{}, err
	}
	return ListProviderLimitSnapshotsResult{LimitSnapshots: snapshots, Page: page}, nil
}

// ListProviderOperations returns provider operation audit records.
func (s *Service) ListProviderOperations(ctx context.Context, input ListProviderOperationsInput) (ListProviderOperationsResult, error) {
	if input.ProviderSlug != "" && !validProviderSlug(input.ProviderSlug) {
		return ListProviderOperationsResult{}, errs.ErrInvalidArgument
	}
	if input.ExternalAccountID != nil && *input.ExternalAccountID == uuid.Nil {
		return ListProviderOperationsResult{}, errs.ErrInvalidArgument
	}
	if !validOperationTypes(input.OperationTypes) || !validOperationStatuses(input.Statuses) {
		return ListProviderOperationsResult{}, errs.ErrInvalidArgument
	}
	operations, page, err := s.repository.ListProviderOperations(ctx, query.ProviderOperationFilter{
		ProviderSlug:      input.ProviderSlug,
		ExternalAccountID: input.ExternalAccountID,
		OperationTypes:    input.OperationTypes,
		Statuses:          input.Statuses,
		TargetRef:         strings.TrimSpace(input.TargetRef),
		StartedSince:      input.StartedSince,
		Page:              input.Page,
	})
	if err != nil {
		return ListProviderOperationsResult{}, err
	}
	return ListProviderOperationsResult{ProviderOperations: operations, Page: page}, nil
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

type uuidGenerator struct{}

func (uuidGenerator) New() uuid.UUID {
	return uuid.New()
}

func int64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	converted := value.UTC()
	return &converted
}
