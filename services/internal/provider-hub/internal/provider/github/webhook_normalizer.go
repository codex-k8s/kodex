package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

var _ providerrepo.WebhookNormalizer = (*Adapter)(nil)

type webhookFacts = value.ProviderWebhookFacts

type webhookRepositoryPayload struct {
	ID       json.Number `json:"id"`
	FullName string      `json:"full_name"`
}

type issueWebhookPayload struct {
	ID        json.Number `json:"id"`
	Number    int64       `json:"number"`
	UpdatedAt string      `json:"updated_at"`
}

type pullRequestWebhookPayload struct {
	ID        json.Number `json:"id"`
	Number    int64       `json:"number"`
	UpdatedAt string      `json:"updated_at"`
}

type commentWebhookPayload struct {
	ID        json.Number `json:"id"`
	UpdatedAt string      `json:"updated_at"`
}

type reviewWebhookPayload struct {
	ID        json.Number `json:"id"`
	UpdatedAt string      `json:"updated_at"`
}

type issuesWebhookEnvelope struct {
	Repository webhookRepositoryPayload `json:"repository"`
	Issue      issueWebhookPayload      `json:"issue"`
}

type pullRequestWebhookEnvelope struct {
	Repository  webhookRepositoryPayload  `json:"repository"`
	PullRequest pullRequestWebhookPayload `json:"pull_request"`
}

type issueCommentWebhookEnvelope struct {
	Repository webhookRepositoryPayload `json:"repository"`
	Issue      issueWebhookPayload      `json:"issue"`
	Comment    commentWebhookPayload    `json:"comment"`
}

type pullRequestReviewWebhookEnvelope struct {
	Repository  webhookRepositoryPayload  `json:"repository"`
	PullRequest pullRequestWebhookPayload `json:"pull_request"`
	Review      reviewWebhookPayload      `json:"review"`
}

type pullRequestReviewCommentWebhookEnvelope struct {
	Repository  webhookRepositoryPayload  `json:"repository"`
	PullRequest pullRequestWebhookPayload `json:"pull_request"`
	Comment     commentWebhookPayload     `json:"comment"`
}

type workItemSource struct {
	repository     webhookRepositoryPayload
	id             json.Number
	kind           string
	number         int64
	updatedAt      string
	missingMessage string
}

// NormalizeWebhook maps GitHub webhook payloads to provider-neutral facts.
func (a *Adapter) NormalizeWebhook(webhook entity.WebhookEvent) (value.ProviderWebhookFacts, bool, error) {
	if webhook.ProviderSlug != enum.ProviderSlugGitHub {
		return value.ProviderWebhookFacts{}, false, nil
	}
	return normalizeWebhookPayload(strings.TrimSpace(webhook.EventName), webhook.PayloadJSON, webhook.ReceivedAt)
}

func normalizeWebhookPayload(eventName string, payload []byte, receivedAt time.Time) (value.ProviderWebhookFacts, bool, error) {
	switch eventName {
	case "issues":
		return normalizeWorkItemWebhook[issuesWebhookEnvelope](payload, receivedAt, issueSource)
	case "pull_request":
		return normalizeWorkItemWebhook[pullRequestWebhookEnvelope](payload, receivedAt, pullRequestSource)
	case "issue_comment":
		var envelope issueCommentWebhookEnvelope
		if err := decodeProviderPayload(payload, &envelope); err != nil {
			return webhookFacts{}, true, err
		}
		commentID := numberString(envelope.Comment.ID)
		if commentID == "" {
			return webhookFacts{}, true, fmt.Errorf("github issue_comment webhook misses comment.id")
		}
		return webhookFacts{
			FactKind:             value.ProviderWebhookFactKindComment,
			ProviderWorkItemID:   numberString(envelope.Issue.ID),
			ProviderCommentID:    commentID,
			Kind:                 "comment",
			Number:               envelope.Issue.Number,
			RepositoryFullName:   strings.TrimSpace(envelope.Repository.FullName),
			RepositoryProviderID: numberString(envelope.Repository.ID),
			OccurredAt:           timeValue(envelope.Comment.UpdatedAt, receivedAt),
		}, true, nil
	case "pull_request_review":
		var envelope pullRequestReviewWebhookEnvelope
		if err := decodeProviderPayload(payload, &envelope); err != nil {
			return webhookFacts{}, true, err
		}
		return reviewFacts(envelope.Repository, envelope.PullRequest, envelope.Review.ID, envelope.Review.UpdatedAt, receivedAt, eventName)
	case "pull_request_review_comment":
		var envelope pullRequestReviewCommentWebhookEnvelope
		if err := decodeProviderPayload(payload, &envelope); err != nil {
			return webhookFacts{}, true, err
		}
		return reviewFacts(envelope.Repository, envelope.PullRequest, envelope.Comment.ID, envelope.Comment.UpdatedAt, receivedAt, eventName)
	default:
		return webhookFacts{}, false, nil
	}
}

func normalizeWorkItemWebhook[T any](payload []byte, receivedAt time.Time, source func(T) workItemSource) (value.ProviderWebhookFacts, bool, error) {
	var envelope T
	if err := decodeProviderPayload(payload, &envelope); err != nil {
		return webhookFacts{}, true, err
	}
	item := source(envelope)
	return workItemFactsFromPayload(item.repository, item.id, item.kind, item.number, item.updatedAt, receivedAt, item.missingMessage)
}

func issueSource(envelope issuesWebhookEnvelope) workItemSource {
	return workItemSource{
		repository:     envelope.Repository,
		id:             envelope.Issue.ID,
		kind:           "issue",
		number:         envelope.Issue.Number,
		updatedAt:      envelope.Issue.UpdatedAt,
		missingMessage: "github issues webhook misses issue.id",
	}
}

func pullRequestSource(envelope pullRequestWebhookEnvelope) workItemSource {
	return workItemSource{
		repository:     envelope.Repository,
		id:             envelope.PullRequest.ID,
		kind:           "pull_request",
		number:         envelope.PullRequest.Number,
		updatedAt:      envelope.PullRequest.UpdatedAt,
		missingMessage: "github pull_request webhook misses pull_request.id",
	}
}

func workItemFactsFromPayload(repository webhookRepositoryPayload, id json.Number, kind string, number int64, updatedAt string, receivedAt time.Time, missingMessage string) (value.ProviderWebhookFacts, bool, error) {
	workItemID := numberString(id)
	if workItemID == "" {
		return webhookFacts{}, true, fmt.Errorf("%s", missingMessage)
	}
	return workItemFacts(repository, workItemID, kind, number, updatedAt, receivedAt), true, nil
}

func workItemFacts(repository webhookRepositoryPayload, workItemID string, kind string, number int64, updatedAt string, receivedAt time.Time) value.ProviderWebhookFacts {
	return webhookFacts{
		FactKind:             value.ProviderWebhookFactKindWorkItem,
		ProviderWorkItemID:   workItemID,
		Kind:                 kind,
		Number:               number,
		RepositoryFullName:   strings.TrimSpace(repository.FullName),
		RepositoryProviderID: numberString(repository.ID),
		OccurredAt:           timeValue(updatedAt, receivedAt),
	}
}

func reviewFacts(repository webhookRepositoryPayload, pullRequest pullRequestWebhookPayload, reviewID json.Number, updatedAt string, receivedAt time.Time, eventName string) (value.ProviderWebhookFacts, bool, error) {
	commentID := numberString(reviewID)
	if commentID == "" {
		return webhookFacts{}, true, fmt.Errorf("github %s webhook misses review/comment id", eventName)
	}
	return webhookFacts{
		FactKind:             value.ProviderWebhookFactKindComment,
		ProviderWorkItemID:   numberString(pullRequest.ID),
		ProviderCommentID:    commentID,
		Kind:                 "review",
		Number:               pullRequest.Number,
		RepositoryFullName:   strings.TrimSpace(repository.FullName),
		RepositoryProviderID: numberString(repository.ID),
		OccurredAt:           timeValue(updatedAt, receivedAt),
	}, true, nil
}

func decodeProviderPayload[T any](raw []byte, payload *T) error {
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
