package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

type webhookNormalizationResult struct {
	status         enum.WebhookProcessingStatus
	lastError      string
	providerEvents []entity.ProviderEvent
	outboxEvents   []entity.OutboxEvent
}

type githubWebhookFacts struct {
	eventType            string
	aggregateType        string
	aggregateID          string
	providerWorkItemID   string
	providerCommentID    string
	kind                 string
	number               int64
	repositoryFullName   string
	repositoryProviderID string
	occurredAt           time.Time
}

type githubWorkItemSource struct {
	repository     githubRepositoryWebhookPayload
	id             json.Number
	kind           string
	number         int64
	updatedAt      string
	missingMessage string
}

type githubRepositoryWebhookPayload struct {
	ID       json.Number `json:"id"`
	FullName string      `json:"full_name"`
}

type githubIssueWebhookPayload struct {
	ID        json.Number `json:"id"`
	Number    int64       `json:"number"`
	UpdatedAt string      `json:"updated_at"`
}

type githubPullRequestWebhookPayload struct {
	ID        json.Number `json:"id"`
	Number    int64       `json:"number"`
	UpdatedAt string      `json:"updated_at"`
}

type githubCommentWebhookPayload struct {
	ID        json.Number `json:"id"`
	UpdatedAt string      `json:"updated_at"`
}

type githubReviewWebhookPayload struct {
	ID        json.Number `json:"id"`
	UpdatedAt string      `json:"updated_at"`
}

type githubIssuesWebhookPayload struct {
	Repository githubRepositoryWebhookPayload `json:"repository"`
	Issue      githubIssueWebhookPayload      `json:"issue"`
}

type githubPullRequestWebhookEnvelope struct {
	Repository  githubRepositoryWebhookPayload  `json:"repository"`
	PullRequest githubPullRequestWebhookPayload `json:"pull_request"`
}

type githubIssueCommentWebhookEnvelope struct {
	Repository githubRepositoryWebhookPayload `json:"repository"`
	Issue      githubIssueWebhookPayload      `json:"issue"`
	Comment    githubCommentWebhookPayload    `json:"comment"`
}

type githubPullRequestReviewWebhookEnvelope struct {
	Repository  githubRepositoryWebhookPayload  `json:"repository"`
	PullRequest githubPullRequestWebhookPayload `json:"pull_request"`
	Review      githubReviewWebhookPayload      `json:"review"`
}

type githubPullRequestReviewCommentWebhookEnvelope struct {
	Repository  githubRepositoryWebhookPayload  `json:"repository"`
	PullRequest githubPullRequestWebhookPayload `json:"pull_request"`
	Comment     githubCommentWebhookPayload     `json:"comment"`
}

func (s *Service) normalizeWebhook(webhook entity.WebhookEvent) (webhookNormalizationResult, error) {
	receivedEvent, err := s.webhookReceivedOutbox(webhook)
	if err != nil {
		return webhookNormalizationResult{}, err
	}
	facts, ok, err := normalizeProviderPayload(webhook)
	if err != nil {
		return webhookNormalizationResult{
			status:       enum.WebhookProcessingStatusFailed,
			lastError:    err.Error(),
			outboxEvents: []entity.OutboxEvent{receivedEvent},
		}, nil
	}
	if !ok {
		return webhookNormalizationResult{
			status:       enum.WebhookProcessingStatusIgnored,
			outboxEvents: []entity.OutboxEvent{receivedEvent},
		}, nil
	}

	providerEventID := s.ids.New()
	providerEventPayload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(webhook.ProviderSlug),
		WebhookEventID:       webhook.ID.String(),
		ProviderEventID:      providerEventID.String(),
		DeliveryID:           webhook.DeliveryID,
		EventName:            webhook.EventName,
		ProviderRepositoryID: facts.repositoryProviderID,
		RepositoryFullName:   facts.repositoryFullName,
		ProviderWorkItemID:   facts.providerWorkItemID,
		ProviderCommentID:    facts.providerCommentID,
		Kind:                 facts.kind,
		Number:               facts.number,
	})
	if err != nil {
		return webhookNormalizationResult{}, err
	}
	sourceID := webhook.ID
	providerEvent := entity.ProviderEvent{
		ID:                   providerEventID,
		SourceWebhookEventID: &sourceID,
		EventType:            facts.eventType,
		AggregateType:        facts.aggregateType,
		AggregateID:          facts.aggregateID,
		PayloadJSON:          providerEventPayload,
		OccurredAt:           facts.occurredAt,
	}
	normalizedOutbox, err := s.webhookNormalizedOutbox(webhook, providerEvent, facts)
	if err != nil {
		return webhookNormalizationResult{}, err
	}
	return webhookNormalizationResult{
		status:         enum.WebhookProcessingStatusProcessed,
		providerEvents: []entity.ProviderEvent{providerEvent},
		outboxEvents:   []entity.OutboxEvent{receivedEvent, normalizedOutbox},
	}, nil
}

func (s *Service) webhookReceivedOutbox(webhook entity.WebhookEvent) (entity.OutboxEvent, error) {
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(webhook.ProviderSlug),
		WebhookEventID:       webhook.ID.String(),
		DeliveryID:           webhook.DeliveryID,
		EventName:            webhook.EventName,
		ProviderRepositoryID: webhook.RepositoryProviderID,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEventRecord(s.ids.New(), providerEventWebhookReceived, providerAggregateWebhookEvent, webhook.ID, payload, webhook.ReceivedAt), nil
}

func (s *Service) webhookNormalizedOutbox(webhook entity.WebhookEvent, providerEvent entity.ProviderEvent, facts githubWebhookFacts) (entity.OutboxEvent, error) {
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(webhook.ProviderSlug),
		WebhookEventID:       webhook.ID.String(),
		ProviderEventID:      providerEvent.ID.String(),
		DeliveryID:           webhook.DeliveryID,
		EventName:            webhook.EventName,
		ProviderRepositoryID: facts.repositoryProviderID,
		RepositoryFullName:   facts.repositoryFullName,
		ProviderWorkItemID:   facts.providerWorkItemID,
		ProviderCommentID:    facts.providerCommentID,
		Kind:                 facts.kind,
		Number:               facts.number,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEventRecord(s.ids.New(), providerEventWebhookNormalized, providerAggregateProviderEvent, providerEvent.ID, payload, providerEvent.OccurredAt), nil
}

func outboxEventRecord(id uuid.UUID, eventType string, aggregateType string, aggregateID uuid.UUID, payload []byte, occurredAt time.Time) entity.OutboxEvent {
	event := outboxlib.NewEvent(id, eventType, providerEventSchemaVersion, aggregateType, aggregateID, payload, occurredAt, 0)
	return outboxlib.RecordFromParts(event, outboxlib.RecordDelivery{}, outboxlib.RecordFailure{})
}

func normalizeProviderPayload(webhook entity.WebhookEvent) (githubWebhookFacts, bool, error) {
	if webhook.ProviderSlug != enum.ProviderSlugGitHub {
		return githubWebhookFacts{}, false, nil
	}
	if err := validateJSONObject(webhook.PayloadJSON); err != nil {
		return githubWebhookFacts{}, false, err
	}
	facts, ok, err := normalizeGitHubPayload(strings.TrimSpace(webhook.EventName), webhook.PayloadJSON, webhook.ReceivedAt)
	if !ok || err != nil {
		return facts, ok, err
	}
	if facts.repositoryProviderID == "" {
		facts.repositoryProviderID = webhook.RepositoryProviderID
	}
	return facts, true, nil
}

func normalizeGitHubPayload(eventName string, payload []byte, receivedAt time.Time) (githubWebhookFacts, bool, error) {
	switch eventName {
	case "issues":
		return normalizeGitHubWorkItemWebhook[githubIssuesWebhookPayload](payload, receivedAt, githubIssueSource)
	case "pull_request":
		return normalizeGitHubWorkItemWebhook[githubPullRequestWebhookEnvelope](payload, receivedAt, githubPullRequestSource)
	case "issue_comment":
		var envelope githubIssueCommentWebhookEnvelope
		if err := decodeWebhookPayload(payload, &envelope); err != nil {
			return githubWebhookFacts{}, true, err
		}
		commentID := numberString(envelope.Comment.ID)
		if commentID == "" {
			return githubWebhookFacts{}, true, fmt.Errorf("github issue_comment webhook misses comment.id")
		}
		return githubWebhookFacts{
			eventType:            providerEventCommentSynced,
			aggregateType:        providerAggregateComment,
			aggregateID:          commentID,
			providerWorkItemID:   numberString(envelope.Issue.ID),
			providerCommentID:    commentID,
			kind:                 "comment",
			number:               envelope.Issue.Number,
			repositoryFullName:   strings.TrimSpace(envelope.Repository.FullName),
			repositoryProviderID: numberString(envelope.Repository.ID),
			occurredAt:           timeValue(envelope.Comment.UpdatedAt, receivedAt),
		}, true, nil
	case "pull_request_review":
		var envelope githubPullRequestReviewWebhookEnvelope
		if err := decodeWebhookPayload(payload, &envelope); err != nil {
			return githubWebhookFacts{}, true, err
		}
		return githubReviewFacts(envelope.Repository, envelope.PullRequest, envelope.Review.ID, envelope.Review.UpdatedAt, receivedAt, eventName)
	case "pull_request_review_comment":
		var envelope githubPullRequestReviewCommentWebhookEnvelope
		if err := decodeWebhookPayload(payload, &envelope); err != nil {
			return githubWebhookFacts{}, true, err
		}
		return githubReviewFacts(envelope.Repository, envelope.PullRequest, envelope.Comment.ID, envelope.Comment.UpdatedAt, receivedAt, eventName)
	default:
		return githubWebhookFacts{}, false, nil
	}
}

func normalizeGitHubWorkItemWebhook[T any](payload []byte, receivedAt time.Time, source func(T) githubWorkItemSource) (githubWebhookFacts, bool, error) {
	var envelope T
	if err := decodeWebhookPayload(payload, &envelope); err != nil {
		return githubWebhookFacts{}, true, err
	}
	item := source(envelope)
	return githubWorkItemFactsFromPayload(item.repository, item.id, item.kind, item.number, item.updatedAt, receivedAt, item.missingMessage)
}

func githubIssueSource(envelope githubIssuesWebhookPayload) githubWorkItemSource {
	return githubWorkItemSource{
		repository:     envelope.Repository,
		id:             envelope.Issue.ID,
		kind:           "issue",
		number:         envelope.Issue.Number,
		updatedAt:      envelope.Issue.UpdatedAt,
		missingMessage: "github issues webhook misses issue.id",
	}
}

func githubPullRequestSource(envelope githubPullRequestWebhookEnvelope) githubWorkItemSource {
	return githubWorkItemSource{
		repository:     envelope.Repository,
		id:             envelope.PullRequest.ID,
		kind:           "pull_request",
		number:         envelope.PullRequest.Number,
		updatedAt:      envelope.PullRequest.UpdatedAt,
		missingMessage: "github pull_request webhook misses pull_request.id",
	}
}

func githubWorkItemFactsFromPayload(repository githubRepositoryWebhookPayload, id json.Number, kind string, number int64, updatedAt string, receivedAt time.Time, missingMessage string) (githubWebhookFacts, bool, error) {
	workItemID := numberString(id)
	if workItemID == "" {
		return githubWebhookFacts{}, true, fmt.Errorf("%s", missingMessage)
	}
	return githubWorkItemFacts(repository, workItemID, kind, number, updatedAt, receivedAt), true, nil
}

func githubWorkItemFacts(repository githubRepositoryWebhookPayload, workItemID string, kind string, number int64, updatedAt string, receivedAt time.Time) githubWebhookFacts {
	return githubWebhookFacts{
		eventType:            providerEventWorkItemSynced,
		aggregateType:        providerAggregateWorkItem,
		aggregateID:          workItemID,
		providerWorkItemID:   workItemID,
		kind:                 kind,
		number:               number,
		repositoryFullName:   strings.TrimSpace(repository.FullName),
		repositoryProviderID: numberString(repository.ID),
		occurredAt:           timeValue(updatedAt, receivedAt),
	}
}

func githubReviewFacts(repository githubRepositoryWebhookPayload, pullRequest githubPullRequestWebhookPayload, reviewID json.Number, updatedAt string, receivedAt time.Time, eventName string) (githubWebhookFacts, bool, error) {
	commentID := numberString(reviewID)
	if commentID == "" {
		return githubWebhookFacts{}, true, fmt.Errorf("github %s webhook misses review/comment id", eventName)
	}
	return githubWebhookFacts{
		eventType:            providerEventCommentSynced,
		aggregateType:        providerAggregateComment,
		aggregateID:          commentID,
		providerWorkItemID:   numberString(pullRequest.ID),
		providerCommentID:    commentID,
		kind:                 "review",
		number:               pullRequest.Number,
		repositoryFullName:   strings.TrimSpace(repository.FullName),
		repositoryProviderID: numberString(repository.ID),
		occurredAt:           timeValue(updatedAt, receivedAt),
	}, true, nil
}

func marshalProviderEventPayload(payload value.ProviderEventPayload) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func canonicalJSONObject(raw []byte) ([]byte, error) {
	if err := validateJSONObject(raw); err != nil {
		return nil, err
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return compacted.Bytes(), nil
}

func validateJSONObject(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return errs.ErrInvalidArgument
	}
	if !json.Valid(trimmed) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func decodeWebhookPayload[T any](raw []byte, payload *T) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(payload); err != nil {
		return errs.ErrInvalidArgument
	}
	return nil
}

func numberString(value json.Number) string {
	text := strings.TrimSpace(value.String())
	if text == "" {
		return ""
	}
	number, err := value.Int64()
	if err != nil || number <= 0 {
		return ""
	}
	return text
}

func timeValue(text string, fallback time.Time) time.Time {
	text = strings.TrimSpace(text)
	if text == "" {
		return fallback.UTC()
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return fallback.UTC()
	}
	return parsed.UTC()
}
