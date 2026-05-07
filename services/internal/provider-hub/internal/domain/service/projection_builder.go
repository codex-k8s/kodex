package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

const (
	watermarkStart        = "<!-- kodex:artifact v1"
	watermarkEnd          = "-->"
	relationshipSource    = "source"
	relationshipParent    = "parent"
	relationshipNext      = "next"
	projectionSummarySize = 320
)

var whitespacePattern = regexp.MustCompile(`\s+`)

type watermarkParseResult struct {
	status enum.WorkItemWatermarkStatus
	fields map[string]string
}

func (s *Service) projectionUpdateFromFacts(webhook entity.WebhookEvent, facts value.ProviderWebhookFacts) (providerrepo.ProjectionUpdate, []entity.OutboxEvent, error) {
	now := s.clock.Now().UTC()
	var update providerrepo.ProjectionUpdate
	var events []entity.OutboxEvent
	if facts.WorkItem != nil {
		workItem, relationships, err := workItemProjectionFromSnapshot(*facts.WorkItem, now)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, err
		}
		update.WorkItem = &workItem
		update.Relationships = append(update.Relationships, relationships...)
		event, err := s.workItemSyncedOutbox(webhook, workItem)
		if err != nil {
			return providerrepo.ProjectionUpdate{}, nil, err
		}
		events = append(events, event)
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
	watermark := parseWatermark(body)
	watermarkJSON, err := json.Marshal(watermark.fields)
	if err != nil {
		return entity.ProviderWorkItemProjection{}, nil, err
	}
	labels := trimStrings(snapshot.Labels)
	labelsJSON, err := json.Marshal(labels)
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
	return workItem, relationshipsFromWatermark(workItem.ID, watermark.fields, now), nil
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
		AuthorProviderLogin:  strings.TrimSpace(snapshot.AuthorLogin),
		BodyDigest:           bodyDigest(body),
		Summary:              summary(body),
		ProviderCreatedAt:    timePtrIfSet(snapshot.ProviderCreatedAt),
		ProviderUpdatedAt:    timePtrIfSet(snapshot.ProviderUpdatedAt),
	}
}

func parseWatermark(body string) watermarkParseResult {
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
	if fields["kind"] == "" || fields["managed_by"] == "" {
		fields["error"] = "required watermark fields missing"
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
