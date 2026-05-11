package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	githubapi "github.com/google/go-github/v82/github"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
)

const githubAPIPerPage = 50

type workItemScope struct {
	owner  string
	repo   string
	kind   enum.WorkItemKind
	number int
}

func (a *Adapter) reconcileWorkItem(ctx context.Context, client *githubapi.Client, request providerclient.ReconciliationRequest, observedAt time.Time) (providerclient.ReconciliationResult, error) {
	target, err := parseWorkItemScope(request.Cursor.ScopeRef)
	if err != nil {
		return providerclient.ReconciliationResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	result := providerclient.ReconciliationResult{}
	switch request.Cursor.ArtifactKind {
	case enum.SyncArtifactIssue:
		item, _, err := client.Issues.Get(ctx, target.owner, target.repo, target.number)
		if err != nil {
			return providerclient.ReconciliationResult{}, classifyGitHubError(err)
		}
		if item.IsPullRequest() {
			return resultWithCursor(result, observedAt), nil
		}
		result.WorkItems = append(result.WorkItems, issueAPISnapshot(target.repository(), item, enum.WorkItemKindIssue))
	case enum.SyncArtifactPullRequest:
		item, _, err := client.PullRequests.Get(ctx, target.owner, target.repo, target.number)
		if err != nil {
			return providerclient.ReconciliationResult{}, classifyGitHubError(err)
		}
		result.WorkItems = append(result.WorkItems, pullRequestAPISnapshot(target.repository(), item))
	case enum.SyncArtifactComment:
		workItem, comments, err := a.fetchWorkItemComments(ctx, client, target, request.Cursor.CursorValue, int(request.MaxItems))
		if err != nil {
			return providerclient.ReconciliationResult{}, err
		}
		if workItem.ProviderWorkItemID != "" {
			result.WorkItems = append(result.WorkItems, workItem)
		}
		result.Comments = append(result.Comments, comments...)
	case enum.SyncArtifactRelationship:
		workItem, err := a.fetchWorkItemSnapshot(ctx, client, target)
		if err != nil {
			return providerclient.ReconciliationResult{}, err
		}
		if workItem.ProviderWorkItemID != "" {
			result.WorkItems = append(result.WorkItems, workItem)
		}
	default:
		return providerclient.ReconciliationResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	return resultWithCursor(result, observedAt), nil
}

func (a *Adapter) reconcileRepository(ctx context.Context, client *githubapi.Client, request providerclient.ReconciliationRequest, observedAt time.Time) (providerclient.ReconciliationResult, error) {
	owner, repo, err := parseRepositoryRef(request.Cursor.ScopeRef)
	if err != nil {
		return providerclient.ReconciliationResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	limit := int(request.MaxItems)
	if limit <= 0 || limit > githubAPIPerPage {
		limit = githubAPIPerPage
	}
	// ProjectionUpdate currently commits one work item per cursor completion.
	// The worker will re-enter through the advanced cursor for the next item.
	if limit > 1 {
		limit = 1
	}
	switch request.Cursor.ArtifactKind {
	case enum.SyncArtifactIssue:
		return a.reconcileRepositoryIssues(ctx, client, owner, repo, request, observedAt, limit)
	case enum.SyncArtifactPullRequest:
		return a.reconcileRepositoryPullRequests(ctx, client, owner, repo, request, observedAt, limit)
	case enum.SyncArtifactRepository:
		_, response, err := client.Repositories.Get(ctx, owner, repo)
		if err != nil {
			return providerclient.ReconciliationResult{}, classifyGitHubError(err)
		}
		return resultWithCursor(providerclient.ReconciliationResult{
			LimitSnapshots:      a.limitSnapshots(request.Credential.ExternalAccountID, observedAt, rateLimitsFromResponse(response)),
			RateBudgetStateJSON: rateBudgetStateJSON(response),
		}, observedAt), nil
	default:
		return providerclient.ReconciliationResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
}

func (a *Adapter) reconcileRepositoryIssues(ctx context.Context, client *githubapi.Client, owner string, repo string, request providerclient.ReconciliationRequest, observedAt time.Time, limit int) (providerclient.ReconciliationResult, error) {
	since := cursorSince(request.Cursor)
	options := &githubapi.IssueListByRepoOptions{
		State:     "all",
		Sort:      "updated",
		Direction: "asc",
		Since:     since,
		ListOptions: githubapi.ListOptions{
			PerPage: limit,
		},
	}
	items, response, err := client.Issues.ListByRepo(ctx, owner, repo, options)
	if err != nil {
		return providerclient.ReconciliationResult{}, classifyGitHubError(err)
	}
	result := providerclient.ReconciliationResult{
		LimitSnapshots:      a.limitSnapshots(request.Credential.ExternalAccountID, observedAt, rateLimitsFromResponse(response)),
		RateBudgetStateJSON: rateBudgetStateJSON(response),
	}
	for _, item := range items {
		if item == nil || item.IsPullRequest() {
			continue
		}
		result.WorkItems = append(result.WorkItems, issueAPISnapshot(owner+"/"+repo, item, enum.WorkItemKindIssue))
		if len(result.WorkItems) >= limit {
			break
		}
	}
	return resultWithCursor(result, maxProviderUpdatedAt(result, observedAt)), nil
}

func (a *Adapter) reconcileRepositoryPullRequests(ctx context.Context, client *githubapi.Client, owner string, repo string, request providerclient.ReconciliationRequest, observedAt time.Time, limit int) (providerclient.ReconciliationResult, error) {
	since := cursorSince(request.Cursor)
	options := &githubapi.IssueListByRepoOptions{
		State:     "all",
		Sort:      "updated",
		Direction: "asc",
		ListOptions: githubapi.ListOptions{
			PerPage: limit,
		},
		Since: since,
	}
	items, response, err := client.Issues.ListByRepo(ctx, owner, repo, options)
	if err != nil {
		return providerclient.ReconciliationResult{}, classifyGitHubError(err)
	}
	result := providerclient.ReconciliationResult{
		LimitSnapshots:      a.limitSnapshots(request.Credential.ExternalAccountID, observedAt, rateLimitsFromResponse(response)),
		RateBudgetStateJSON: rateBudgetStateJSON(response),
	}
	for _, item := range items {
		if item == nil || !item.IsPullRequest() {
			continue
		}
		result.WorkItems = append(result.WorkItems, pullRequestIssueAPISnapshot(owner+"/"+repo, item))
		if len(result.WorkItems) >= limit {
			break
		}
	}
	return resultWithCursor(result, maxProviderUpdatedAt(result, observedAt)), nil
}

func (a *Adapter) fetchWorkItemComments(ctx context.Context, client *githubapi.Client, target workItemScope, cursorValue string, maxItems int) (value.ProviderWorkItemSnapshot, []value.ProviderCommentSnapshot, error) {
	workItem, err := a.fetchWorkItemSnapshot(ctx, client, target)
	if err != nil {
		return value.ProviderWorkItemSnapshot{}, nil, err
	}
	since := parseCursorTime(cursorValue)
	sortValue := "updated"
	directionValue := "asc"
	options := &githubapi.IssueListCommentsOptions{
		Sort:      &sortValue,
		Direction: &directionValue,
		Since:     &since,
		ListOptions: githubapi.ListOptions{
			PerPage: boundedPageSize(maxItems),
		},
	}
	items, _, err := client.Issues.ListComments(ctx, target.owner, target.repo, target.number, options)
	if err != nil {
		return value.ProviderWorkItemSnapshot{}, nil, classifyGitHubError(err)
	}
	comments := make([]value.ProviderCommentSnapshot, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		comments = append(comments, issueCommentSnapshot(workItem.ProviderWorkItemID, item))
		if len(comments) >= maxItems {
			return workItem, comments, nil
		}
	}
	if target.kind == enum.WorkItemKindPullRequest && len(comments) < maxItems {
		reviews, _, err := client.PullRequests.ListReviews(ctx, target.owner, target.repo, target.number, &githubapi.ListOptions{PerPage: boundedPageSize(maxItems - len(comments))})
		if err != nil {
			return value.ProviderWorkItemSnapshot{}, nil, classifyGitHubError(err)
		}
		for _, review := range reviews {
			if review == nil || !review.GetSubmittedAt().After(since) {
				continue
			}
			comments = append(comments, reviewSnapshot(workItem.ProviderWorkItemID, review))
			if len(comments) >= maxItems {
				break
			}
		}
	}
	return workItem, comments, nil
}

func (a *Adapter) fetchWorkItemSnapshot(ctx context.Context, client *githubapi.Client, target workItemScope) (value.ProviderWorkItemSnapshot, error) {
	switch target.kind {
	case enum.WorkItemKindIssue:
		item, _, err := client.Issues.Get(ctx, target.owner, target.repo, target.number)
		if err != nil {
			return value.ProviderWorkItemSnapshot{}, classifyGitHubError(err)
		}
		if item.IsPullRequest() {
			return value.ProviderWorkItemSnapshot{}, nil
		}
		return issueAPISnapshot(target.repository(), item, enum.WorkItemKindIssue), nil
	case enum.WorkItemKindPullRequest:
		item, _, err := client.PullRequests.Get(ctx, target.owner, target.repo, target.number)
		if err != nil {
			return value.ProviderWorkItemSnapshot{}, classifyGitHubError(err)
		}
		return pullRequestAPISnapshot(target.repository(), item), nil
	default:
		return value.ProviderWorkItemSnapshot{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
}

func parseWorkItemScope(scopeRef string) (workItemScope, error) {
	repository, rest, ok := strings.Cut(strings.TrimSpace(scopeRef), "#")
	if !ok {
		return workItemScope{}, providerclient.ErrUnsupported
	}
	owner, repo, err := parseRepositoryRef(repository)
	if err != nil {
		return workItemScope{}, err
	}
	kind, numberText, ok := strings.Cut(rest, ":")
	if !ok {
		return workItemScope{}, providerclient.ErrUnsupported
	}
	number, err := strconv.Atoi(strings.TrimSpace(numberText))
	if err != nil || number <= 0 {
		return workItemScope{}, providerclient.ErrUnsupported
	}
	target := workItemScope{owner: owner, repo: repo, number: number}
	switch strings.TrimSpace(kind) {
	case string(enum.WorkItemKindIssue), "number", "":
		target.kind = enum.WorkItemKindIssue
	case string(enum.WorkItemKindPullRequest):
		target.kind = enum.WorkItemKindPullRequest
	default:
		return workItemScope{}, providerclient.ErrUnsupported
	}
	return target, nil
}

func parseRepositoryRef(scopeRef string) (string, string, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(scopeRef), "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" || strings.Contains(repo, "/") {
		return "", "", providerclient.ErrUnsupported
	}
	return strings.TrimSpace(owner), strings.TrimSpace(repo), nil
}

func (s workItemScope) repository() string {
	return s.owner + "/" + s.repo
}

func issueAPISnapshot(repository string, issue *githubapi.Issue, kind enum.WorkItemKind) value.ProviderWorkItemSnapshot {
	labels := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		if label != nil && strings.TrimSpace(label.GetName()) != "" {
			labels = append(labels, label.GetName())
		}
	}
	assignees := make([]string, 0, len(issue.Assignees))
	for _, assignee := range issue.Assignees {
		if assignee != nil && strings.TrimSpace(assignee.GetLogin()) != "" {
			assignees = append(assignees, assignee.GetLogin())
		}
	}
	milestone := ""
	if issue.GetMilestone() != nil {
		milestone = issue.GetMilestone().GetTitle()
	}
	number := issue.GetNumber()
	return value.ProviderWorkItemSnapshot{
		ProviderSlug:       string(enum.ProviderSlugGitHub),
		ProviderWorkItemID: providerWorkItemID(repository, kind, number),
		RepositoryFullName: repository,
		Kind:               string(kind),
		Number:             int64(number),
		URL:                issue.GetHTMLURL(),
		Title:              issue.GetTitle(),
		State:              issue.GetState(),
		Body:               issue.GetBody(),
		Labels:             labels,
		Assignees:          assignees,
		Milestone:          milestone,
		ProviderUpdatedAt:  issue.GetUpdatedAt().Time,
	}
}

func pullRequestAPISnapshot(repository string, pullRequest *githubapi.PullRequest) value.ProviderWorkItemSnapshot {
	labels := make([]string, 0, len(pullRequest.Labels))
	for _, label := range pullRequest.Labels {
		if label != nil && strings.TrimSpace(label.GetName()) != "" {
			labels = append(labels, label.GetName())
		}
	}
	assignees := make([]string, 0, len(pullRequest.Assignees))
	for _, assignee := range pullRequest.Assignees {
		if assignee != nil && strings.TrimSpace(assignee.GetLogin()) != "" {
			assignees = append(assignees, assignee.GetLogin())
		}
	}
	milestone := ""
	if pullRequest.GetMilestone() != nil {
		milestone = pullRequest.GetMilestone().GetTitle()
	}
	number := pullRequest.GetNumber()
	return value.ProviderWorkItemSnapshot{
		ProviderSlug:       string(enum.ProviderSlugGitHub),
		ProviderWorkItemID: providerWorkItemID(repository, enum.WorkItemKindPullRequest, number),
		RepositoryFullName: repository,
		Kind:               string(enum.WorkItemKindPullRequest),
		Number:             int64(number),
		URL:                pullRequest.GetHTMLURL(),
		Title:              pullRequest.GetTitle(),
		State:              pullRequest.GetState(),
		Body:               pullRequest.GetBody(),
		Labels:             labels,
		Assignees:          assignees,
		Milestone:          milestone,
		ProviderUpdatedAt:  pullRequest.GetUpdatedAt().Time,
	}
}

func pullRequestIssueAPISnapshot(repository string, issue *githubapi.Issue) value.ProviderWorkItemSnapshot {
	snapshot := issueAPISnapshot(repository, issue, enum.WorkItemKindPullRequest)
	snapshot.ProviderWorkItemID = providerWorkItemID(repository, enum.WorkItemKindPullRequest, int(snapshot.Number))
	snapshot.Kind = string(enum.WorkItemKindPullRequest)
	return snapshot
}

func issueCommentSnapshot(workItemID string, comment *githubapi.IssueComment) value.ProviderCommentSnapshot {
	return value.ProviderCommentSnapshot{
		ProviderSlug:       string(enum.ProviderSlugGitHub),
		ProviderCommentID:  strconv.FormatInt(comment.GetID(), 10),
		ProviderWorkItemID: workItemID,
		Kind:               string(enum.CommentKindComment),
		AuthorLogin:        userLogin(comment.GetUser()),
		Body:               comment.GetBody(),
		ProviderCreatedAt:  comment.GetCreatedAt().Time,
		ProviderUpdatedAt:  comment.GetUpdatedAt().Time,
	}
}

func reviewSnapshot(workItemID string, review *githubapi.PullRequestReview) value.ProviderCommentSnapshot {
	return value.ProviderCommentSnapshot{
		ProviderSlug:       string(enum.ProviderSlugGitHub),
		ProviderCommentID:  strconv.FormatInt(review.GetID(), 10),
		ProviderWorkItemID: workItemID,
		Kind:               string(enum.CommentKindReview),
		ReviewState:        normalizedAPIReviewState(review.GetState()),
		AuthorLogin:        userLogin(review.GetUser()),
		Body:               review.GetBody(),
		ProviderCreatedAt:  review.GetSubmittedAt().Time,
		ProviderUpdatedAt:  review.GetSubmittedAt().Time,
	}
}

func userLogin(user *githubapi.User) string {
	if user == nil {
		return ""
	}
	return user.GetLogin()
}

func normalizedAPIReviewState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "approved":
		return string(enum.ReviewStateApproved)
	case "changes_requested":
		return string(enum.ReviewStateChangesRequested)
	case "commented":
		return string(enum.ReviewStateCommented)
	case "dismissed":
		return string(enum.ReviewStateDismissed)
	default:
		return string(enum.ReviewStatePending)
	}
}

func providerWorkItemID(repository string, kind enum.WorkItemKind, number int) string {
	return strings.Join([]string{string(enum.ProviderSlugGitHub), repository, string(kind), strconv.Itoa(number)}, ":")
}

func boundedPageSize(maxItems int) int {
	if maxItems <= 0 || maxItems > githubAPIPerPage {
		return githubAPIPerPage
	}
	return maxItems
}

func cursorSince(cursor entity.SyncCursor) time.Time {
	if cursor.OverlapSince != nil {
		return cursor.OverlapSince.UTC()
	}
	return parseCursorTime(cursor.CursorValue)
}

func parseCursorTime(cursorValue string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(cursorValue))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func resultWithCursor(result providerclient.ReconciliationResult, cursorAt time.Time) providerclient.ReconciliationResult {
	if strings.TrimSpace(result.NextCursorValue) == "" {
		result.NextCursorValue = cursorAt.UTC().Format(time.RFC3339Nano)
	}
	if len(result.RateBudgetStateJSON) == 0 {
		result.RateBudgetStateJSON = []byte(`{}`)
	}
	return result
}

func maxProviderUpdatedAt(result providerclient.ReconciliationResult, fallback time.Time) time.Time {
	maxValue := fallback.UTC()
	for _, item := range result.WorkItems {
		if item.ProviderUpdatedAt.After(maxValue) {
			maxValue = item.ProviderUpdatedAt.UTC()
		}
	}
	for _, comment := range result.Comments {
		if comment.ProviderUpdatedAt.After(maxValue) {
			maxValue = comment.ProviderUpdatedAt.UTC()
		}
	}
	return maxValue
}

type githubRateBudgetState struct {
	Core *githubRateBudgetResource `json:"core,omitempty"`
}

type githubRateBudgetResource struct {
	Limit     int    `json:"limit"`
	Remaining int    `json:"remaining"`
	ResetAt   string `json:"reset_at,omitempty"`
}

func rateBudgetStateJSON(response *githubapi.Response) []byte {
	if response == nil {
		return []byte(`{}`)
	}
	payload := githubRateBudgetState{Core: &githubRateBudgetResource{
		Limit:     response.Rate.Limit,
		Remaining: response.Rate.Remaining,
		ResetAt:   response.Rate.Reset.UTC().Format(time.RFC3339Nano),
	}}
	raw, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}

func rateLimitsFromResponse(response *githubapi.Response) *githubapi.RateLimits {
	if response == nil {
		return nil
	}
	core := response.Rate
	return &githubapi.RateLimits{Core: &core}
}

func classifyGitHubError(err error) error {
	var rateLimitErr *githubapi.RateLimitError
	if errors.As(err, &rateLimitErr) {
		return providerError(providerclient.ErrorKindRateLimited, retryAfterRateLimit(rateLimitErr), nil)
	}
	var abuseErr *githubapi.AbuseRateLimitError
	if errors.As(err, &abuseErr) {
		return providerError(providerclient.ErrorKindRateLimited, abuseErr.GetRetryAfter(), nil)
	}
	var githubErr *githubapi.ErrorResponse
	if errors.As(err, &githubErr) && githubErr.Response != nil {
		switch githubErr.Response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return providerError(providerclient.ErrorKindAuthFailed, 0, nil)
		case http.StatusNotFound:
			return providerError(providerclient.ErrorKindNotFound, 0, nil)
		case http.StatusTooManyRequests:
			return providerError(providerclient.ErrorKindRateLimited, retryAfterHeader(githubErr.Response), nil)
		case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return providerError(providerclient.ErrorKindTransient, retryAfterHeader(githubErr.Response), nil)
		default:
			return providerError(providerclient.ErrorKindPermanent, 0, nil)
		}
	}
	return providerError(providerclient.ErrorKindTransient, 0, nil)
}

func providerError(kind providerclient.ErrorKind, retryAfter time.Duration, cause error) error {
	return &providerclient.Error{Kind: kind, RetryAfter: retryAfter, Cause: cause}
}

func retryAfterRateLimit(err *githubapi.RateLimitError) time.Duration {
	if err == nil || err.Rate.Reset.IsZero() {
		return 0
	}
	retryAfter := time.Until(err.Rate.Reset.Time)
	if retryAfter < 0 {
		return 0
	}
	return retryAfter
}

func retryAfterHeader(response *http.Response) time.Duration {
	if response == nil {
		return 0
	}
	value := strings.TrimSpace(response.Header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
