package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
)

const (
	syncCursorLeaseTTL          = 30 * time.Second
	maxReconciliationBatchItems = int32(500)
)

// Service is the domain entrypoint for provider-native work item workflows.
type Service struct {
	repository             providerrepo.Repository
	clock                  providerrepo.Clock
	ids                    providerrepo.IDGenerator
	accountUsage           AccountUsageResolver
	secretResolver         secretresolver.Resolver
	providerAdapters       map[enum.ProviderSlug]providerclient.Adapter
	providerWriteExecutors map[enum.ProviderSlug]providerclient.WriteExecutor
	webhookNormalizers     map[enum.ProviderSlug]providerrepo.WebhookNormalizer
}

// New creates a provider-hub domain service.
func New(repository providerrepo.Repository, normalizers ...providerrepo.WebhookNormalizer) *Service {
	return NewWithRuntime(repository, systemClock{}, uuidGenerator{}, normalizers...)
}

// NewWithRuntime creates a provider-hub domain service with deterministic runtime dependencies.
func NewWithRuntime(repository providerrepo.Repository, clock providerrepo.Clock, ids providerrepo.IDGenerator, normalizers ...providerrepo.WebhookNormalizer) *Service {
	return NewWithDependencies(Dependencies{
		Repository:         repository,
		Clock:              clock,
		IDGenerator:        ids,
		WebhookNormalizers: normalizers,
	})
}

// NewWithDependencies creates a provider-hub domain service with explicit collaborators.
func NewWithDependencies(deps Dependencies) *Service {
	if deps.Repository == nil {
		panic("provider-hub repository is required")
	}
	if deps.Clock == nil {
		deps.Clock = systemClock{}
	}
	if deps.IDGenerator == nil {
		deps.IDGenerator = uuidGenerator{}
	}
	return &Service{
		repository:             deps.Repository,
		clock:                  deps.Clock,
		ids:                    deps.IDGenerator,
		accountUsage:           deps.AccountUsageResolver,
		secretResolver:         deps.SecretResolver,
		providerAdapters:       providerAdapterRegistry(deps.ProviderAdapters),
		providerWriteExecutors: providerWriteExecutorRegistry(deps.ProviderWriteExecutors),
		webhookNormalizers:     webhookNormalizerRegistry(deps.WebhookNormalizers),
	}
}

func webhookNormalizerRegistry(normalizers []providerrepo.WebhookNormalizer) map[enum.ProviderSlug]providerrepo.WebhookNormalizer {
	return providerSlugRegistry(
		normalizers,
		"provider-hub webhook normalizer is required",
		"provider-hub webhook normalizer has invalid provider slug",
	)
}

func providerAdapterRegistry(adapters []providerclient.Adapter) map[enum.ProviderSlug]providerclient.Adapter {
	return providerSlugRegistry(
		adapters,
		"provider-hub adapter is required",
		"provider-hub adapter has invalid provider slug",
	)
}

func providerWriteExecutorRegistry(executors []providerclient.WriteExecutor) map[enum.ProviderSlug]providerclient.WriteExecutor {
	return providerSlugRegistry(
		executors,
		"provider-hub write executor is required",
		"provider-hub write executor has invalid provider slug",
	)
}

type providerSlugger interface {
	ProviderSlug() enum.ProviderSlug
}

func providerSlugRegistry[T providerSlugger](items []T, nilMessage string, invalidMessage string) map[enum.ProviderSlug]T {
	registry := make(map[enum.ProviderSlug]T, len(items))
	for _, item := range items {
		if any(item) == nil {
			panic(nilMessage)
		}
		providerSlug := item.ProviderSlug()
		if !validProviderSlug(providerSlug) {
			panic(invalidMessage)
		}
		registry[providerSlug] = item
	}
	return registry
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
		PayloadDigest:        webhookPayloadDigest(payload),
		RetainUntil:          input.ReceivedAt.UTC().Add(webhookPayloadRetention),
	}
	normalization, err := s.normalizeWebhook(ctx, webhook)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	webhook.ProcessingStatus = normalization.status
	webhook.LastError = normalization.lastError
	webhook, err = webhookForInboxStorage(webhook)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	stored, _, err := s.repository.StoreWebhookEvent(ctx, webhook, normalization.projectionUpdate, normalization.providerEvents, normalization.outboxEvents)
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
	normalization, err := s.normalizeWebhook(ctx, webhook)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	webhook.ProcessingStatus = normalization.status
	webhook.LastError = normalization.lastError
	webhook, err = webhookForInboxStorage(webhook)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	stored, err := s.repository.ProcessWebhookEvent(ctx, webhook, normalization.projectionUpdate, normalization.providerEvents, normalization.outboxEvents[1:])
	if errors.Is(err, errs.ErrNotFound) {
		return s.currentWebhookAfterConcurrentProcessing(ctx, input.WebhookEventID)
	}
	return stored, err
}

// GetWorkItemProjection returns one Issue or PR/MR projection by internal id.
func (s *Service) GetWorkItemProjection(ctx context.Context, input GetWorkItemProjectionInput) (entity.ProviderWorkItemProjection, error) {
	if input.WorkItemProjectionID == uuid.Nil {
		return entity.ProviderWorkItemProjection{}, errs.ErrInvalidArgument
	}
	id := input.WorkItemProjectionID
	return s.repository.GetWorkItemProjection(ctx, query.ProviderTargetLookup{ID: &id})
}

// FindWorkItemByProviderRef finds one projection by provider-native reference.
func (s *Service) FindWorkItemByProviderRef(ctx context.Context, input FindWorkItemByProviderRefInput) (entity.ProviderWorkItemProjection, error) {
	if !validProviderSlug(input.ProviderSlug) {
		return entity.ProviderWorkItemProjection{}, errs.ErrInvalidArgument
	}
	hasProviderID := strings.TrimSpace(input.ProviderObjectID) != ""
	hasURL := strings.TrimSpace(input.WebURL) != ""
	hasNumberRef := strings.TrimSpace(input.RepositoryFullName) != "" && validWorkItemKind(input.Kind) && input.Number > 0
	if !hasProviderID && !hasURL && !hasNumberRef {
		return entity.ProviderWorkItemProjection{}, errs.ErrInvalidArgument
	}
	return s.repository.GetWorkItemProjection(ctx, query.ProviderTargetLookup{
		ProviderSlug:       input.ProviderSlug,
		RepositoryFullName: strings.TrimSpace(input.RepositoryFullName),
		Kind:               input.Kind,
		Number:             input.Number,
		ProviderObjectID:   strings.TrimSpace(input.ProviderObjectID),
		WebURL:             strings.TrimSpace(input.WebURL),
	})
}

// ListWorkItemProjections returns Issue and PR/MR projections by supported filters.
func (s *Service) ListWorkItemProjections(ctx context.Context, input ListWorkItemProjectionsInput) (ListWorkItemProjectionsResult, error) {
	if input.ProjectID != nil && *input.ProjectID == uuid.Nil {
		return ListWorkItemProjectionsResult{}, errs.ErrInvalidArgument
	}
	if input.RepositoryID != nil && *input.RepositoryID == uuid.Nil {
		return ListWorkItemProjectionsResult{}, errs.ErrInvalidArgument
	}
	if input.ProviderSlug != "" && !validProviderSlug(input.ProviderSlug) {
		return ListWorkItemProjectionsResult{}, errs.ErrInvalidArgument
	}
	if !validWorkItemKinds(input.Kinds) || !validWorkItemDriftStatuses(input.DriftStatuses) {
		return ListWorkItemProjectionsResult{}, errs.ErrInvalidArgument
	}
	if hasBlankStrings(input.States) || hasBlankStrings(input.Labels) || hasBlankStrings(input.WorkItemTypes) {
		return ListWorkItemProjectionsResult{}, errs.ErrInvalidArgument
	}
	projections, page, err := s.repository.ListWorkItemProjections(ctx, query.WorkItemProjectionFilter{
		ProjectID:          input.ProjectID,
		RepositoryID:       input.RepositoryID,
		ProviderSlug:       input.ProviderSlug,
		RepositoryFullName: strings.TrimSpace(input.RepositoryFullName),
		Kinds:              input.Kinds,
		States:             trimStrings(input.States),
		Labels:             trimStrings(input.Labels),
		WorkItemTypes:      trimStrings(input.WorkItemTypes),
		DriftStatuses:      input.DriftStatuses,
		UpdatedSince:       input.UpdatedSince,
		Page:               input.Page,
	})
	if err != nil {
		return ListWorkItemProjectionsResult{}, err
	}
	return ListWorkItemProjectionsResult{WorkItemProjections: projections, Page: page}, nil
}

// ListComments returns normalized comments and review signals for a work item projection.
func (s *Service) ListComments(ctx context.Context, input ListCommentsInput) (ListCommentsResult, error) {
	if input.WorkItemProjectionID == uuid.Nil || !validCommentKinds(input.Kinds) {
		return ListCommentsResult{}, errs.ErrInvalidArgument
	}
	comments, page, err := s.repository.ListComments(ctx, query.CommentProjectionFilter{
		WorkItemProjectionID: input.WorkItemProjectionID,
		Kinds:                input.Kinds,
		Page:                 input.Page,
	})
	if err != nil {
		return ListCommentsResult{}, err
	}
	return ListCommentsResult{Comments: comments, Page: page}, nil
}

// ListRelationships returns normalized provider relationships by supported filters.
func (s *Service) ListRelationships(ctx context.Context, input ListRelationshipsInput) (ListRelationshipsResult, error) {
	if input.WorkItemProjectionID != nil && *input.WorkItemProjectionID == uuid.Nil {
		return ListRelationshipsResult{}, errs.ErrInvalidArgument
	}
	if hasBlankStrings(input.RelationshipTypes) || !validRelationshipSources(input.Sources) || !validRelationshipConfidenceLevels(input.ConfidenceLevels) {
		return ListRelationshipsResult{}, errs.ErrInvalidArgument
	}
	relationships, page, err := s.repository.ListRelationships(ctx, query.RelationshipFilter{
		WorkItemProjectionID: input.WorkItemProjectionID,
		RelationshipTypes:    trimStrings(input.RelationshipTypes),
		Sources:              input.Sources,
		ConfidenceLevels:     input.ConfidenceLevels,
		Page:                 input.Page,
	})
	if err != nil {
		return ListRelationshipsResult{}, err
	}
	return ListRelationshipsResult{Relationships: relationships, Page: page}, nil
}

// GetRepositoryMergeSignal returns one safe provider-owned merge signal by stable identity.
func (s *Service) GetRepositoryMergeSignal(ctx context.Context, input GetRepositoryMergeSignalInput) (RepositoryMergeSignalResult, error) {
	if input.SignalID != nil && *input.SignalID == uuid.Nil {
		return RepositoryMergeSignalResult{}, errs.ErrInvalidArgument
	}
	signalKey := strings.TrimSpace(input.SignalKey)
	if input.SignalID == nil && signalKey == "" {
		return RepositoryMergeSignalResult{}, errs.ErrInvalidArgument
	}
	signal, err := s.repository.GetRepositoryMergeSignal(ctx, query.RepositoryMergeSignalLookup{
		ID:        input.SignalID,
		SignalKey: signalKey,
	})
	if errors.Is(err, errs.ErrNotFound) {
		return RepositoryMergeSignalResult{Status: enum.ProviderOwnedDataStatusNotFound}, nil
	}
	if err != nil {
		return RepositoryMergeSignalResult{}, err
	}
	return RepositoryMergeSignalResult{Status: enum.ProviderOwnedDataStatusReady, MergeSignal: &signal}, nil
}

// ListRepositoryMergeSignals returns safe provider-owned merge signals by repository/project context.
func (s *Service) ListRepositoryMergeSignals(ctx context.Context, input ListRepositoryMergeSignalsInput) (ListRepositoryMergeSignalsResult, error) {
	if input.ProjectID != nil && *input.ProjectID == uuid.Nil {
		return ListRepositoryMergeSignalsResult{}, errs.ErrInvalidArgument
	}
	if input.RepositoryID != nil && *input.RepositoryID == uuid.Nil {
		return ListRepositoryMergeSignalsResult{}, errs.ErrInvalidArgument
	}
	if input.ProviderSlug != "" && !validProviderSlug(input.ProviderSlug) {
		return ListRepositoryMergeSignalsResult{}, errs.ErrInvalidArgument
	}
	if !validRepositoryMergeSignalKinds(input.Kinds) || !validRepositoryMergeSignalStatuses(input.Statuses) {
		return ListRepositoryMergeSignalsResult{}, errs.ErrInvalidArgument
	}
	if input.PullRequestNumber != nil && *input.PullRequestNumber <= 0 {
		return ListRepositoryMergeSignalsResult{}, errs.ErrInvalidArgument
	}
	signals, page, err := s.repository.ListRepositoryMergeSignals(ctx, query.RepositoryMergeSignalFilter{
		ProjectID:            input.ProjectID,
		RepositoryID:         input.RepositoryID,
		ProviderSlug:         input.ProviderSlug,
		RepositoryFullName:   strings.TrimSpace(input.RepositoryFullName),
		ProviderRepositoryID: strings.TrimSpace(input.ProviderRepositoryID),
		Kinds:                input.Kinds,
		Statuses:             input.Statuses,
		PullRequestNumber:    input.PullRequestNumber,
		MergedSince:          input.MergedSince,
		Page:                 input.Page,
	})
	if err != nil {
		return ListRepositoryMergeSignalsResult{}, err
	}
	return ListRepositoryMergeSignalsResult{MergeSignals: signals, Page: page}, nil
}

// GetRepositoryAdoptionScanSnapshot returns one safe provider-owned adoption scan snapshot.
func (s *Service) GetRepositoryAdoptionScanSnapshot(ctx context.Context, input GetRepositoryAdoptionScanSnapshotInput) (RepositoryAdoptionScanSnapshotResult, error) {
	if input.SnapshotID != nil && *input.SnapshotID == uuid.Nil {
		return RepositoryAdoptionScanSnapshotResult{}, errs.ErrInvalidArgument
	}
	if input.ProviderOperationID != nil && *input.ProviderOperationID == uuid.Nil {
		return RepositoryAdoptionScanSnapshotResult{}, errs.ErrInvalidArgument
	}
	snapshotKey := strings.TrimSpace(input.SnapshotKey)
	if input.SnapshotID == nil && input.ProviderOperationID == nil && snapshotKey == "" {
		return RepositoryAdoptionScanSnapshotResult{}, errs.ErrInvalidArgument
	}
	snapshot, err := s.repository.GetRepositoryAdoptionScan(ctx, query.RepositoryAdoptionScanLookup{
		ID:                  input.SnapshotID,
		SnapshotKey:         snapshotKey,
		ProviderOperationID: input.ProviderOperationID,
	})
	if errors.Is(err, errs.ErrNotFound) {
		return RepositoryAdoptionScanSnapshotResult{Status: enum.ProviderOwnedDataStatusNotFound}, nil
	}
	if err != nil {
		return RepositoryAdoptionScanSnapshotResult{}, err
	}
	return RepositoryAdoptionScanSnapshotResult{Status: enum.ProviderOwnedDataStatusReady, Snapshot: &snapshot}, nil
}

// ListRepositoryAdoptionScanSnapshots returns safe provider-owned adoption scan snapshots by repository/project context.
func (s *Service) ListRepositoryAdoptionScanSnapshots(ctx context.Context, input ListRepositoryAdoptionScanSnapshotsInput) (ListRepositoryAdoptionScanSnapshotsResult, error) {
	if input.ProjectID != nil && *input.ProjectID == uuid.Nil {
		return ListRepositoryAdoptionScanSnapshotsResult{}, errs.ErrInvalidArgument
	}
	if input.RepositoryID != nil && *input.RepositoryID == uuid.Nil {
		return ListRepositoryAdoptionScanSnapshotsResult{}, errs.ErrInvalidArgument
	}
	if input.ExternalAccountID != nil && *input.ExternalAccountID == uuid.Nil {
		return ListRepositoryAdoptionScanSnapshotsResult{}, errs.ErrInvalidArgument
	}
	if input.ProviderSlug != "" && !validProviderSlug(input.ProviderSlug) {
		return ListRepositoryAdoptionScanSnapshotsResult{}, errs.ErrInvalidArgument
	}
	if !validRepositoryAdoptionScanStatuses(input.Statuses) {
		return ListRepositoryAdoptionScanSnapshotsResult{}, errs.ErrInvalidArgument
	}
	snapshots, page, err := s.repository.ListRepositoryAdoptionScans(ctx, query.RepositoryAdoptionScanFilter{
		ProjectID:            input.ProjectID,
		RepositoryID:         input.RepositoryID,
		ExternalAccountID:    input.ExternalAccountID,
		ProviderSlug:         input.ProviderSlug,
		RepositoryFullName:   strings.TrimSpace(input.RepositoryFullName),
		ProviderRepositoryID: strings.TrimSpace(input.ProviderRepositoryID),
		Statuses:             input.Statuses,
		ObservedSince:        input.ObservedSince,
		Page:                 input.Page,
	})
	if err != nil {
		return ListRepositoryAdoptionScanSnapshotsResult{}, err
	}
	return ListRepositoryAdoptionScanSnapshotsResult{Snapshots: snapshots, Page: page}, nil
}

// EnqueueReconciliation creates or updates sync cursors for one reconciliation scope.
func (s *Service) EnqueueReconciliation(ctx context.Context, input EnqueueReconciliationInput) (EnqueueReconciliationResult, error) {
	idempotencyKey := strings.TrimSpace(input.Meta.IdempotencyKey)
	if !validCommandIdentity(input.Meta) || idempotencyKey == "" {
		return EnqueueReconciliationResult{}, errs.ErrInvalidArgument
	}
	providerSlug := enum.ProviderSlug(strings.TrimSpace(string(input.ProviderSlug)))
	scopeRef := strings.TrimSpace(input.ScopeRef)
	artifactKinds := uniqueSyncArtifactKinds(input.ArtifactKinds)
	sort.Slice(artifactKinds, func(i, j int) bool {
		return artifactKinds[i] < artifactKinds[j]
	})
	if !validProviderSlug(providerSlug) ||
		input.ExternalAccountID == uuid.Nil ||
		!validSyncCursorScope(input.ScopeType) ||
		scopeRef == "" ||
		len(artifactKinds) == 0 ||
		!validSyncArtifactKinds(artifactKinds) ||
		!validSyncCursorPriority(input.Priority) {
		return EnqueueReconciliationResult{}, errs.ErrInvalidArgument
	}
	now := s.clock.Now().UTC()
	request := entity.ReconciliationRequest{
		ID:                s.ids.New(),
		ProviderSlug:      providerSlug,
		ExternalAccountID: input.ExternalAccountID,
		ScopeType:         input.ScopeType,
		ScopeRef:          scopeRef,
		IdempotencyKey:    idempotencyKey,
		ArtifactKinds:     artifactKinds,
		Priority:          input.Priority,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	cursors := s.buildSyncCursors(providerSlug, input.ExternalAccountID, input.ScopeType, scopeRef, artifactKinds, input.Priority, now)
	stored, err := s.repository.EnqueueSyncCursors(ctx, request, cursors)
	if err != nil {
		return EnqueueReconciliationResult{}, err
	}
	return EnqueueReconciliationResult{SyncCursors: stored}, nil
}

// RunReconciliationBatch leases one cursor and performs read-only provider reconciliation.
func (s *Service) RunReconciliationBatch(ctx context.Context, input RunReconciliationBatchInput) (RunReconciliationBatchResult, error) {
	if !validCommandIdentity(input.Meta) ||
		strings.TrimSpace(input.LeaseOwner) == "" ||
		input.MaxItems <= 0 ||
		input.MaxItems > maxReconciliationBatchItems {
		return RunReconciliationBatchResult{}, errs.ErrInvalidArgument
	}
	if input.SyncCursorID != nil && *input.SyncCursorID == uuid.Nil {
		return RunReconciliationBatchResult{}, errs.ErrInvalidArgument
	}
	if input.ExternalAccountID != nil && *input.ExternalAccountID == uuid.Nil {
		return RunReconciliationBatchResult{}, errs.ErrInvalidArgument
	}
	providerSlug := enum.ProviderSlug(strings.TrimSpace(string(input.ProviderSlug)))
	if providerSlug != "" && !validProviderSlug(providerSlug) {
		return RunReconciliationBatchResult{}, errs.ErrInvalidArgument
	}
	now := s.clock.Now().UTC()
	cursor, err := s.repository.ClaimSyncCursor(ctx, providerrepo.SyncCursorClaim{
		ID:                input.SyncCursorID,
		ProviderSlug:      providerSlug,
		ExternalAccountID: input.ExternalAccountID,
		LeaseOwner:        strings.TrimSpace(input.LeaseOwner),
		Now:               now,
		LeaseUntil:        now.Add(syncCursorLeaseTTL),
	})
	if err != nil {
		return RunReconciliationBatchResult{}, err
	}
	return s.runClaimedReconciliationBatch(ctx, cursor, input)
}

// GetSyncCursor returns one reconciliation cursor.
func (s *Service) GetSyncCursor(ctx context.Context, input GetSyncCursorInput) (entity.SyncCursor, error) {
	id := input.SyncCursorID
	if id == uuid.Nil {
		return entity.SyncCursor{}, errs.ErrInvalidArgument
	}
	return s.repository.GetSyncCursor(ctx, id)
}

// ListSyncCursors returns reconciliation cursors by supported filters.
func (s *Service) ListSyncCursors(ctx context.Context, input ListSyncCursorsInput) (ListSyncCursorsResult, error) {
	providerSlug := enum.ProviderSlug(strings.TrimSpace(string(input.ProviderSlug)))
	if providerSlug != "" && !validProviderSlug(providerSlug) {
		return ListSyncCursorsResult{}, errs.ErrInvalidArgument
	}
	if input.ExternalAccountID != nil && *input.ExternalAccountID == uuid.Nil {
		return ListSyncCursorsResult{}, errs.ErrInvalidArgument
	}
	if input.ScopeType != "" && !validSyncCursorScope(input.ScopeType) {
		return ListSyncCursorsResult{}, errs.ErrInvalidArgument
	}
	if strings.TrimSpace(input.ScopeRef) == "" && input.ScopeRef != "" {
		return ListSyncCursorsResult{}, errs.ErrInvalidArgument
	}
	if !validSyncArtifactKinds(input.ArtifactKinds) || !validSyncCursorPriorities(input.Priorities) {
		return ListSyncCursorsResult{}, errs.ErrInvalidArgument
	}
	cursors, page, err := s.repository.ListSyncCursors(ctx, query.SyncCursorFilter{
		ProviderSlug:      providerSlug,
		ExternalAccountID: input.ExternalAccountID,
		ScopeType:         input.ScopeType,
		ScopeRef:          strings.TrimSpace(input.ScopeRef),
		ArtifactKinds:     input.ArtifactKinds,
		Priorities:        input.Priorities,
		IncludeHealthy:    input.IncludeHealthy,
		Page:              input.Page,
	})
	if err != nil {
		return ListSyncCursorsResult{}, err
	}
	return ListSyncCursorsResult{SyncCursors: cursors, Page: page}, nil
}

func (s *Service) currentWebhookAfterConcurrentProcessing(ctx context.Context, webhookEventID uuid.UUID) (entity.WebhookEvent, error) {
	current, err := s.repository.GetWebhookEvent(ctx, webhookEventID)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	switch current.ProcessingStatus {
	case enum.WebhookProcessingStatusFailed,
		enum.WebhookProcessingStatusProcessed,
		enum.WebhookProcessingStatusIgnored:
		return current, nil
	default:
		return entity.WebhookEvent{}, errs.ErrConflict
	}
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
