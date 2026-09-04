package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

func githubRead[T any](ctx context.Context, capability integrationpackage.Capability, call func() (T, *github.Response, error)) (T, error) {
	for attempt := 1; ; attempt++ {
		value, response, err := call()
		if err == nil {
			return value, nil
		}
		retryAfter := ""
		retry := response == nil
		if response != nil {
			retryAfter = response.Header.Get("Retry-After")
			retry = retryableProviderStatus(response.StatusCode) || githubRateLimited(response)
		}
		if retry && attempt < capability.Execution.MaxAttempts && waitProviderRetry(ctx, capability, attempt, retryAfter) {
			continue
		}
		if githubRateLimited(response) {
			return value, &SafeError{Code: "INTEGRATION_RATE_LIMITED"}
		}
		return value, githubError(response, err)
	}
}

func githubRateLimited(response *github.Response) bool {
	return response != nil && (response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden &&
		(response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != ""))
}

func githubMutationError(response *github.Response, err error) error {
	if err == nil {
		return nil
	}
	if response == nil || response.StatusCode >= 500 || response.StatusCode >= 200 && response.StatusCode < 300 {
		return &UnknownOutcomeError{}
	}
	if githubRateLimited(response) {
		return &SafeError{Code: "INTEGRATION_RATE_LIMITED"}
	}
	return githubError(response, err)
}

func findGitHubIssueByMarker(ctx context.Context, client *github.Client, owner, repository string, capability integrationpackage.Capability, marker string) (*github.Issue, error) {
	for page := 1; page <= 100; page++ {
		var next int
		issues, err := githubRead(ctx, capability, func() ([]*github.Issue, *github.Response, error) {
			items, response, err := client.Issues.ListByRepo(ctx, owner, repository, &github.IssueListByRepoOptions{State: "all", ListOptions: github.ListOptions{Page: page, PerPage: 100}})
			if response != nil {
				next = response.NextPage
			}
			return items, response, err
		})
		if err != nil {
			return nil, err
		}
		for _, issue := range issues {
			if !issue.IsPullRequest() && strings.Contains(issue.GetBody(), marker) {
				return issue, nil
			}
		}
		if next == 0 {
			return nil, nil
		}
		if next != page+1 {
			break
		}
	}
	return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
}

func githubIssueMatchesUpdate(issue *github.Issue, title, body, state string) bool {
	return issue != nil && !issue.IsPullRequest() && (title == "" || issue.GetTitle() == title) &&
		(body == "" || issue.GetBody() == body) && (state == "" || issue.GetState() == state)
}

func (adapter *Adapter) listGitHubIssues(ctx context.Context, client *github.Client, owner, repository string, request Request, capability integrationpackage.Capability, raw []byte) (Result, error) {
	var input struct {
		State         string
		Limit, Cursor int
	}
	if json.Unmarshal(raw, &input) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if input.Limit == 0 {
		input.Limit = 30
	}
	if input.Cursor == 0 {
		input.Cursor = 1
	}
	if input.State == "" {
		input.State = "open"
	}
	var next int
	issues, err := githubRead(ctx, capability, func() ([]*github.Issue, *github.Response, error) {
		items, response, err := client.Issues.ListByRepo(ctx, owner, repository, &github.IssueListByRepoOptions{State: input.State, ListOptions: github.ListOptions{Page: input.Cursor, PerPage: input.Limit}})
		if response != nil {
			next = response.NextPage
		}
		return items, response, err
	})
	if err != nil {
		return Result{}, err
	}
	projection := make([]map[string]any, 0, len(issues))
	for _, issue := range issues {
		if issue.IsPullRequest() {
			continue
		}
		if issue.GetID() == 0 || issue.GetNumber() < 1 {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		projection = append(projection, map[string]any{"number": issue.GetNumber(), "title": issue.GetTitle(), "state": issue.GetState()})
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return Result{}, err
	}
	output := map[string]any{"count": len(projection), "issues": string(encoded)}
	if next != 0 {
		output["next_cursor"] = next
	}
	return providerResult(request, "github-issues:"+owner+"/"+repository+":"+strconv.Itoa(input.Cursor), output)
}

func (adapter *Adapter) readGitHubIssue(ctx context.Context, client *github.Client, owner, repository string, request Request, capability integrationpackage.Capability, raw []byte) (Result, error) {
	var input struct {
		Number int `json:"issue_number"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	issue, err := githubRead(ctx, capability, func() (*github.Issue, *github.Response, error) {
		return client.Issues.Get(ctx, owner, repository, input.Number)
	})
	if err != nil {
		return Result{}, err
	}
	if issue.GetNumber() != input.Number {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return githubIssueResult(issue, request, true)
}

func (adapter *Adapter) createGitHubIssueComment(ctx context.Context, client *github.Client, owner, repository string, request Request, capability integrationpackage.Capability, raw []byte) (Result, error) {
	var input struct {
		Number int    `json:"issue_number"`
		Body   string `json:"body"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	issue, err := githubRead(ctx, capability, func() (*github.Issue, *github.Response, error) {
		return client.Issues.Get(ctx, owner, repository, input.Number)
	})
	if err != nil {
		return Result{}, err
	}
	if issue.IsPullRequest() || issue.GetNumber() != input.Number {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	marker := "<!-- kodex-effect:" + request.EffectKey + " -->"
	find := func() (*github.IssueComment, error) {
		for page := 1; page <= 100; page++ {
			comments, response, err := client.Issues.ListComments(ctx, owner, repository, input.Number, &github.IssueListCommentsOptions{ListOptions: github.ListOptions{Page: page, PerPage: 100}})
			if err != nil {
				return nil, githubError(response, err)
			}
			for _, comment := range comments {
				if strings.Contains(comment.GetBody(), marker) {
					return comment, nil
				}
			}
			if response == nil || response.NextPage == 0 {
				return nil, nil
			}
			if response.NextPage != page+1 {
				break
			}
		}
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	comment, err := find()
	if err != nil {
		return Result{}, err
	}
	if comment == nil {
		var response *github.Response
		comment, response, err = client.Issues.CreateComment(ctx, owner, repository, input.Number, &github.IssueComment{Body: github.Ptr(input.Body + "\n\n" + marker)})
		if err = githubMutationError(response, err); err != nil {
			if IsUnknownOutcome(err) {
				if found, findErr := find(); findErr == nil && found != nil {
					comment, err = found, nil
				}
			}
			if err != nil {
				return Result{}, err
			}
		}
	}
	if comment.GetID() < 1 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, "github-comment:"+strconv.FormatInt(comment.GetID(), 10), map[string]any{"comment_id": comment.GetID(), "issue_number": input.Number})
}
