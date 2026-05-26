package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

const (
	watermarkStart                       = "<!-- kodex:artifact v1"
	watermarkEnd                         = "-->"
	relationshipSource                   = "source"
	relationshipParent                   = "parent"
	relationshipNext                     = "next"
	relationshipProjectRepositoryBinding = "project_repository_binding"
	onboardingMergeSignalStatus          = "merged"
	projectionSummarySize                = 320
)

var whitespacePattern = regexp.MustCompile(`\s+`)

type watermarkParseResult struct {
	status enum.WorkItemWatermarkStatus
	fields map[string]string
}

func (s *Service) projectionUpdateFromFacts(ctx context.Context, webhook entity.WebhookEvent, facts value.ProviderWebhookFacts) (providerrepo.ProjectionUpdate, []entity.OutboxEvent, error) {
	now := s.clock.Now().UTC()
	var update providerrepo.ProjectionUpdate
	var events []entity.OutboxEvent
	if facts.WorkItem != nil {
		workItem, relationships, err := workItemProjectionFromSnapshot(*facts.WorkItem, now)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, err
		}
		if facts.MergeSignal != nil {
			enriched, err := s.enrichOnboardingMergeProjection(ctx, workItem)
			if err != nil {
				return providerrepo.ProjectionUpdate{}, nil, err
			}
			workItem = enriched
		}
		update.WorkItem = &workItem
		update.Relationships = append(update.Relationships, relationships...)
		event, err := s.workItemSyncedOutbox(webhook, workItem)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, err
		}
		events = append(events, event)
		if facts.MergeSignal != nil {
			mergeSignal, ok, err := s.repositoryMergeSignalFromFacts(webhook, facts, workItem)
			if err != nil {
				return providerrepo.ProjectionUpdate{}, nil, err
			}
			if ok {
				update.MergeSignal = &mergeSignal
				event, err := s.repositoryMergedOutbox(webhook, mergeSignal)
				if err != nil {
					return providerrepo.ProjectionUpdate{}, nil, err
				}
				events = append(events, event)
			}
		}
	}
	if facts.Comment != nil && update.WorkItem != nil {
		comment := commentProjectionFromSnapshot(*facts.Comment, now)
		update.Comments = append(update.Comments, comment)
		event, err := s.commentSyncedOutbox(webhook, comment, facts)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, err
		}
		events = append(events, event)
	}
	for _, relationship := range update.Relationships {
		event, err := s.relationshipSyncedOutbox(webhook, relationship, facts)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, err
		}
		events = append(events, event)
	}
	return update, events, nil
}

func workItemProjectionFromSnapshot(snapshot value.ProviderWorkItemSnapshot, now time.Time) (entity.ProviderWorkItemProjection, []entity.ProviderRelationship, error) {
	providerSlug := enum.ProviderSlug(strings.TrimSpace(snapshot.ProviderSlug))
	kind := enum.WorkItemKind(strings.TrimSpace(snapshot.Kind))
	providerID := strings.TrimSpace(snapshot.ProviderWorkItemID)
	repositoryFullName := strings.TrimSpace(snapshot.RepositoryFullName)
	body := strings.TrimSpace(snapshot.Body)
	watermark := parseWatermark(body, kind)
	watermarkJSON, err := json.Marshal(watermark.fields)
	if err != nil {
		return entity.ProviderWorkItemProjection{}, nil, err
	}
	labels := trimStrings(snapshot.Labels)
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return entity.ProviderWorkItemProjection{}, nil, err
	}
	projectID, err := optionalSnapshotUUID(snapshot.ProjectID)
	if err != nil {
		return entity.ProviderWorkItemProjection{}, nil, err
	}
	repositoryID, err := optionalSnapshotUUID(snapshot.RepositoryID)
	if err != nil {
		return entity.ProviderWorkItemProjection{}, nil, err
	}
	assigneesJSON, err := json.Marshal(trimStrings(snapshot.Assignees))
	if err != nil {
		return entity.ProviderWorkItemProjection{}, nil, err
	}
	providerUpdatedAt := timePtrIfSet(snapshot.ProviderUpdatedAt)
	workItemID := stableUUID("work-item", string(providerSlug), providerID)
	workItem := entity.ProviderWorkItemProjection{
		Base:               entity.Base{ID: workItemID, Version: 1, CreatedAt: now, UpdatedAt: now},
		ProjectID:          projectID,
		RepositoryID:       repositoryID,
		ProviderSlug:       providerSlug,
		ProviderWorkItemID: providerID,
		RepositoryFullName: repositoryFullName,
		Kind:               kind,
		Number:             snapshot.Number,
		URL:                strings.TrimSpace(snapshot.URL),
		Title:              strings.TrimSpace(snapshot.Title),
		State:              strings.TrimSpace(snapshot.State),
		WorkItemType:       workItemType(watermark.fields, labels),
		LabelsJSON:         labelsJSON,
		AssigneesJSON:      assigneesJSON,
		Milestone:          strings.TrimSpace(snapshot.Milestone),
		ProjectFieldsJSON:  []byte(`{}`),
		WatermarkStatus:    watermark.status,
		WatermarkJSON:      watermarkJSON,
		BodyDigest:         bodyDigest(body),
		ProviderUpdatedAt:  providerUpdatedAt,
		SyncedAt:           now,
		DriftStatus:        enum.WorkItemDriftStatusFresh,
	}
	relationships := relationshipsFromWatermark(workItem.ID, watermark.fields, now)
	return workItem, relationships, nil
}

func optionalSnapshotUUID(raw string) (*uuid.UUID, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return nil, fmt.Errorf("invalid snapshot uuid %q", value)
	}
	return &id, nil
}

func projectRepositoryBindingRelationships(workItemID uuid.UUID, projectID *uuid.UUID, repositoryID *uuid.UUID, now time.Time) []entity.ProviderRelationship {
	if projectID == nil || repositoryID == nil {
		return nil
	}
	targetProviderRef := "project-catalog:project:" + projectID.String() + ":repository:" + repositoryID.String()
	return []entity.ProviderRelationship{{
		ID:                stableUUID("relationship", workItemID.String(), relationshipProjectRepositoryBinding, targetProviderRef),
		Version:           1,
		SourceWorkItemID:  workItemID,
		TargetProviderRef: targetProviderRef,
		RelationshipType:  relationshipProjectRepositoryBinding,
		Source:            enum.RelationshipSourceManual,
		Confidence:        enum.RelationshipConfidenceConfirmed,
		CreatedAt:         now,
	}}
}

func (s *Service) enrichOnboardingMergeProjection(ctx context.Context, workItem entity.ProviderWorkItemProjection) (entity.ProviderWorkItemProjection, error) {
	existing, err := s.repository.GetWorkItemProjection(ctx, query.ProviderTargetLookup{
		ProviderSlug:     workItem.ProviderSlug,
		ProviderObjectID: workItem.ProviderWorkItemID,
	})
	if errors.Is(err, errs.ErrNotFound) {
		return workItem, nil
	}
	if err != nil {
		return entity.ProviderWorkItemProjection{}, err
	}
	if workItem.ProjectID == nil {
		workItem.ProjectID = existing.ProjectID
	}
	if workItem.RepositoryID == nil {
		workItem.RepositoryID = existing.RepositoryID
	}
	if strings.TrimSpace(workItem.WorkItemType) == "" {
		workItem.WorkItemType = existing.WorkItemType
	}
	if workItem.WatermarkStatus == enum.WorkItemWatermarkStatusMissing && len(existing.WatermarkJSON) > 0 {
		workItem.WatermarkStatus = existing.WatermarkStatus
		workItem.WatermarkJSON = append(workItem.WatermarkJSON[:0], existing.WatermarkJSON...)
	}
	return workItem, nil
}

func (s *Service) repositoryMergeSignalFromFacts(webhook entity.WebhookEvent, facts value.ProviderWebhookFacts, workItem entity.ProviderWorkItemProjection) (entity.RepositoryMergeSignal, bool, error) {
	if facts.MergeSignal == nil || workItem.Kind != enum.WorkItemKindPullRequest || strings.TrimSpace(workItem.State) != onboardingMergeSignalStatus {
		return entity.RepositoryMergeSignal{}, false, nil
	}
	if workItem.ProjectID == nil || workItem.RepositoryID == nil {
		return entity.RepositoryMergeSignal{}, false, nil
	}
	kind, ok := onboardingMergeSignalKind(workItem, *facts.MergeSignal)
	if !ok {
		return entity.RepositoryMergeSignal{}, false, nil
	}
	if strings.TrimSpace(facts.MergeSignal.MergeCommitSHA) == "" ||
		strings.TrimSpace(facts.MergeSignal.BaseBranch) == "" ||
		strings.TrimSpace(facts.MergeSignal.HeadBranch) == "" {
		return entity.RepositoryMergeSignal{}, false, errs.ErrInvalidArgument
	}
	now := s.clock.Now().UTC()
	mergedAt := facts.MergeSignal.MergedAt.UTC()
	if mergedAt.IsZero() {
		mergedAt = facts.OccurredAt.UTC()
	}
	if mergedAt.IsZero() {
		mergedAt = webhook.ReceivedAt.UTC()
	}
	sourceRef := onboardingMergeSourceRef(workItem, *facts.MergeSignal)
	operationRef := onboardingMergeOperationRef(kind, workItem, *facts.MergeSignal)
	signalKey := repositoryMergeSignalKey(workItem.ProviderSlug, kind, workItem.ProviderWorkItemID)
	signal := entity.RepositoryMergeSignal{
		Base: entity.Base{
			ID:        stableUUID("repository-merge-signal", signalKey),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		SignalKey:                   signalKey,
		Kind:                        kind,
		ProviderSlug:                workItem.ProviderSlug,
		ProjectID:                   workItem.ProjectID,
		RepositoryID:                workItem.RepositoryID,
		RepositoryFullName:          workItem.RepositoryFullName,
		ProviderRepositoryID:        strings.TrimSpace(facts.RepositoryProviderID),
		WorkItemProjectionID:        workItem.ID,
		ProviderWorkItemID:          workItem.ProviderWorkItemID,
		PullRequestNumber:           workItem.Number,
		PullRequestProviderID:       strings.TrimSpace(facts.MergeSignal.PullRequestProviderID),
		PullRequestURL:              strings.TrimSpace(facts.MergeSignal.PullRequestURL),
		BaseBranch:                  strings.TrimSpace(facts.MergeSignal.BaseBranch),
		HeadBranch:                  strings.TrimSpace(facts.MergeSignal.HeadBranch),
		MergeCommitSHA:              strings.TrimSpace(facts.MergeSignal.MergeCommitSHA),
		SourceRef:                   sourceRef,
		RelatedProviderOperationRef: operationRef,
		WatermarkDigest:             bodyDigest(string(workItem.WatermarkJSON)),
		ObservedAt:                  webhook.ReceivedAt.UTC(),
		MergedAt:                    mergedAt,
		Status:                      enum.RepositoryMergeSignalStatusMerged,
	}
	return signal, true, nil
}

func onboardingMergeSignalKind(workItem entity.ProviderWorkItemProjection, signal value.ProviderRepositoryMergeSignalSnapshot) (enum.RepositoryMergeSignalKind, bool) {
	candidates := []string{workItem.WorkItemType, signal.HeadBranch, signal.SourceRef}
	watermarkFields := map[string]string{}
	if len(workItem.WatermarkJSON) > 0 {
		_ = json.Unmarshal(workItem.WatermarkJSON, &watermarkFields)
	}
	candidates = append(candidates, watermarkFields["work_type"], watermarkFields["kind"], watermarkFields["source_ref"])
	for _, candidate := range candidates {
		normalized := strings.ToLower(strings.TrimSpace(candidate))
		switch {
		case strings.Contains(normalized, "bootstrap"):
			return enum.RepositoryMergeSignalKindBootstrap, true
		case strings.Contains(normalized, "adoption"):
			return enum.RepositoryMergeSignalKindAdoption, true
		}
	}
	return "", false
}

func onboardingMergeSourceRef(workItem entity.ProviderWorkItemProjection, signal value.ProviderRepositoryMergeSignalSnapshot) string {
	watermarkFields := map[string]string{}
	if len(workItem.WatermarkJSON) > 0 {
		_ = json.Unmarshal(workItem.WatermarkJSON, &watermarkFields)
	}
	if sourceRef := strings.TrimSpace(watermarkFields["source_ref"]); sourceRef != "" {
		return sourceRef
	}
	if sourceRef := strings.TrimSpace(signal.SourceRef); sourceRef != "" {
		return sourceRef
	}
	return strings.TrimSpace(signal.HeadBranch)
}

func onboardingMergeOperationRef(kind enum.RepositoryMergeSignalKind, workItem entity.ProviderWorkItemProjection, signal value.ProviderRepositoryMergeSignalSnapshot) string {
	watermarkFields := map[string]string{}
	if len(workItem.WatermarkJSON) > 0 {
		_ = json.Unmarshal(workItem.WatermarkJSON, &watermarkFields)
	}
	for _, field := range []string{"provider_operation_ref", "operation_ref"} {
		if operationRef := strings.TrimSpace(watermarkFields[field]); operationRef != "" {
			return operationRef
		}
	}
	operationType := enum.ProviderOperationCreateBootstrapPullRequest
	if kind == enum.RepositoryMergeSignalKindAdoption {
		operationType = enum.ProviderOperationCreateAdoptionPullRequest
	}
	providerWorkItemID := strings.TrimSpace(workItem.ProviderWorkItemID)
	if providerWorkItemID == "" {
		providerWorkItemID = strings.TrimSpace(signal.PullRequestURL)
	}
	return "provider-hub:" + string(operationType) + ":" + providerWorkItemID
}

func repositoryMergeSignalKey(providerSlug enum.ProviderSlug, kind enum.RepositoryMergeSignalKind, providerWorkItemID string) string {
	return "provider:" + string(providerSlug) + ":repository_merge:" + string(kind) + ":" + strings.TrimSpace(providerWorkItemID)
}

func commentProjectionFromSnapshot(snapshot value.ProviderCommentSnapshot, now time.Time) entity.ProviderCommentProjection {
	providerCommentID := strings.TrimSpace(snapshot.ProviderCommentID)
	providerWorkItemID := strings.TrimSpace(snapshot.ProviderWorkItemID)
	body := strings.TrimSpace(snapshot.Body)
	providerSlug := strings.TrimSpace(snapshot.ProviderSlug)
	return entity.ProviderCommentProjection{
		Base:                 entity.Base{ID: stableUUID("comment", providerSlug, providerWorkItemID, providerCommentID), Version: 1, CreatedAt: now, UpdatedAt: now},
		WorkItemProjectionID: stableUUID("work-item", providerSlug, providerWorkItemID),
		ProviderCommentID:    providerCommentID,
		Kind:                 enum.CommentKind(strings.TrimSpace(snapshot.Kind)),
		ReviewState:          enum.ReviewState(strings.TrimSpace(snapshot.ReviewState)),
		AuthorProviderLogin:  strings.TrimSpace(snapshot.AuthorLogin),
		BodyDigest:           bodyDigest(body),
		Summary:              summary(body),
		ProviderCreatedAt:    timePtrIfSet(snapshot.ProviderCreatedAt),
		ProviderUpdatedAt:    timePtrIfSet(snapshot.ProviderUpdatedAt),
	}
}

func parseWatermark(body string, actualKind enum.WorkItemKind) watermarkParseResult {
	start := strings.Index(body, watermarkStart)
	if start < 0 {
		return watermarkParseResult{status: enum.WorkItemWatermarkStatusMissing, fields: map[string]string{}}
	}
	rest := body[start+len(watermarkStart):]
	end := strings.Index(rest, watermarkEnd)
	if end < 0 {
		return watermarkParseResult{status: enum.WorkItemWatermarkStatusInvalid, fields: map[string]string{"error": "watermark end marker missing"}}
	}
	fields := make(map[string]string)
	for _, line := range strings.Split(rest[:end], "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			fields[key] = value
		}
	}
	if fields["kind"] == "" || fields["managed_by"] == "" || fields["work_type"] == "" {
		fields["error"] = "required watermark fields missing"
		return watermarkParseResult{status: enum.WorkItemWatermarkStatusInvalid, fields: fields}
	}
	if fields["kind"] != string(actualKind) {
		fields["error"] = "watermark kind mismatch"
		return watermarkParseResult{status: enum.WorkItemWatermarkStatusInvalid, fields: fields}
	}
	if actualKind != enum.WorkItemKindIssue && strings.TrimSpace(fields["source_ref"]) == "" {
		fields["error"] = "source_ref is required for pull request or merge request"
		return watermarkParseResult{status: enum.WorkItemWatermarkStatusInvalid, fields: fields}
	}
	return watermarkParseResult{status: enum.WorkItemWatermarkStatusValid, fields: fields}
}

func workItemType(watermark map[string]string, labels []string) string {
	if value := strings.TrimSpace(watermark["work_type"]); value != "" {
		return value
	}
	for _, label := range labels {
		trimmed := strings.TrimSpace(label)
		for _, prefix := range []string{"type:", "type/", "work_type:"} {
			if strings.HasPrefix(trimmed, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			}
		}
	}
	return ""
}

func relationshipsFromWatermark(sourceID uuid.UUID, watermark map[string]string, now time.Time) []entity.ProviderRelationship {
	candidates := []struct {
		field string
		kind  string
	}{
		{field: "source_ref", kind: relationshipSource},
		{field: "parent_ref", kind: relationshipParent},
		{field: "next_ref", kind: relationshipNext},
	}
	relationships := make([]entity.ProviderRelationship, 0, len(candidates))
	for _, candidate := range candidates {
		target := strings.TrimSpace(watermark[candidate.field])
		if target == "" {
			continue
		}
		relationships = append(relationships, entity.ProviderRelationship{
			ID:                stableUUID("relationship", sourceID.String(), candidate.kind, target),
			SourceWorkItemID:  sourceID,
			TargetProviderRef: target,
			RelationshipType:  candidate.kind,
			Source:            enum.RelationshipSourceWatermark,
			Confidence:        enum.RelationshipConfidenceConfirmed,
			CreatedAt:         now,
		})
	}
	return relationships
}

func (s *Service) workItemSyncedOutbox(webhook entity.WebhookEvent, workItem entity.ProviderWorkItemProjection) (entity.OutboxEvent, error) {
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(webhook.ProviderSlug),
		WebhookEventID:       webhook.ID.String(),
		RepositoryFullName:   workItem.RepositoryFullName,
		WorkItemProjectionID: workItem.ID.String(),
		ProviderWorkItemID:   workItem.ProviderWorkItemID,
		Kind:                 string(workItem.Kind),
		Number:               workItem.Number,
		WatermarkStatus:      string(workItem.WatermarkStatus),
		DriftStatus:          string(workItem.DriftStatus),
		Version:              workItem.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEventRecord(s.ids.New(), providerEventWorkItemSynced, providerAggregateWorkItem, workItem.ID, payload, workItem.SyncedAt), nil
}

func (s *Service) commentSyncedOutbox(webhook entity.WebhookEvent, comment entity.ProviderCommentProjection, facts value.ProviderWebhookFacts) (entity.OutboxEvent, error) {
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(webhook.ProviderSlug),
		WebhookEventID:       webhook.ID.String(),
		RepositoryFullName:   facts.RepositoryFullName,
		WorkItemProjectionID: comment.WorkItemProjectionID.String(),
		CommentProjectionID:  comment.ID.String(),
		ProviderWorkItemID:   facts.ProviderWorkItemID,
		ProviderCommentID:    comment.ProviderCommentID,
		Kind:                 string(comment.Kind),
		ReviewState:          string(comment.ReviewState),
		Number:               facts.Number,
		Version:              comment.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	occurredAt := comment.UpdatedAt
	if comment.ProviderUpdatedAt != nil {
		occurredAt = *comment.ProviderUpdatedAt
	}
	return outboxEventRecord(s.ids.New(), providerEventCommentSynced, providerAggregateComment, comment.ID, payload, occurredAt), nil
}

func (s *Service) relationshipSyncedOutbox(webhook entity.WebhookEvent, relationship entity.ProviderRelationship, facts value.ProviderWebhookFacts) (entity.OutboxEvent, error) {
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(webhook.ProviderSlug),
		WebhookEventID:       webhook.ID.String(),
		RepositoryFullName:   facts.RepositoryFullName,
		WorkItemProjectionID: relationship.SourceWorkItemID.String(),
		ProviderWorkItemID:   facts.ProviderWorkItemID,
		RelationshipID:       relationship.ID.String(),
		RelationshipType:     relationship.RelationshipType,
		Source:               string(relationship.Source),
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEventRecord(s.ids.New(), providerEventRelationshipSynced, providerAggregateRelationship, relationship.ID, payload, relationship.CreatedAt), nil
}

func (s *Service) repositoryMergedOutbox(webhook entity.WebhookEvent, signal entity.RepositoryMergeSignal) (entity.OutboxEvent, error) {
	eventType := providerEventRepositoryBootstrapMerged
	if signal.Kind == enum.RepositoryMergeSignalKindAdoption {
		eventType = providerEventRepositoryAdoptionMerged
	}
	projectID := ""
	if signal.ProjectID != nil {
		projectID = signal.ProjectID.String()
	}
	repositoryID := ""
	if signal.RepositoryID != nil {
		repositoryID = signal.RepositoryID.String()
	}
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:                string(signal.ProviderSlug),
		WebhookEventID:              webhook.ID.String(),
		DeliveryID:                  webhook.DeliveryID,
		EventName:                   webhook.EventName,
		RepositoryMergeSignalID:     signal.ID.String(),
		SignalKey:                   signal.SignalKey,
		SignalKind:                  string(signal.Kind),
		ProjectID:                   projectID,
		RepositoryID:                repositoryID,
		RepositoryFullName:          signal.RepositoryFullName,
		ProviderRepositoryID:        signal.ProviderRepositoryID,
		ProviderWorkItemID:          signal.ProviderWorkItemID,
		WorkItemProjectionID:        signal.WorkItemProjectionID.String(),
		Number:                      signal.PullRequestNumber,
		PullRequestProviderID:       signal.PullRequestProviderID,
		PullRequestURL:              signal.PullRequestURL,
		BaseBranch:                  signal.BaseBranch,
		HeadBranch:                  signal.HeadBranch,
		MergeCommitSHA:              signal.MergeCommitSHA,
		SourceRef:                   signal.SourceRef,
		RelatedProviderOperationRef: signal.RelatedProviderOperationRef,
		WatermarkDigest:             signal.WatermarkDigest,
		ObservedAt:                  signal.ObservedAt.Format(time.RFC3339Nano),
		MergedAt:                    signal.MergedAt.Format(time.RFC3339Nano),
		Status:                      string(signal.Status),
		Version:                     signal.Version,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEventRecord(s.ids.New(), eventType, providerAggregateRepositoryMergeSignal, signal.ID, payload, signal.MergedAt), nil
}

func stableUUID(parts ...string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("kodex/provider-hub/"+strings.Join(parts, "/")))
}

func bodyDigest(body string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(body)))
	return hex.EncodeToString(digest[:])
}

func summary(body string) string {
	text := strings.TrimSpace(whitespacePattern.ReplaceAllString(body, " "))
	runes := []rune(text)
	if len(runes) <= projectionSummarySize {
		return text
	}
	return strings.TrimSpace(string(runes[:projectionSummarySize]))
}

func timePtrIfSet(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	converted := value.UTC()
	return &converted
}
