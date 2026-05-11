package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
)

const (
	reconciliationErrorAccessDenied        = "access_denied"
	reconciliationErrorSecretUnavailable   = "secret_unavailable"
	reconciliationErrorProviderAuthFailed  = "provider_auth_failed"
	reconciliationErrorProviderNotFound    = "provider_not_found"
	reconciliationErrorProviderRateLimited = "provider_rate_limited"
	reconciliationErrorProviderTransient   = "provider_transient_error"
	reconciliationErrorProviderUnsupported = "provider_unsupported"
	reconciliationErrorProviderPermanent   = "provider_permanent_error"
)

func (s *Service) runClaimedReconciliationBatch(ctx context.Context, cursor entity.SyncCursor, input RunReconciliationBatchInput) (RunReconciliationBatchResult, error) {
	if s.accountUsage == nil || s.secretResolver == nil {
		stored, err := s.completeReconciliationFailure(ctx, cursor, input.LeaseOwner, reconciliationErrorAccessDenied, 0)
		return RunReconciliationBatchResult{SyncCursor: stored}, joinErrors(errs.ErrDependencyUnavailable, err)
	}
	adapter := s.providerAdapters[cursor.ProviderSlug]
	if adapter == nil {
		stored, err := s.completeReconciliationFailure(ctx, cursor, input.LeaseOwner, reconciliationErrorProviderUnsupported, 0)
		return RunReconciliationBatchResult{SyncCursor: stored}, joinErrors(errs.ErrPreconditionFailed, err)
	}
	usageScope, err := reconciliationUsageScope(cursor)
	if err != nil {
		stored, completeErr := s.completeReconciliationFailure(ctx, cursor, input.LeaseOwner, reconciliationErrorProviderUnsupported, 0)
		return RunReconciliationBatchResult{SyncCursor: stored}, joinErrors(err, completeErr)
	}
	usage, err := s.accountUsage.ResolveExternalAccountUsage(ctx, ExternalAccountUsageInput{
		ExternalAccountID: cursor.ExternalAccountID,
		ActionKey:         accesscatalog.ActionProviderReconciliationRun,
		ScopeType:         usageScope.Type,
		ScopeID:           usageScope.ID,
	})
	if err != nil {
		stored, completeErr := s.completeReconciliationFailure(ctx, cursor, input.LeaseOwner, reconciliationErrorAccessDenied, 0)
		return RunReconciliationBatchResult{SyncCursor: stored}, joinErrors(err, completeErr)
	}
	if enum.ProviderSlug(strings.TrimSpace(string(usage.ProviderSlug))) != cursor.ProviderSlug {
		stored, completeErr := s.completeReconciliationFailure(ctx, cursor, input.LeaseOwner, reconciliationErrorAccessDenied, 0)
		return RunReconciliationBatchResult{SyncCursor: stored}, joinErrors(errs.ErrPreconditionFailed, completeErr)
	}
	secret, err := s.secretResolver.Resolve(ctx, secretresolver.SecretRef{StoreType: usage.SecretStoreType, StoreRef: usage.SecretStoreRef})
	if err != nil {
		stored, completeErr := s.completeReconciliationFailure(ctx, cursor, input.LeaseOwner, reconciliationErrorSecretUnavailable, 0)
		return RunReconciliationBatchResult{SyncCursor: stored}, joinErrors(mapSecretResolverError(err), completeErr)
	}
	defer secret.Clear()

	now := s.clock.Now().UTC()
	providerResult, err := adapter.Reconcile(ctx, providerclient.ReconciliationRequest{
		Credential: providerclient.AccountCredential{
			ExternalAccountID: cursor.ExternalAccountID,
			ProviderSlug:      cursor.ProviderSlug,
			Token:             secret,
		},
		Cursor:     cursor,
		MaxItems:   input.MaxItems,
		ObservedAt: now,
	})
	if err != nil {
		return s.completeProviderError(ctx, cursor, input.LeaseOwner, err)
	}
	return s.completeReconciliationSuccess(ctx, cursor, input.LeaseOwner, providerResult)
}

func (s *Service) completeReconciliationSuccess(ctx context.Context, cursor entity.SyncCursor, leaseOwner string, providerResult providerclient.ReconciliationResult) (RunReconciliationBatchResult, error) {
	now := s.clock.Now().UTC()
	projectionUpdate, providerEvents, outboxEvents, err := s.reconciliationProjection(providerResult, cursor, now)
	if err != nil {
		return RunReconciliationBatchResult{}, err
	}
	cursor.CursorValue = nextCursorValue(cursor.CursorValue, providerResult.NextCursorValue, now)
	cursor.OverlapSince = overlapSinceFromCursorValue(cursor.CursorValue)
	cursor.LastSuccessAt = &now
	cursor.LastCheckedAt = &now
	cursor.LastError = ""
	cursor.RateBudgetStateJSON = jsonPayloadOrDefault(providerResult.RateBudgetStateJSON, "{}")
	cursor.LeaseOwner = ""
	cursor.LeaseUntil = nil
	runtimeState := s.runtimeStateFromReconciliation(cursor, now, providerResult, "")
	stored, storedProviderEvents, err := s.repository.ApplyReconciliationBatch(ctx, providerrepo.ReconciliationBatchCompletion{
		Cursor:             cursor,
		ExpectedLeaseOwner: strings.TrimSpace(leaseOwner),
		ProjectionUpdate:   projectionUpdate,
		ProviderEvents:     providerEvents,
		OutboxEvents:       outboxEvents,
		LimitSnapshots:     providerResult.LimitSnapshots,
		RuntimeState:       &runtimeState,
		Now:                now,
	})
	if err != nil {
		return RunReconciliationBatchResult{}, err
	}
	return RunReconciliationBatchResult{
		SyncCursor:      stored,
		ItemsProcessed:  int64(len(providerResult.WorkItems) + len(providerResult.Comments)),
		EventsPublished: int64(len(storedProviderEvents) + 1),
	}, nil
}

func (s *Service) completeProviderError(ctx context.Context, cursor entity.SyncCursor, leaseOwner string, err error) (RunReconciliationBatchResult, error) {
	var providerErr *providerclient.Error
	if !errors.As(err, &providerErr) {
		stored, completeErr := s.completeReconciliationFailure(ctx, cursor, leaseOwner, reconciliationErrorProviderTransient, 0)
		return RunReconciliationBatchResult{SyncCursor: stored}, joinErrors(errs.ErrDependencyUnavailable, completeErr)
	}
	code, domainErr := reconciliationProviderError(providerErr)
	stored, completeErr := s.completeReconciliationFailure(ctx, cursor, leaseOwner, code, providerErr.RetryAfter)
	result := RunReconciliationBatchResult{SyncCursor: stored}
	if providerErr.RetryAfter > 0 {
		result.RetryAfter = providerErr.RetryAfter.String()
	}
	if providerErr.Kind == providerclient.ErrorKindRateLimited {
		return result, completeErr
	}
	return result, joinErrors(domainErr, completeErr)
}

func (s *Service) completeReconciliationFailure(ctx context.Context, cursor entity.SyncCursor, leaseOwner string, lastError string, retryAfter time.Duration) (entity.SyncCursor, error) {
	now := s.clock.Now().UTC()
	cursor.LastCheckedAt = &now
	cursor.LastError = lastError
	if len(cursor.RateBudgetStateJSON) == 0 {
		cursor.RateBudgetStateJSON = []byte(`{}`)
	}
	if retryAfter > 0 {
		retryAt := now.Add(retryAfter)
		cursor.LeaseOwner = strings.TrimSpace(leaseOwner)
		cursor.LeaseUntil = &retryAt
	} else {
		cursor.LeaseOwner = ""
		cursor.LeaseUntil = nil
	}
	runtimeState := s.runtimeStateFromReconciliation(cursor, now, providerclient.ReconciliationResult{}, lastError)
	stored, _, err := s.repository.ApplyReconciliationBatch(ctx, providerrepo.ReconciliationBatchCompletion{
		Cursor:             cursor,
		ExpectedLeaseOwner: strings.TrimSpace(leaseOwner),
		RuntimeState:       &runtimeState,
		Now:                now,
	})
	return stored, err
}

func (s *Service) reconciliationProjection(providerResult providerclient.ReconciliationResult, cursor entity.SyncCursor, now time.Time) (providerrepo.ProjectionUpdate, []entity.ProviderEvent, []entity.OutboxEvent, error) {
	var update providerrepo.ProjectionUpdate
	var providerEvents []entity.ProviderEvent
	var outboxEvents []entity.OutboxEvent
	for _, snapshot := range providerResult.WorkItems {
		workItem, relationships, err := workItemProjectionFromSnapshot(snapshot, now)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, nil, err
		}
		update.WorkItem = &workItem
		update.Relationships = append(update.Relationships, relationships...)
		providerEvent, eventOutbox, err := s.reconciliationWorkItemEvents(cursor, workItem)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, nil, err
		}
		providerEvents = append(providerEvents, providerEvent)
		outboxEvents = append(outboxEvents, eventOutbox)
	}
	for _, snapshot := range providerResult.Comments {
		comment := commentProjectionFromSnapshot(snapshot, now)
		update.Comments = append(update.Comments, comment)
		providerEvent, eventOutbox, err := s.reconciliationCommentEvents(cursor, comment, snapshot)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, nil, err
		}
		providerEvents = append(providerEvents, providerEvent)
		outboxEvents = append(outboxEvents, eventOutbox)
	}
	for _, relationship := range update.Relationships {
		outboxEvent, err := s.reconciliationRelationshipOutbox(cursor, relationship)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, nil, err
		}
		outboxEvents = append(outboxEvents, outboxEvent)
	}
	cursorOutbox, err := s.syncCursorAdvancedOutbox(cursor)
	if err != nil {
		return providerrepo.ProjectionUpdate{}, nil, nil, err
	}
	outboxEvents = append(outboxEvents, cursorOutbox)
	return update, providerEvents, outboxEvents, nil
}

func (s *Service) reconciliationWorkItemEvents(cursor entity.SyncCursor, workItem entity.ProviderWorkItemProjection) (entity.ProviderEvent, entity.OutboxEvent, error) {
	providerEventID := s.ids.New()
	providerPayload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(cursor.ProviderSlug),
		ProviderEventID:      providerEventID.String(),
		ExternalAccountID:    cursor.ExternalAccountID.String(),
		RepositoryFullName:   workItem.RepositoryFullName,
		ProviderWorkItemID:   workItem.ProviderWorkItemID,
		WorkItemProjectionID: workItem.ID.String(),
		Kind:                 string(workItem.Kind),
		Number:               workItem.Number,
		WatermarkStatus:      string(workItem.WatermarkStatus),
		DriftStatus:          string(workItem.DriftStatus),
		SyncCursorID:         cursor.ID.String(),
	})
	if err != nil {
		return entity.ProviderEvent{}, entity.OutboxEvent{}, err
	}
	providerEvent := entity.ProviderEvent{
		ID:            providerEventID,
		EventType:     providerEventWorkItemSynced,
		AggregateType: providerAggregateWorkItem,
		AggregateID:   workItem.ProviderWorkItemID,
		PayloadJSON:   providerPayload,
		OccurredAt:    workItem.SyncedAt,
	}
	outboxPayload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(cursor.ProviderSlug),
		ExternalAccountID:    cursor.ExternalAccountID.String(),
		RepositoryFullName:   workItem.RepositoryFullName,
		ProviderWorkItemID:   workItem.ProviderWorkItemID,
		WorkItemProjectionID: workItem.ID.String(),
		Kind:                 string(workItem.Kind),
		Number:               workItem.Number,
		WatermarkStatus:      string(workItem.WatermarkStatus),
		DriftStatus:          string(workItem.DriftStatus),
		SyncCursorID:         cursor.ID.String(),
	})
	if err != nil {
		return entity.ProviderEvent{}, entity.OutboxEvent{}, err
	}
	return providerEvent, outboxEventRecord(s.ids.New(), providerEventWorkItemSynced, providerAggregateWorkItem, workItem.ID, outboxPayload, workItem.SyncedAt), nil
}

func (s *Service) reconciliationCommentEvents(cursor entity.SyncCursor, comment entity.ProviderCommentProjection, snapshot value.ProviderCommentSnapshot) (entity.ProviderEvent, entity.OutboxEvent, error) {
	providerEventID := s.ids.New()
	providerPayload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:        string(cursor.ProviderSlug),
		ProviderEventID:     providerEventID.String(),
		ExternalAccountID:   cursor.ExternalAccountID.String(),
		ProviderWorkItemID:  snapshot.ProviderWorkItemID,
		ProviderCommentID:   comment.ProviderCommentID,
		CommentProjectionID: comment.ID.String(),
		Kind:                string(comment.Kind),
		ReviewState:         string(comment.ReviewState),
		SyncCursorID:        cursor.ID.String(),
	})
	if err != nil {
		return entity.ProviderEvent{}, entity.OutboxEvent{}, err
	}
	providerEvent := entity.ProviderEvent{
		ID:            providerEventID,
		EventType:     providerEventCommentSynced,
		AggregateType: providerAggregateComment,
		AggregateID:   comment.ProviderCommentID,
		PayloadJSON:   providerPayload,
		OccurredAt:    comment.UpdatedAt,
	}
	if comment.ProviderUpdatedAt != nil {
		providerEvent.OccurredAt = *comment.ProviderUpdatedAt
	}
	outboxPayload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:        string(cursor.ProviderSlug),
		ExternalAccountID:   cursor.ExternalAccountID.String(),
		ProviderWorkItemID:  snapshot.ProviderWorkItemID,
		ProviderCommentID:   comment.ProviderCommentID,
		CommentProjectionID: comment.ID.String(),
		Kind:                string(comment.Kind),
		ReviewState:         string(comment.ReviewState),
		SyncCursorID:        cursor.ID.String(),
	})
	if err != nil {
		return entity.ProviderEvent{}, entity.OutboxEvent{}, err
	}
	return providerEvent, outboxEventRecord(s.ids.New(), providerEventCommentSynced, providerAggregateComment, comment.ID, outboxPayload, providerEvent.OccurredAt), nil
}

func (s *Service) reconciliationRelationshipOutbox(cursor entity.SyncCursor, relationship entity.ProviderRelationship) (entity.OutboxEvent, error) {
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:      string(cursor.ProviderSlug),
		ExternalAccountID: cursor.ExternalAccountID.String(),
		RelationshipID:    relationship.ID.String(),
		RelationshipType:  relationship.RelationshipType,
		Source:            string(relationship.Source),
		SyncCursorID:      cursor.ID.String(),
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEventRecord(s.ids.New(), providerEventRelationshipSynced, providerAggregateRelationship, relationship.ID, payload, relationship.CreatedAt), nil
}

func (s *Service) syncCursorAdvancedOutbox(cursor entity.SyncCursor) (entity.OutboxEvent, error) {
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:      string(cursor.ProviderSlug),
		ExternalAccountID: cursor.ExternalAccountID.String(),
		SyncCursorID:      cursor.ID.String(),
		ScopeType:         string(cursor.ScopeType),
		ScopeRef:          cursor.ScopeRef,
		Kind:              string(cursor.ArtifactKind),
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	occurredAt := s.clock.Now().UTC()
	return outboxEventRecord(s.ids.New(), providerEventSyncCursorAdvanced, providerAggregateSyncCursor, cursor.ID, payload, occurredAt), nil
}

func (s *Service) runtimeStateFromReconciliation(cursor entity.SyncCursor, now time.Time, providerResult providerclient.ReconciliationResult, lastError string) entity.ProviderAccountRuntimeState {
	status := enum.ProviderAccountRuntimeStatusActive
	if lastError != "" {
		status = enum.ProviderAccountRuntimeStatusError
	}
	if lastError == reconciliationErrorProviderAuthFailed {
		status = enum.ProviderAccountRuntimeStatusReauthorizationRequired
	}
	if lastError == reconciliationErrorProviderRateLimited || reconciliationHasExhaustedLimit(providerResult.LimitSnapshots) {
		status = enum.ProviderAccountRuntimeStatusLimited
	}
	state := entity.ProviderAccountRuntimeState{
		Base: entity.Base{
			ID:        stableUUID("account-runtime", cursor.ExternalAccountID.String(), string(cursor.ProviderSlug)),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		ExternalAccountID: cursor.ExternalAccountID,
		ProviderSlug:      cursor.ProviderSlug,
		Status:            status,
		LastCheckedAt:     &now,
		LastErrorCode:     lastError,
		LastErrorMessage:  lastError,
	}
	if lastError == "" {
		state.LastSuccessAt = &now
		state.LastErrorMessage = ""
	}
	return state
}

type reconciliationScope struct {
	Type string
	ID   string
}

func reconciliationUsageScope(cursor entity.SyncCursor) (reconciliationScope, error) {
	scopeRef := strings.TrimSpace(cursor.ScopeRef)
	switch cursor.ScopeType {
	case enum.SyncCursorScopeRepository:
		if scopeRef == "" {
			return reconciliationScope{}, errs.ErrInvalidArgument
		}
		return reconciliationScope{Type: accesscatalog.ScopeRepository, ID: scopeRef}, nil
	case enum.SyncCursorScopeWorkItem:
		repository, err := repositoryFromWorkItemScopeRef(scopeRef)
		if err != nil {
			return reconciliationScope{}, err
		}
		return reconciliationScope{Type: accesscatalog.ScopeRepository, ID: repository}, nil
	case enum.SyncCursorScopeOrganization:
		if scopeRef == "" {
			return reconciliationScope{}, errs.ErrInvalidArgument
		}
		return reconciliationScope{Type: accesscatalog.ScopeOrganization, ID: scopeRef}, nil
	default:
		return reconciliationScope{}, errs.ErrInvalidArgument
	}
}

func repositoryFromWorkItemScopeRef(scopeRef string) (string, error) {
	scopeRef = strings.TrimSpace(scopeRef)
	if scopeRef == "" {
		return "", errs.ErrInvalidArgument
	}
	if value, ok := strings.CutPrefix(scopeRef, "provider_object_id:"); ok {
		return repositoryFromProviderObjectID(value)
	}
	if value, ok := strings.CutPrefix(scopeRef, "web_url:"); ok {
		return repositoryFromGitHubWebURL(value)
	}
	repository, _, ok := strings.Cut(scopeRef, "#")
	if !ok || strings.TrimSpace(repository) == "" {
		return "", errs.ErrInvalidArgument
	}
	return strings.TrimSpace(repository), nil
}

func repositoryFromProviderObjectID(raw string) (string, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) == 4 && parts[0] == string(enum.ProviderSlugGitHub) {
		repository := strings.TrimSpace(parts[1])
		if repository != "" {
			return repository, nil
		}
	}
	return "", errs.ErrInvalidArgument
}

func repositoryFromGitHubWebURL(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	parts := strings.Split(trimmed, "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "github.com" && parts[i+1] != "" && parts[i+2] != "" {
			return parts[i+1] + "/" + parts[i+2], nil
		}
	}
	return "", errs.ErrInvalidArgument
}

func nextCursorValue(current string, incoming string, now time.Time) string {
	incoming = strings.TrimSpace(incoming)
	if incoming != "" {
		return incoming
	}
	if strings.TrimSpace(current) != "" {
		return current
	}
	return now.UTC().Format(time.RFC3339Nano)
}

func overlapSinceFromCursorValue(cursorValue string) *time.Time {
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(cursorValue)); err == nil {
		overlap := parsed.UTC().Add(-5 * time.Minute)
		return &overlap
	}
	return nil
}

func jsonPayloadOrDefault(raw []byte, fallback string) []byte {
	if len(raw) == 0 {
		return []byte(fallback)
	}
	return raw
}

func reconciliationHasExhaustedLimit(snapshots []entity.ProviderLimitSnapshot) bool {
	for _, snapshot := range snapshots {
		if snapshot.Remaining != nil && *snapshot.Remaining == 0 {
			return true
		}
	}
	return false
}

func reconciliationProviderError(err *providerclient.Error) (string, error) {
	switch err.Kind {
	case providerclient.ErrorKindRateLimited:
		return reconciliationErrorProviderRateLimited, nil
	case providerclient.ErrorKindAuthFailed:
		return reconciliationErrorProviderAuthFailed, errs.ErrPreconditionFailed
	case providerclient.ErrorKindNotFound:
		return reconciliationErrorProviderNotFound, errs.ErrNotFound
	case providerclient.ErrorKindUnsupported:
		return reconciliationErrorProviderUnsupported, errs.ErrPreconditionFailed
	case providerclient.ErrorKindPermanent:
		return reconciliationErrorProviderPermanent, errs.ErrPreconditionFailed
	default:
		return reconciliationErrorProviderTransient, errs.ErrDependencyUnavailable
	}
}

func mapSecretResolverError(err error) error {
	switch {
	case errors.Is(err, secretresolver.ErrInvalidRef),
		errors.Is(err, secretresolver.ErrUnsupportedStoreType),
		errors.Is(err, secretresolver.ErrSecretNotFound):
		return errs.ErrPreconditionFailed
	case errors.Is(err, context.Canceled):
		return err
	default:
		return errs.ErrDependencyUnavailable
	}
}

func joinErrors(primary error, secondary error) error {
	if secondary == nil {
		return primary
	}
	if primary == nil {
		return secondary
	}
	return fmt.Errorf("%w: %v", primary, secondary)
}
