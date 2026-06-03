package github

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
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

type webhookRepositoryRefPayload struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
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
	ID             json.Number                 `json:"id"`
	Number         int64                       `json:"number"`
	HTMLURL        string                      `json:"html_url"`
	Title          string                      `json:"title"`
	State          string                      `json:"state"`
	Body           string                      `json:"body"`
	Labels         []webhookLabelPayload       `json:"labels"`
	Assignees      []webhookUserPayload        `json:"assignees"`
	Milestone      *webhookMilestonePayload    `json:"milestone"`
	Merged         bool                        `json:"merged"`
	MergeCommitSHA string                      `json:"merge_commit_sha"`
	Base           webhookRepositoryRefPayload `json:"base"`
	Head           webhookRepositoryRefPayload `json:"head"`
	MergedAt       string                      `json:"merged_at"`
	ClosedAt       string                      `json:"closed_at"`
	UpdatedAt      string                      `json:"updated_at"`
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
	Action      string                    `json:"action"`
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

type pushWebhookEnvelope struct {
	Ref        string                   `json:"ref"`
	Before     string                   `json:"before"`
	After      string                   `json:"after"`
	Repository webhookRepositoryPayload `json:"repository"`
	HeadCommit pushCommitPayload        `json:"head_commit"`
	Commits    []pushCommitPayload      `json:"commits"`
}

type pushCommitPayload struct {
	ID        string   `json:"id"`
	Timestamp string   `json:"timestamp"`
	Added     []string `json:"added"`
	Modified  []string `json:"modified"`
	Removed   []string `json:"removed"`
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

const (
	maxRepositoryChangePaths     = 512
	maxRepositoryChangePathBytes = 512
)

// NormalizeWebhook maps GitHub webhook payloads to provider-neutral facts.
func (a *Adapter) NormalizeWebhook(webhook entity.WebhookEvent) (value.ProviderWebhookFacts, bool, error) {
	if webhook.ProviderSlug != enum.ProviderSlugGitHub {
		return value.ProviderWebhookFacts{}, false, nil
	}
	return normalizeWebhookPayload(strings.TrimSpace(webhook.EventName), webhook.PayloadJSON, webhook.ReceivedAt)
}

func normalizeWebhookPayload(eventName string, payload []byte, receivedAt time.Time) (value.ProviderWebhookFacts, bool, error) {
	switch eventName {
	case "push":
		return normalizePushWebhook(payload, receivedAt)
	case "issues":
		return normalizeWorkItemWebhook[issuesWebhookEnvelope](payload, receivedAt, issueSource)
	case "pull_request":
		return normalizePullRequestWebhook(payload, receivedAt)
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

func normalizePullRequestWebhook(payload []byte, receivedAt time.Time) (value.ProviderWebhookFacts, bool, error) {
	var envelope pullRequestWebhookEnvelope
	if err := decodeProviderPayload(payload, &envelope); err != nil {
		return webhookFacts{}, true, err
	}
	item := pullRequestSource(envelope)
	facts, ok, err := workItemFactsFromPayload(item.repository, item.item, item.kind, receivedAt, item.missingMessage)
	if err != nil || !ok {
		return facts, ok, err
	}
	if pullRequestMerged(envelope) {
		facts.MergeSignal = pullRequestMergeSignal(envelope.PullRequest, receivedAt)
		facts.RepositoryChange = pullRequestRepositoryChangeSignal(envelope.Repository, envelope.PullRequest, receivedAt)
	}
	return facts, true, nil
}

func normalizePushWebhook(payload []byte, receivedAt time.Time) (value.ProviderWebhookFacts, bool, error) {
	var envelope pushWebhookEnvelope
	if err := decodeProviderPayload(payload, &envelope); err != nil {
		return webhookFacts{}, true, err
	}
	repositoryFullName := strings.TrimSpace(envelope.Repository.FullName)
	providerRepositoryID := numberString(envelope.Repository.ID)
	ref := strings.TrimSpace(envelope.Ref)
	baseBranch := branchNameFromRef(ref)
	commitSHA := strings.TrimSpace(envelope.After)
	if repositoryFullName == "" || providerRepositoryID == "" || ref == "" || baseBranch == "" || commitSHA == "" || allZeroSHA(commitSHA) {
		return webhookFacts{}, true, fmt.Errorf("github push webhook misses repository ref or commit")
	}
	observedAt := timeValue(envelope.HeadCommit.Timestamp, receivedAt)
	change := pushRepositoryChangeSignal(repositoryFullName, providerRepositoryID, ref, baseBranch, commitSHA, strings.TrimSpace(envelope.Before), envelope.Commits, observedAt)
	return webhookFacts{
		FactKind:             value.ProviderWebhookFactKindRepositoryChange,
		Kind:                 "push",
		RepositoryFullName:   repositoryFullName,
		RepositoryProviderID: providerRepositoryID,
		OccurredAt:           observedAt,
		RepositoryChange:     &change,
	}, true, nil
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

func pullRequestMerged(envelope pullRequestWebhookEnvelope) bool {
	return strings.EqualFold(strings.TrimSpace(envelope.Action), "closed") && envelope.PullRequest.Merged
}

func pullRequestMergeSignal(pullRequest pullRequestWebhookPayload, fallback time.Time) *value.ProviderRepositoryMergeSignalSnapshot {
	mergedAt := timeValue(pullRequest.MergedAt, fallback)
	if strings.TrimSpace(pullRequest.MergedAt) == "" {
		mergedAt = timeValue(pullRequest.ClosedAt, fallback)
	}
	return &value.ProviderRepositoryMergeSignalSnapshot{
		PullRequestProviderID: numberString(pullRequest.ID),
		PullRequestURL:        strings.TrimSpace(pullRequest.HTMLURL),
		BaseBranch:            strings.TrimSpace(pullRequest.Base.Ref),
		HeadBranch:            strings.TrimSpace(pullRequest.Head.Ref),
		MergeCommitSHA:        strings.TrimSpace(pullRequest.MergeCommitSHA),
		SourceRef:             strings.TrimSpace(pullRequest.Head.Ref),
		MergedAt:              mergedAt,
	}
}

func pullRequestRepositoryChangeSignal(repository webhookRepositoryPayload, pullRequest pullRequestWebhookPayload, fallback time.Time) *value.ProviderRepositoryChangeSignalSnapshot {
	mergedAt := timeValue(pullRequest.MergedAt, fallback)
	if strings.TrimSpace(pullRequest.MergedAt) == "" {
		mergedAt = timeValue(pullRequest.ClosedAt, fallback)
	}
	repositoryFullName := strings.TrimSpace(repository.FullName)
	providerRepositoryID := numberString(repository.ID)
	baseBranch := strings.TrimSpace(pullRequest.Base.Ref)
	commitSHA := strings.TrimSpace(pullRequest.MergeCommitSHA)
	signalKey := repositoryChangeSignalKey(enum.ProviderSlugGitHub, "pull_request_merged", repositoryFullName, baseBranch, commitSHA, pullRequest.Number)
	fingerprint := repositoryChangeFingerprint(
		"pull_request_merged",
		repositoryFullName,
		providerRepositoryID,
		baseBranch,
		commitSHA,
		"",
		"",
		"",
		false,
		false,
		pullRequest.Number,
	)
	return &value.ProviderRepositoryChangeSignalSnapshot{
		SignalKey:             signalKey,
		EventKind:             "pull_request_merged",
		RepositoryFullName:    repositoryFullName,
		ProviderRepositoryID:  providerRepositoryID,
		Ref:                   "refs/heads/" + baseBranch,
		BaseBranch:            baseBranch,
		CommitSHA:             commitSHA,
		SourceRef:             strings.TrimSpace(pullRequest.Head.Ref),
		PullRequestNumber:     pullRequest.Number,
		PullRequestProviderID: numberString(pullRequest.ID),
		PullRequestURL:        strings.TrimSpace(pullRequest.HTMLURL),
		PathSummaryStatus:     "unavailable",
		PathDigest:            emptyRepositoryChangePathDigest(),
		ChangeFingerprint:     fingerprint,
		ObservedAt:            mergedAt,
	}
}

func pushRepositoryChangeSignal(
	repositoryFullName string,
	providerRepositoryID string,
	ref string,
	baseBranch string,
	commitSHA string,
	beforeSHA string,
	commits []pushCommitPayload,
	observedAt time.Time,
) value.ProviderRepositoryChangeSignalSnapshot {
	summary := repositoryPathSummary(commits)
	pathSummaryStatus := "ready"
	if summary.Truncated {
		pathSummaryStatus = "truncated"
	} else if summary.PathCount == 0 {
		pathSummaryStatus = "unavailable"
	}
	signalKey := repositoryChangeSignalKey(enum.ProviderSlugGitHub, "push", repositoryFullName, baseBranch, commitSHA, 0)
	fingerprint := repositoryChangeFingerprint(
		"push",
		repositoryFullName,
		providerRepositoryID,
		baseBranch,
		commitSHA,
		beforeSHA,
		summary.PathDigest,
		formatRepositoryChangeCategories(summary.Categories),
		summary.ServicesPolicyChanged,
		summary.DeployRelevantChanged,
		0,
	)
	return value.ProviderRepositoryChangeSignalSnapshot{
		SignalKey:             signalKey,
		EventKind:             "push",
		RepositoryFullName:    repositoryFullName,
		ProviderRepositoryID:  providerRepositoryID,
		Ref:                   ref,
		BaseBranch:            baseBranch,
		CommitSHA:             commitSHA,
		BeforeSHA:             beforeSHA,
		PathSummaryStatus:     pathSummaryStatus,
		ChangedPathCount:      summary.PathCount,
		PathDigest:            summary.PathDigest,
		PathCategories:        summary.Categories,
		ServicesPolicyChanged: summary.ServicesPolicyChanged,
		DeployRelevantChanged: summary.DeployRelevantChanged,
		ChangeFingerprint:     fingerprint,
		ObservedAt:            observedAt,
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

type repositoryPathSummaryResult struct {
	PathCount             int64
	PathDigest            string
	Categories            []value.ProviderRepositoryChangePathCategoryCount
	ServicesPolicyChanged bool
	DeployRelevantChanged bool
	Truncated             bool
}

func repositoryPathSummary(commits []pushCommitPayload) repositoryPathSummaryResult {
	accumulator := repositoryPathAccumulator{changes: map[string]string{}}
	for _, commit := range commits {
		accumulator.record(commit.Added, "added")
		if accumulator.truncated {
			break
		}
		accumulator.record(commit.Modified, "modified")
		if accumulator.truncated {
			break
		}
		accumulator.record(commit.Removed, "removed")
		if accumulator.truncated {
			break
		}
	}
	keys := make([]string, 0, len(accumulator.changes))
	for path := range accumulator.changes {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return repositoryPathSummaryResult{PathDigest: emptyRepositoryChangePathDigest(), Truncated: accumulator.truncated}
	}
	counts := map[string]int64{}
	hash := sha256.New()
	for _, path := range keys {
		action := accumulator.changes[path]
		category := repositoryChangePathCategory(path)
		counts[category]++
		_, _ = hash.Write([]byte(path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(action))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(category))
		_, _ = hash.Write([]byte{0})
	}
	categories := make([]value.ProviderRepositoryChangePathCategoryCount, 0, len(counts))
	for category, count := range counts {
		categories = append(categories, value.ProviderRepositoryChangePathCategoryCount{Category: category, Count: count})
	}
	sort.Slice(categories, func(left, right int) bool {
		return categories[left].Category < categories[right].Category
	})
	servicesChanged := counts[string(enum.RepositoryChangePathCategoryServicesPolicy)] > 0
	return repositoryPathSummaryResult{
		PathCount:             int64(len(keys)),
		PathDigest:            "sha256:" + hex.EncodeToString(hash.Sum(nil)),
		Categories:            categories,
		ServicesPolicyChanged: servicesChanged,
		DeployRelevantChanged: servicesChanged || repositoryChangeDeployRelevant(counts),
		Truncated:             accumulator.truncated,
	}
}

type repositoryPathAccumulator struct {
	changes   map[string]string
	truncated bool
}

func (a *repositoryPathAccumulator) record(paths []string, action string) {
	for _, path := range paths {
		path, truncated := normalizeRepositoryPath(path)
		if truncated {
			a.truncated = true
			return
		}
		if path == "" {
			continue
		}
		if len(a.changes) >= maxRepositoryChangePaths {
			if _, ok := a.changes[path]; !ok {
				a.truncated = true
				return
			}
		}
		a.changes[path] = action
	}
}

func normalizeRepositoryPath(path string) (string, bool) {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimPrefix(path, "./")
	if len(path) > maxRepositoryChangePathBytes {
		return "", true
	}
	if path == "" ||
		strings.HasPrefix(path, "/") ||
		strings.Contains(path, "\x00") ||
		strings.Contains(path, "../") ||
		strings.HasPrefix(path, "..") {
		return "", false
	}
	return path, false
}

func repositoryChangePathCategory(path string) string {
	lower := strings.ToLower(strings.TrimSpace(path))
	switch {
	case lower == "services.yaml":
		return string(enum.RepositoryChangePathCategoryServicesPolicy)
	case strings.HasPrefix(lower, "deploy/") ||
		strings.HasPrefix(lower, "k8s/") ||
		strings.HasPrefix(lower, "kubernetes/") ||
		strings.Contains(lower, "/kustomization.yaml"):
		return string(enum.RepositoryChangePathCategoryDeployManifest)
	case strings.HasPrefix(lower, "services/") && repositoryConfigPath(lower):
		return string(enum.RepositoryChangePathCategoryServiceConfig)
	case strings.HasPrefix(lower, "services/") ||
		strings.HasPrefix(lower, "libs/") ||
		strings.HasPrefix(lower, "cmd/"):
		return string(enum.RepositoryChangePathCategoryServiceSource)
	case strings.HasPrefix(lower, "proto/") ||
		strings.HasPrefix(lower, "specs/") ||
		strings.HasPrefix(lower, "docs/design-guidelines/"):
		return string(enum.RepositoryChangePathCategoryPlatformPolicy)
	case strings.HasPrefix(lower, "bootstrap/") ||
		strings.HasPrefix(lower, "runtime/"):
		return string(enum.RepositoryChangePathCategoryRuntimeConfig)
	case strings.HasPrefix(lower, "docs/") ||
		lower == "readme.md" ||
		lower == "agents.md":
		return string(enum.RepositoryChangePathCategoryDocumentation)
	case strings.Contains(lower, "_test.") ||
		strings.HasPrefix(lower, "test/") ||
		strings.HasPrefix(lower, "tests/"):
		return string(enum.RepositoryChangePathCategoryTest)
	default:
		return string(enum.RepositoryChangePathCategoryOther)
	}
}

func repositoryConfigPath(path string) bool {
	for _, suffix := range []string{".yaml", ".yml", ".json", ".toml", ".env.example"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func repositoryChangeDeployRelevant(counts map[string]int64) bool {
	for _, category := range []enum.RepositoryChangePathCategory{
		enum.RepositoryChangePathCategoryServiceSource,
		enum.RepositoryChangePathCategoryServiceConfig,
		enum.RepositoryChangePathCategoryDeployManifest,
		enum.RepositoryChangePathCategoryRuntimeConfig,
		enum.RepositoryChangePathCategoryPlatformPolicy,
	} {
		if counts[string(category)] > 0 {
			return true
		}
	}
	return false
}

func formatRepositoryChangeCategories(categories []value.ProviderRepositoryChangePathCategoryCount) string {
	if len(categories) == 0 {
		return ""
	}
	parts := make([]string, 0, len(categories))
	for _, category := range categories {
		parts = append(parts, fmt.Sprintf("%s:%d", category.Category, category.Count))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func emptyRepositoryChangePathDigest() string {
	digest := sha256.Sum256(nil)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func repositoryChangeSignalKey(providerSlug enum.ProviderSlug, kind string, repositoryFullName string, baseBranch string, commitSHA string, pullRequestNumber int64) string {
	parts := []string{
		"provider",
		string(providerSlug),
		"repository_change",
		strings.TrimSpace(kind),
		strings.TrimSpace(repositoryFullName),
		strings.TrimSpace(baseBranch),
		strings.TrimSpace(commitSHA),
	}
	if pullRequestNumber > 0 {
		parts = append(parts, fmt.Sprintf("pull_request:%d", pullRequestNumber))
	}
	return strings.Join(parts, ":")
}

func repositoryChangeFingerprint(parts ...any) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(fmt.Sprint(part)))
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func branchNameFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if branch, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		return strings.TrimSpace(branch)
	}
	return ref
}

func allZeroSHA(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && strings.Trim(value, "0") == ""
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
