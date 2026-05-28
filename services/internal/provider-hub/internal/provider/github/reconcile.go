package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
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
		workItem, comments, cursorAt, err := a.fetchWorkItemComments(ctx, client, target, request.Cursor.CursorValue, int(request.MaxItems), observedAt)
		if err != nil {
			return providerclient.ReconciliationResult{}, err
		}
		if workItem.ProviderWorkItemID != "" {
			result.WorkItems = append(result.WorkItems, workItem)
		}
		result.Comments = append(result.Comments, comments...)
		return resultWithCursor(result, cursorAt), nil
	case enum.SyncArtifactRelationship:
		workItem, err := a.fetchWorkItemSnapshot(ctx, client, target)
		if err != nil {
			return providerclient.ReconciliationResult{}, err
		}
		if workItem.ProviderWorkItemID != "" {
			result.WorkItems = append(result.WorkItems, workItem)
			if !workItem.ProviderUpdatedAt.IsZero() {
				return resultWithCursor(result, workItem.ProviderUpdatedAt), nil
			}
		}
	default:
		return providerclient.ReconciliationResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	return resultWithCursor(result, observedAt), nil
}

func (a *Adapter) reconcileRepository(ctx context.Context, client *githubapi.Client, request providerclient.ReconciliationRequest, observedAt time.Time) (providerclient.ReconciliationResult, error) {
	owner, repo, err := a.resolveRepositoryRef(ctx, client, request.Cursor.ScopeRef)
	if err != nil {
		return providerclient.ReconciliationResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	switch request.Cursor.ArtifactKind {
	case enum.SyncArtifactIssue:
		return a.reconcileRepositoryIssues(ctx, client, owner, repo, request, observedAt)
	case enum.SyncArtifactPullRequest:
		return a.reconcileRepositoryPullRequests(ctx, client, owner, repo, request, observedAt)
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

func (a *Adapter) reconcileRepositoryIssues(ctx context.Context, client *githubapi.Client, owner string, repo string, request providerclient.ReconciliationRequest, observedAt time.Time) (providerclient.ReconciliationResult, error) {
	since := cursorSince(request.Cursor)
	options := &githubapi.IssueListByRepoOptions{
		State:     "all",
		Sort:      "updated",
		Direction: "asc",
		Since:     since,
		ListOptions: githubapi.ListOptions{
			PerPage: githubAPIPerPage,
		},
	}
	result := providerclient.ReconciliationResult{
		RateBudgetStateJSON: []byte(`{}`),
	}
	lastSeenAt := time.Time{}
	for {
		items, response, err := client.Issues.ListByRepo(ctx, owner, repo, options)
		if err != nil {
			return providerclient.ReconciliationResult{}, classifyGitHubError(err)
		}
		result.LimitSnapshots = a.limitSnapshots(request.Credential.ExternalAccountID, observedAt, rateLimitsFromResponse(response))
		result.RateBudgetStateJSON = rateBudgetStateJSON(response)
		for _, item := range items {
			if item == nil {
				continue
			}
			lastSeenAt = maxTime(lastSeenAt, issueUpdatedAt(item, observedAt))
			if item.IsPullRequest() {
				continue
			}
			result.WorkItems = append(result.WorkItems, issueAPISnapshot(owner+"/"+repo, item, enum.WorkItemKindIssue))
			return resultWithCursor(result, issueUpdatedAt(item, observedAt)), nil
		}
		if response == nil || response.NextPage == 0 {
			break
		}
		options.ListOptions.Page = response.NextPage
	}
	return resultWithCursor(result, cursorAfterEmptyProjectionPage(lastSeenAt, observedAt)), nil
}

func (a *Adapter) reconcileRepositoryPullRequests(ctx context.Context, client *githubapi.Client, owner string, repo string, request providerclient.ReconciliationRequest, observedAt time.Time) (providerclient.ReconciliationResult, error) {
	since := cursorSince(request.Cursor)
	options := &githubapi.IssueListByRepoOptions{
		State:     "all",
		Sort:      "updated",
		Direction: "asc",
		ListOptions: githubapi.ListOptions{
			PerPage: githubAPIPerPage,
		},
		Since: since,
	}
	result := providerclient.ReconciliationResult{
		RateBudgetStateJSON: []byte(`{}`),
	}
	lastSeenAt := time.Time{}
	for {
		items, response, err := client.Issues.ListByRepo(ctx, owner, repo, options)
		if err != nil {
			return providerclient.ReconciliationResult{}, classifyGitHubError(err)
		}
		result.LimitSnapshots = a.limitSnapshots(request.Credential.ExternalAccountID, observedAt, rateLimitsFromResponse(response))
		result.RateBudgetStateJSON = rateBudgetStateJSON(response)
		for _, item := range items {
			if item == nil {
				continue
			}
			lastSeenAt = maxTime(lastSeenAt, issueUpdatedAt(item, observedAt))
			if !item.IsPullRequest() {
				continue
			}
			result.WorkItems = append(result.WorkItems, pullRequestIssueAPISnapshot(owner+"/"+repo, item))
			return resultWithCursor(result, issueUpdatedAt(item, observedAt)), nil
		}
		if response == nil || response.NextPage == 0 {
			break
		}
		options.ListOptions.Page = response.NextPage
	}
	return resultWithCursor(result, cursorAfterEmptyProjectionPage(lastSeenAt, observedAt)), nil
}

func (a *Adapter) fetchWorkItemComments(ctx context.Context, client *githubapi.Client, target workItemScope, cursorValue string, maxItems int, observedAt time.Time) (value.ProviderWorkItemSnapshot, []value.ProviderCommentSnapshot, time.Time, error) {
	workItem, actualKind, err := a.fetchWorkItemSnapshotWithKind(ctx, client, target)
	if err != nil {
		return value.ProviderWorkItemSnapshot{}, nil, time.Time{}, err
	}
	target.kind = actualKind
	since := parseCursorTime(cursorValue)
	sortValue := "updated"
	directionValue := "asc"
	options := &githubapi.IssueListCommentsOptions{
		Sort:      &sortValue,
		Direction: &directionValue,
		Since:     &since,
		ListOptions: githubapi.ListOptions{
			PerPage: githubAPIPerPage,
		},
	}
	candidates := make([]commentCandidate, 0, maxItems)
	for {
		items, response, err := client.Issues.ListComments(ctx, target.owner, target.repo, target.number, options)
		if err != nil {
			return value.ProviderWorkItemSnapshot{}, nil, time.Time{}, classifyGitHubError(err)
		}
		for _, item := range items {
			if item == nil {
				continue
			}
			snapshot := issueCommentSnapshot(workItem.ProviderWorkItemID, item)
			candidates = append(candidates, commentCandidate{Snapshot: snapshot, CursorAt: commentUpdatedAt(snapshot, observedAt)})
		}
		if len(candidates) >= maxItems || response == nil || response.NextPage == 0 {
			break
		}
		options.Page = response.NextPage
	}
	if target.kind == enum.WorkItemKindPullRequest {
		reviewOptions := &githubapi.ListOptions{PerPage: githubAPIPerPage}
		reviewCandidateCount := 0
		for {
			reviews, response, err := client.PullRequests.ListReviews(ctx, target.owner, target.repo, target.number, reviewOptions)
			if err != nil {
				return value.ProviderWorkItemSnapshot{}, nil, time.Time{}, classifyGitHubError(err)
			}
			for _, review := range reviews {
				if review == nil || !review.GetSubmittedAt().After(since) {
					continue
				}
				snapshot := reviewSnapshot(workItem.ProviderWorkItemID, review)
				candidates = append(candidates, commentCandidate{Snapshot: snapshot, CursorAt: commentUpdatedAt(snapshot, observedAt)})
				reviewCandidateCount++
			}
			if response == nil || response.NextPage == 0 || (reviewCandidateCount > 0 && len(candidates) >= maxItems) {
				break
			}
			reviewOptions.Page = response.NextPage
		}
	}
	comments, cursorAt := selectCommentCandidates(candidates, maxItems, observedAt)
	return workItem, comments, cursorAt, nil
}

func (a *Adapter) fetchWorkItemSnapshot(ctx context.Context, client *githubapi.Client, target workItemScope) (value.ProviderWorkItemSnapshot, error) {
	snapshot, _, err := a.fetchWorkItemSnapshotWithKind(ctx, client, target)
	return snapshot, err
}

func (a *Adapter) fetchWorkItemSnapshotWithKind(ctx context.Context, client *githubapi.Client, target workItemScope) (value.ProviderWorkItemSnapshot, enum.WorkItemKind, error) {
	switch target.kind {
	case enum.WorkItemKindIssue:
		item, _, err := client.Issues.Get(ctx, target.owner, target.repo, target.number)
		if err != nil {
			return value.ProviderWorkItemSnapshot{}, "", classifyGitHubError(err)
		}
		if item.IsPullRequest() {
			return value.ProviderWorkItemSnapshot{}, "", nil
		}
		return issueAPISnapshot(target.repository(), item, enum.WorkItemKindIssue), enum.WorkItemKindIssue, nil
	case enum.WorkItemKindPullRequest:
		item, _, err := client.PullRequests.Get(ctx, target.owner, target.repo, target.number)
		if err != nil {
			return value.ProviderWorkItemSnapshot{}, "", classifyGitHubError(err)
		}
		return pullRequestAPISnapshot(target.repository(), item), enum.WorkItemKindPullRequest, nil
	case "":
		item, _, err := client.Issues.Get(ctx, target.owner, target.repo, target.number)
		if err != nil {
			return value.ProviderWorkItemSnapshot{}, "", classifyGitHubError(err)
		}
		if !item.IsPullRequest() {
			return issueAPISnapshot(target.repository(), item, enum.WorkItemKindIssue), enum.WorkItemKindIssue, nil
		}
		pullRequest, _, err := client.PullRequests.Get(ctx, target.owner, target.repo, target.number)
		if err != nil {
			return value.ProviderWorkItemSnapshot{}, "", classifyGitHubError(err)
		}
		return pullRequestAPISnapshot(target.repository(), pullRequest), enum.WorkItemKindPullRequest, nil
	default:
		return value.ProviderWorkItemSnapshot{}, "", providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
}

func parseWorkItemScope(scopeRef string) (workItemScope, error) {
	if rawURL, ok := strings.CutPrefix(strings.TrimSpace(scopeRef), "web_url:"); ok {
		return parseGitHubWorkItemURL(rawURL)
	}
	if providerObjectID, ok := strings.CutPrefix(strings.TrimSpace(scopeRef), "provider_object_id:"); ok {
		return parseGitHubProviderWorkItemID(providerObjectID)
	}
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
	case string(enum.WorkItemKindIssue):
		target.kind = enum.WorkItemKindIssue
	case string(enum.WorkItemKindPullRequest):
		target.kind = enum.WorkItemKindPullRequest
	case "number", "":
		target.kind = ""
	default:
		return workItemScope{}, providerclient.ErrUnsupported
	}
	return target, nil
}

func (a *Adapter) resolveRepositoryRef(ctx context.Context, client *githubapi.Client, scopeRef string) (string, string, error) {
	scopeRef = strings.TrimSpace(scopeRef)
	if providerRepositoryID, ok := strings.CutPrefix(scopeRef, "provider_repository_id:"); ok {
		id, err := strconv.ParseInt(strings.TrimSpace(providerRepositoryID), 10, 64)
		if err != nil || id <= 0 {
			return "", "", providerclient.ErrUnsupported
		}
		repository, _, err := client.Repositories.GetByID(ctx, id)
		if err != nil {
			return "", "", classifyGitHubError(err)
		}
		return parseRepositoryRef(repository.GetFullName())
	}
	if rawURL, ok := strings.CutPrefix(scopeRef, "web_url:"); ok {
		return parseGitHubRepositoryURL(rawURL)
	}
	return parseRepositoryRef(scopeRef)
}

func parseRepositoryRef(scopeRef string) (string, string, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(scopeRef), "/")
	if !ok || strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" || strings.Contains(repo, "/") {
		return "", "", providerclient.ErrUnsupported
	}
	return strings.TrimSpace(owner), strings.TrimSpace(repo), nil
}

func parseGitHubWorkItemURL(rawURL string) (workItemScope, error) {
	owner, repo, kind, number, err := parseGitHubWorkItemURLParts(rawURL)
	if err != nil {
		return workItemScope{}, err
	}
	return workItemScope{owner: owner, repo: repo, kind: kind, number: number}, nil
}

func parseGitHubWorkItemURLParts(rawURL string) (string, string, enum.WorkItemKind, int, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", "", "", 0, providerclient.ErrUnsupported
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	if host != "github.com" {
		return "", "", "", 0, providerclient.ErrUnsupported
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 4 {
		return "", "", "", 0, providerclient.ErrUnsupported
	}
	owner, repo := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	number, err := strconv.Atoi(strings.TrimSpace(parts[3]))
	if owner == "" || repo == "" || err != nil || number <= 0 {
		return "", "", "", 0, providerclient.ErrUnsupported
	}
	switch parts[2] {
	case "issues":
		return owner, repo, enum.WorkItemKindIssue, number, nil
	case "pull":
		return owner, repo, enum.WorkItemKindPullRequest, number, nil
	default:
		return "", "", "", 0, providerclient.ErrUnsupported
	}
}

func parseGitHubRepositoryURL(rawURL string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", "", providerclient.ErrUnsupported
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	if host != "github.com" {
		return "", "", providerclient.ErrUnsupported
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", providerclient.ErrUnsupported
	}
	return parseRepositoryRef(parts[0] + "/" + parts[1])
}

func parseGitHubProviderWorkItemID(providerObjectID string) (workItemScope, error) {
	parts := strings.Split(strings.TrimSpace(providerObjectID), ":")
	if len(parts) != 4 || parts[0] != string(enum.ProviderSlugGitHub) {
		return workItemScope{}, providerclient.ErrUnsupported
	}
	owner, repo, err := parseRepositoryRef(parts[1])
	if err != nil {
		return workItemScope{}, err
	}
	number, err := strconv.Atoi(strings.TrimSpace(parts[3]))
	if err != nil || number <= 0 {
		return workItemScope{}, providerclient.ErrUnsupported
	}
	kind := enum.WorkItemKind(strings.TrimSpace(parts[2]))
	switch kind {
	case enum.WorkItemKindIssue, enum.WorkItemKindPullRequest:
		return workItemScope{owner: owner, repo: repo, kind: kind, number: number}, nil
	default:
		return workItemScope{}, providerclient.ErrUnsupported
	}
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

type commentCandidate struct {
	Snapshot value.ProviderCommentSnapshot
	CursorAt time.Time
}

func selectCommentCandidates(candidates []commentCandidate, maxItems int, observedAt time.Time) ([]value.ProviderCommentSnapshot, time.Time) {
	if len(candidates) == 0 {
		return nil, observedAt
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if !left.CursorAt.Equal(right.CursorAt) {
			return left.CursorAt.Before(right.CursorAt)
		}
		return left.Snapshot.ProviderCommentID < right.Snapshot.ProviderCommentID
	})
	limit := maxItems
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}
	comments := make([]value.ProviderCommentSnapshot, 0, limit)
	for _, candidate := range candidates[:limit] {
		comments = append(comments, candidate.Snapshot)
	}
	return comments, commentUpdatedAt(comments[len(comments)-1], observedAt)
}

func issueUpdatedAt(issue *githubapi.Issue, fallback time.Time) time.Time {
	if issue == nil || issue.GetUpdatedAt().IsZero() {
		return fallback.UTC()
	}
	return issue.GetUpdatedAt().UTC()
}

func commentUpdatedAt(comment value.ProviderCommentSnapshot, fallback time.Time) time.Time {
	if comment.ProviderUpdatedAt.IsZero() {
		return fallback.UTC()
	}
	return comment.ProviderUpdatedAt.UTC()
}

func cursorAfterEmptyProjectionPage(lastSeenAt time.Time, observedAt time.Time) time.Time {
	if lastSeenAt.IsZero() {
		return observedAt.UTC()
	}
	return lastSeenAt.UTC()
}

func maxTime(left time.Time, right time.Time) time.Time {
	if right.IsZero() {
		return left
	}
	if left.IsZero() || right.After(left) {
		return right.UTC()
	}
	return left.UTC()
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
		case http.StatusUnauthorized:
			return providerError(providerclient.ErrorKindAuthFailed, 0, nil)
		case http.StatusForbidden:
			if gitHubErrorLooksRateLimited(githubErr) {
				return providerError(providerclient.ErrorKindRateLimited, retryAfterGitHubResponse(githubErr.Response), nil)
			}
			return providerError(providerclient.ErrorKindAuthFailed, 0, nil)
		case http.StatusNotFound:
			return providerError(providerclient.ErrorKindNotFound, 0, nil)
		case http.StatusTooManyRequests:
			return providerError(providerclient.ErrorKindRateLimited, retryAfterGitHubResponse(githubErr.Response), nil)
		case http.StatusConflict, http.StatusPreconditionFailed:
			return providerError(providerclient.ErrorKindConflict, 0, nil)
		case http.StatusUnprocessableEntity:
			return providerError(providerclient.ErrorKindValidation, 0, nil)
		case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return providerError(providerclient.ErrorKindTransient, retryAfterHeader(githubErr.Response), nil)
		default:
			return providerError(providerclient.ErrorKindPermanent, 0, nil)
		}
	}
	return providerError(providerclient.ErrorKindTransient, 0, nil)
}

func gitHubErrorLooksRateLimited(err *githubapi.ErrorResponse) bool {
	if err == nil {
		return false
	}
	if err.Response != nil {
		if strings.TrimSpace(err.Response.Header.Get("Retry-After")) != "" {
			return true
		}
		if strings.TrimSpace(err.Response.Header.Get("X-RateLimit-Remaining")) == "0" {
			return true
		}
	}
	message := strings.ToLower(strings.TrimSpace(err.Message))
	return strings.Contains(message, "rate limit") ||
		strings.Contains(message, "abuse detection")
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

func retryAfterGitHubResponse(response *http.Response) time.Duration {
	if retryAfter := retryAfterHeader(response); retryAfter > 0 {
		return retryAfter
	}
	if response == nil {
		return 0
	}
	reset := strings.TrimSpace(response.Header.Get("X-RateLimit-Reset"))
	if reset == "" {
		return 0
	}
	unixSeconds, err := strconv.ParseInt(reset, 10, 64)
	if err != nil || unixSeconds <= 0 {
		return 0
	}
	retryAfter := time.Until(time.Unix(unixSeconds, 0))
	if retryAfter < 0 {
		return 0
	}
	return retryAfter
}
