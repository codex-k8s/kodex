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

type webhookLabelPayload struct {
	Name string `json:"name"`
}

type webhookUserPayload struct {
	Login string `json:"login"`
}

type webhookMilestonePayload struct {
	Title string `json:"title"`
}

type issueWebhookPayload struct {
	ID          json.Number              `json:"id"`
	Number      int64                    `json:"number"`
	HTMLURL     string                   `json:"html_url"`
	Title       string                   `json:"title"`
	State       string                   `json:"state"`
	Body        string                   `json:"body"`
	Labels      []webhookLabelPayload    `json:"labels"`
	Assignees   []webhookUserPayload     `json:"assignees"`
	Milestone   *webhookMilestonePayload `json:"milestone"`
	UpdatedAt   string                   `json:"updated_at"`
	PullRequest *struct {
		HTMLURL string `json:"html_url"`
		URL     string `json:"url"`
	} `json:"pull_request"`
}

type pullRequestWebhookPayload struct {
	ID        json.Number              `json:"id"`
	Number    int64                    `json:"number"`
	HTMLURL   string                   `json:"html_url"`
	Title     string                   `json:"title"`
	State     string                   `json:"state"`
	Body      string                   `json:"body"`
	Labels    []webhookLabelPayload    `json:"labels"`
	Assignees []webhookUserPayload     `json:"assignees"`
	Milestone *webhookMilestonePayload `json:"milestone"`
	Merged    bool                     `json:"merged"`
	UpdatedAt string                   `json:"updated_at"`
}

type commentWebhookPayload struct {
	ID        json.Number        `json:"id"`
	Body      string             `json:"body"`
	User      webhookUserPayload `json:"user"`
	CreatedAt string             `json:"created_at"`
	UpdatedAt string             `json:"updated_at"`
}

type reviewWebhookPayload struct {
	ID          json.Number        `json:"id"`
	Body        string             `json:"body"`
	State       string             `json:"state"`
	User        webhookUserPayload `json:"user"`
	SubmittedAt string             `json:"submitted_at"`
	UpdatedAt   string             `json:"updated_at"`
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
	item           providerWorkItemPayload
	kind           string
	missingMessage string
}

type providerWorkItemPayload struct {
	id        json.Number
	number    int64
	htmlURL   string
	title     string
	state     string
	body      string
	labels    []webhookLabelPayload
	assignees []webhookUserPayload
	milestone *webhookMilestonePayload
	updatedAt string
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
		kind := issueCommentWorkItemKind(envelope.Issue)
		workItemID := githubWorkItemRef(envelope.Repository.FullName, kind, envelope.Issue.Number)
		if workItemID == "" {
			return webhookFacts{}, true, fmt.Errorf("github issue_comment webhook misses stable work item ref")
		}
		return webhookFacts{
			FactKind:             value.ProviderWebhookFactKindComment,
			ProviderWorkItemID:   workItemID,
			ProviderCommentID:    commentID,
			Kind:                 "comment",
			Number:               envelope.Issue.Number,
			RepositoryFullName:   strings.TrimSpace(envelope.Repository.FullName),
			RepositoryProviderID: numberString(envelope.Repository.ID),
			OccurredAt:           timeValue(envelope.Comment.UpdatedAt, receivedAt),
			WorkItem:             issueSnapshot(envelope.Repository, envelope.Issue, kind, workItemID, receivedAt),
			Comment:              commentSnapshot("comment", workItemID, envelope.Comment, receivedAt),
		}, true, nil
	case "pull_request_review":
		var envelope pullRequestReviewWebhookEnvelope
		if err := decodeProviderPayload(payload, &envelope); err != nil {
			return webhookFacts{}, true, err
		}
		return reviewFacts(envelope.Repository, envelope.PullRequest, reviewCommentSnapshot(numberString(envelope.PullRequest.ID), envelope.Review, receivedAt), receivedAt, eventName)
	case "pull_request_review_comment":
		var envelope pullRequestReviewCommentWebhookEnvelope
		if err := decodeProviderPayload(payload, &envelope); err != nil {
			return webhookFacts{}, true, err
		}
		return reviewFacts(envelope.Repository, envelope.PullRequest, commentSnapshot("review", numberString(envelope.PullRequest.ID), envelope.Comment, receivedAt), receivedAt, eventName)
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
	return workItemFactsFromPayload(item.repository, item.item, item.kind, receivedAt, item.missingMessage)
}

func issueSource(envelope issuesWebhookEnvelope) workItemSource {
	return workItemSource{
		repository:     envelope.Repository,
		item:           issueWorkItem(envelope.Issue),
		kind:           "issue",
		missingMessage: "github issues webhook misses issue.id",
	}
}

func pullRequestSource(envelope pullRequestWebhookEnvelope) workItemSource {
	return workItemSource{
		repository:     envelope.Repository,
		item:           pullRequestWorkItem(envelope.PullRequest),
		kind:           "pull_request",
		missingMessage: "github pull_request webhook misses pull_request.id",
	}
}

func workItemFactsFromPayload(repository webhookRepositoryPayload, item providerWorkItemPayload, kind string, receivedAt time.Time, missingMessage string) (value.ProviderWebhookFacts, bool, error) {
	if numberString(item.id) == "" {
		return webhookFacts{}, true, fmt.Errorf("%s", missingMessage)
	}
	workItemID := githubWorkItemRef(repository.FullName, kind, item.number)
	if workItemID == "" {
		return webhookFacts{}, true, fmt.Errorf("github %s webhook misses stable work item ref", kind)
	}
	snapshot := workItemSnapshot(repository, item, kind, workItemID, receivedAt)
	return workItemFacts(repository, workItemID, kind, item.number, item.updatedAt, receivedAt, &snapshot), true, nil
}

func workItemFacts(repository webhookRepositoryPayload, workItemID string, kind string, number int64, updatedAt string, receivedAt time.Time, snapshot *value.ProviderWorkItemSnapshot) value.ProviderWebhookFacts {
	return webhookFacts{
		FactKind:             value.ProviderWebhookFactKindWorkItem,
		ProviderWorkItemID:   workItemID,
		Kind:                 kind,
		Number:               number,
		RepositoryFullName:   strings.TrimSpace(repository.FullName),
		RepositoryProviderID: numberString(repository.ID),
		OccurredAt:           timeValue(updatedAt, receivedAt),
		WorkItem:             snapshot,
	}
}

func reviewFacts(repository webhookRepositoryPayload, pullRequest pullRequestWebhookPayload, comment *value.ProviderCommentSnapshot, receivedAt time.Time, eventName string) (value.ProviderWebhookFacts, bool, error) {
	if comment == nil {
		return webhookFacts{}, true, fmt.Errorf("github %s webhook misses review/comment payload", eventName)
	}
	commentID := strings.TrimSpace(comment.ProviderCommentID)
	if commentID == "" {
		return webhookFacts{}, true, fmt.Errorf("github %s webhook misses review/comment id", eventName)
	}
	occurredAt := comment.ProviderUpdatedAt
	if occurredAt.IsZero() {
		occurredAt = receivedAt
	}
	workItemID := githubWorkItemRef(repository.FullName, "pull_request", pullRequest.Number)
	if workItemID == "" {
		return webhookFacts{}, true, fmt.Errorf("github %s webhook misses stable pull request ref", eventName)
	}
	comment.ProviderWorkItemID = workItemID
	return webhookFacts{
		FactKind:             value.ProviderWebhookFactKindComment,
		ProviderWorkItemID:   workItemID,
		ProviderCommentID:    commentID,
		Kind:                 "review",
		Number:               pullRequest.Number,
		RepositoryFullName:   strings.TrimSpace(repository.FullName),
		RepositoryProviderID: numberString(repository.ID),
		OccurredAt:           occurredAt,
		WorkItem:             pullRequestSnapshot(repository, pullRequest, workItemID, receivedAt),
		Comment:              comment,
	}, true, nil
}

func issueWorkItem(issue issueWebhookPayload) providerWorkItemPayload {
	return providerWorkItemPayload{
		id:        issue.ID,
		number:    issue.Number,
		htmlURL:   issue.HTMLURL,
		title:     issue.Title,
		state:     issue.State,
		body:      issue.Body,
		labels:    issue.Labels,
		assignees: issue.Assignees,
		milestone: issue.Milestone,
		updatedAt: issue.UpdatedAt,
	}
}

func pullRequestWorkItem(pullRequest pullRequestWebhookPayload) providerWorkItemPayload {
	state := strings.TrimSpace(pullRequest.State)
	if pullRequest.Merged {
		state = "merged"
	}
	return providerWorkItemPayload{
		id:        pullRequest.ID,
		number:    pullRequest.Number,
		htmlURL:   pullRequest.HTMLURL,
		title:     pullRequest.Title,
		state:     state,
		body:      pullRequest.Body,
		labels:    pullRequest.Labels,
		assignees: pullRequest.Assignees,
		milestone: pullRequest.Milestone,
		updatedAt: pullRequest.UpdatedAt,
	}
}

func issueSnapshot(repository webhookRepositoryPayload, issue issueWebhookPayload, kind string, workItemID string, fallback time.Time) *value.ProviderWorkItemSnapshot {
	snapshot := workItemSnapshot(repository, issueWorkItem(issue), kind, workItemID, fallback)
	return &snapshot
}

func pullRequestSnapshot(repository webhookRepositoryPayload, pullRequest pullRequestWebhookPayload, workItemID string, fallback time.Time) *value.ProviderWorkItemSnapshot {
	snapshot := workItemSnapshot(repository, pullRequestWorkItem(pullRequest), "pull_request", workItemID, fallback)
	return &snapshot
}

func workItemSnapshot(repository webhookRepositoryPayload, item providerWorkItemPayload, kind string, workItemID string, fallback time.Time) value.ProviderWorkItemSnapshot {
	return value.ProviderWorkItemSnapshot{
		ProviderSlug:       string(enum.ProviderSlugGitHub),
		ProviderWorkItemID: workItemID,
		RepositoryFullName: strings.TrimSpace(repository.FullName),
		Kind:               kind,
		Number:             item.number,
		URL:                strings.TrimSpace(item.htmlURL),
		Title:              strings.TrimSpace(item.title),
		State:              strings.TrimSpace(item.state),
		Body:               strings.TrimSpace(item.body),
		Labels:             labelNames(item.labels),
		Assignees:          assigneeLogins(item.assignees),
		Milestone:          milestoneTitle(item.milestone),
		ProviderUpdatedAt:  timeValue(item.updatedAt, fallback),
	}
}

func commentSnapshot(kind string, workItemID string, comment commentWebhookPayload, fallback time.Time) *value.ProviderCommentSnapshot {
	return &value.ProviderCommentSnapshot{
		ProviderSlug:       string(enum.ProviderSlugGitHub),
		ProviderCommentID:  numberString(comment.ID),
		ProviderWorkItemID: workItemID,
		Kind:               kind,
		AuthorLogin:        strings.TrimSpace(comment.User.Login),
		Body:               strings.TrimSpace(comment.Body),
		ProviderCreatedAt:  timeValue(comment.CreatedAt, fallback),
		ProviderUpdatedAt:  timeValue(comment.UpdatedAt, fallback),
	}
}

func reviewCommentSnapshot(workItemID string, review reviewWebhookPayload, fallback time.Time) *value.ProviderCommentSnapshot {
	createdAt := review.SubmittedAt
	if strings.TrimSpace(createdAt) == "" {
		createdAt = review.UpdatedAt
	}
	return &value.ProviderCommentSnapshot{
		ProviderSlug:       string(enum.ProviderSlugGitHub),
		ProviderCommentID:  numberString(review.ID),
		ProviderWorkItemID: workItemID,
		Kind:               "review",
		ReviewState:        normalizedReviewState(review.State),
		AuthorLogin:        strings.TrimSpace(review.User.Login),
		Body:               strings.TrimSpace(review.Body),
		ProviderCreatedAt:  timeValue(createdAt, fallback),
		ProviderUpdatedAt:  timeValue(review.UpdatedAt, fallback),
	}
}

func issueCommentWorkItemKind(issue issueWebhookPayload) string {
	if issue.PullRequest != nil {
		return "pull_request"
	}
	return "issue"
}

func githubWorkItemRef(repositoryFullName string, kind string, number int64) string {
	repositoryFullName = strings.TrimSpace(repositoryFullName)
	kind = strings.TrimSpace(kind)
	if repositoryFullName == "" || kind == "" || number <= 0 {
		return ""
	}
	return fmt.Sprintf("github:%s:%s:%d", repositoryFullName, kind, number)
}

func normalizedReviewState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "approved", "changes_requested", "commented", "dismissed", "pending":
		return strings.ToLower(strings.TrimSpace(state))
	default:
		return ""
	}
}

func labelNames(labels []webhookLabelPayload) []string {
	return collectTrimmed(labels, func(label webhookLabelPayload) string { return label.Name })
}

func assigneeLogins(users []webhookUserPayload) []string {
	return collectTrimmed(users, func(user webhookUserPayload) string { return user.Login })
}

func collectTrimmed[T any](items []T, value func(T) string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(value(item))
		if text != "" {
			result = append(result, text)
		}
	}
	return result
}

func milestoneTitle(milestone *webhookMilestonePayload) string {
	if milestone == nil {
		return ""
	}
	return strings.TrimSpace(milestone.Title)
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
