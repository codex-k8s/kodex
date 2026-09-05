package integration

import (
	"context"
	"net/url"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

type githubPullView struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Head   string `json:"head"`
	Base   string `json:"base"`
	SHA    string `json:"sha"`
	Draft  bool   `json:"draft"`
	URL    string `json:"url"`
}

func projectGitHubPull(value *github.PullRequest) githubPullView {
	return githubPullView{value.GetNumber(), value.GetTitle(), value.GetBody(), value.GetState(), value.GetHead().GetRef(), value.GetBase().GetRef(), value.GetHead().GetSHA(), value.GetDraft(), value.GetHTMLURL()}
}

type githubReviewView struct {
	ID       int64  `json:"id"`
	Body     string `json:"body"`
	State    string `json:"state"`
	CommitID string `json:"commit_id"`
}

func projectGitHubReview(value *github.PullRequestReview) githubReviewView {
	return githubReviewView{value.GetID(), value.GetBody(), value.GetState(), value.GetCommitID()}
}

type githubCommentView struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

func (adapter *Adapter) executeGitHubCollaboration(ctx context.Context, client *github.Client, owner, repo string, request Request, capability integrationpackage.Capability, in githubCatalogInput, options github.ListOptions) (Result, error) {
	switch request.Operation {
	case "github.pull_request.file.list":
		return githubCatalogPage(ctx, capability, request, in.Limit, in.Cursor, func() ([]githubFileChange, *github.Response, error) {
			files, response, err := client.PullRequests.ListFiles(ctx, owner, repo, in.Number, &options)
			if err != nil {
				return nil, response, err
			}
			projected, err := projectGitHubFiles(files)
			return projected, response, err
		})
	case "github.pull_request.list":
		return githubCatalogPage(ctx, capability, request, in.Limit, in.Cursor, func() ([]githubPullView, *github.Response, error) {
			items, response, err := client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{State: in.State, Head: in.Head, Base: in.Base, ListOptions: options})
			views := make([]githubPullView, 0, len(items))
			for _, item := range items {
				views = append(views, projectGitHubPull(item))
			}
			return views, response, err
		})
	case "github.pull_request.read":
		item, err := githubRead(ctx, capability, func() (*github.PullRequest, *github.Response, error) {
			return client.PullRequests.Get(ctx, owner, repo, in.Number)
		})
		if err != nil {
			return Result{}, err
		}
		if item == nil || item.GetNumber() != in.Number {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "github-pull:"+strconv.Itoa(in.Number), projectGitHubPull(item))
	case "github.pull_request.create", "github.pull_request.update":
		var item *github.PullRequest
		var response *github.Response
		var err error
		if request.Operation == "github.pull_request.create" {
			item, response, err = client.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{Title: github.Ptr(in.Title), Head: github.Ptr(in.Head), Base: github.Ptr(in.Base), Body: github.Ptr(in.Body)})
		} else {
			update := &github.PullRequest{}
			if _, ok := request.Input["title"]; ok {
				update.Title = github.Ptr(in.Title)
			}
			if _, ok := request.Input["body"]; ok {
				update.Body = github.Ptr(in.Body)
			}
			if in.State != "" {
				update.State = github.Ptr(in.State)
			}
			if in.Base != "" {
				update.Base = &github.PullRequestBranch{Ref: github.Ptr(in.Base)}
			}
			item, response, err = client.PullRequests.Edit(ctx, owner, repo, in.Number, update)
		}
		if err := githubMutationError(response, err); err != nil {
			return Result{}, err
		}
		if item == nil || item.GetNumber() <= 0 || in.Number != 0 && item.GetNumber() != in.Number {
			return Result{}, &UnknownOutcomeError{}
		}
		return providerResult(request, "github-pull:"+strconv.Itoa(item.GetNumber()), projectGitHubPull(item))
	case "github.pull_request.merge":
		result, response, err := client.PullRequests.Merge(ctx, owner, repo, in.Number, in.Message, &github.PullRequestOptions{SHA: in.SHA, MergeMethod: in.MergeMethod})
		if err := githubMutationError(response, err); err != nil {
			return Result{}, err
		}
		if result == nil {
			return Result{}, &UnknownOutcomeError{}
		}
		return providerResult(request, "github-pull:"+strconv.Itoa(in.Number), struct {
			Merged bool   `json:"merged"`
			SHA    string `json:"sha"`
		}{result.GetMerged(), result.GetSHA()})
	case "github.pull_request.review.list":
		return githubCatalogPage(ctx, capability, request, in.Limit, in.Cursor, func() ([]githubReviewView, *github.Response, error) {
			items, response, err := client.PullRequests.ListReviews(ctx, owner, repo, in.Number, &options)
			views := make([]githubReviewView, 0, len(items))
			for _, item := range items {
				views = append(views, projectGitHubReview(item))
			}
			return views, response, err
		})
	case "github.pull_request.review.read":
		item, err := githubRead(ctx, capability, func() (*github.PullRequestReview, *github.Response, error) {
			return client.PullRequests.GetReview(ctx, owner, repo, in.Number, in.ReviewID)
		})
		if err != nil {
			return Result{}, err
		}
		if item == nil || item.GetID() != in.ReviewID {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "github-review:"+strconv.FormatInt(in.ReviewID, 10), projectGitHubReview(item))
	case "github.pull_request.review.create":
		item, response, err := client.PullRequests.CreateReview(ctx, owner, repo, in.Number, &github.PullRequestReviewRequest{CommitID: github.Ptr(in.SHA), Body: github.Ptr(in.Body), Event: github.Ptr(in.Event)})
		if err := githubMutationError(response, err); err != nil {
			return Result{}, err
		}
		if item == nil || item.GetID() <= 0 {
			return Result{}, &UnknownOutcomeError{}
		}
		return providerResult(request, "github-review:"+strconv.FormatInt(item.GetID(), 10), projectGitHubReview(item))
	case "github.issue.comment.list", "github.issue.comment.read", "github.issue.comment.update", "github.issue.comment.delete":
		issue, err := githubRead(ctx, capability, func() (*github.Issue, *github.Response, error) {
			return client.Issues.Get(ctx, owner, repo, in.IssueNumber)
		})
		if err != nil {
			return Result{}, err
		}
		if issue == nil || issue.IsPullRequest() || issue.GetNumber() != in.IssueNumber {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if request.Operation == "github.issue.comment.list" {
			return githubCatalogPage(ctx, capability, request, in.Limit, in.Cursor, func() ([]githubCommentView, *github.Response, error) {
				items, response, err := client.Issues.ListComments(ctx, owner, repo, in.IssueNumber, &github.IssueListCommentsOptions{ListOptions: options})
				views := make([]githubCommentView, 0, len(items))
				for _, item := range items {
					views = append(views, githubCommentView{item.GetID(), item.GetBody()})
				}
				return views, response, err
			})
		}
		comment, err := githubRead(ctx, capability, func() (*github.IssueComment, *github.Response, error) {
			return client.Issues.GetComment(ctx, owner, repo, in.CommentID)
		})
		if err != nil {
			return Result{}, err
		}
		expected := *client.BaseURL
		expected.Path += "repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/issues/" + strconv.Itoa(in.IssueNumber)
		if comment == nil || comment.GetID() != in.CommentID || comment.GetIssueURL() != expected.String() {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if request.Operation == "github.issue.comment.read" {
			return providerResult(request, "github-comment:"+strconv.FormatInt(in.CommentID, 10), githubCommentView{comment.GetID(), comment.GetBody()})
		}
		if request.Operation == "github.issue.comment.delete" {
			response, err := client.Issues.DeleteComment(ctx, owner, repo, in.CommentID)
			return githubCommandResult(request, response, err)
		}
		comment, response, err := client.Issues.EditComment(ctx, owner, repo, in.CommentID, &github.IssueComment{Body: github.Ptr(in.Body)})
		if err := githubMutationError(response, err); err != nil {
			return Result{}, err
		}
		if comment == nil || comment.GetID() != in.CommentID {
			return Result{}, &UnknownOutcomeError{}
		}
		return providerResult(request, "github-comment:"+strconv.FormatInt(in.CommentID, 10), githubCommentView{comment.GetID(), comment.GetBody()})
	default:
		return adapter.executeGitHubAutomation(ctx, client, owner, repo, request, capability, in, options)
	}
}
