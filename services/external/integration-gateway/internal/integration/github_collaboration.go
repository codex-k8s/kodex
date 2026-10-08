package integration

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"unicode/utf8"

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

// Полный read отдельно закрепляет источник. Mutation replies не получают
// дополнительных обязательных полей и сохраняют прежнюю семантику outcome.
type githubPullReadView struct {
	githubPullView
	BaseSHA      string `json:"base_sha"`
	ChangedFiles int    `json:"changed_files"`
}

func projectGitHubPullRead(value *github.PullRequest, number int) (githubPullReadView, error) {
	if value == nil || value.GetNumber() != number || value.Head == nil || value.Base == nil ||
		!sourceCommitPattern.MatchString(value.Head.GetSHA()) || !sourceCommitPattern.MatchString(value.Base.GetSHA()) ||
		value.ChangedFiles == nil || value.GetChangedFiles() < 0 || value.GetChangedFiles() > 2147483647 ||
		(value.GetState() != "open" && value.GetState() != "closed") {
		return githubPullReadView{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return githubPullReadView{projectGitHubPull(value), value.Base.GetSHA(), value.GetChangedFiles()}, nil
}

const maximumGitHubPullFilePageSize = 4
const maximumGitHubPullFiles = 3000
const maximumGitHubFilePathBytes = 1024
const maximumGitHubFileChangeCount = int64(9007199254740991)

// Это индекс метаданных, не прочитанный patch или содержимое файла.
// Nullable SHA сохраняет отсутствие provider blob identity без фиктивного hash.
type githubPullFileIndex struct {
	Filename         string  `json:"filename"`
	PreviousFilename string  `json:"previous_filename,omitempty"`
	SHA              *string `json:"sha"`
	Status           string  `json:"status"`
	Additions        int     `json:"additions"`
	Deletions        int     `json:"deletions"`
}

type githubPullFilePage struct {
	Items    string `json:"items"`
	Count    int    `json:"count"`
	Next     int    `json:"next_cursor,omitempty"`
	HeadSHA  string `json:"head_sha"`
	BaseSHA  string `json:"base_sha"`
	Total    int    `json:"total_count"`
	PageSize int    `json:"page_size"`
	Offset   int    `json:"offset"`
	EOF      bool   `json:"eof"`
}

func validGitHubIndexPath(path string, allowEmpty bool) bool {
	if len(path) > maximumGitHubFilePathBytes || !utf8.ValidString(path) || !validRepositoryPath(path, allowEmpty) {
		return false
	}
	for _, character := range path {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func projectGitHubPullFiles(files []*github.CommitFile) ([]githubPullFileIndex, error) {
	result := make([]githubPullFileIndex, 0, len(files))
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		if file == nil || !validGitHubIndexPath(file.GetFilename(), false) || !validGitHubIndexPath(file.GetPreviousFilename(), true) ||
			seen[file.GetFilename()] || file.SHA != nil && !sourceCommitPattern.MatchString(*file.SHA) ||
			file.Additions == nil || file.Deletions == nil || file.Changes == nil ||
			file.GetAdditions() < 0 || file.GetDeletions() < 0 || file.GetChanges() < 0 ||
			int64(file.GetAdditions()) > maximumGitHubFileChangeCount || int64(file.GetDeletions()) > maximumGitHubFileChangeCount ||
			int64(file.GetChanges()) > maximumGitHubFileChangeCount || int64(file.GetAdditions())+int64(file.GetDeletions()) != int64(file.GetChanges()) {
			return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		switch file.GetStatus() {
		case "added", "removed", "modified", "copied", "changed", "unchanged":
		case "renamed":
			if file.GetPreviousFilename() == "" {
				return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
		default:
			return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		seen[file.GetFilename()] = true
		result = append(result, githubPullFileIndex{file.GetFilename(), file.GetPreviousFilename(), file.SHA, file.GetStatus(), file.GetAdditions(), file.GetDeletions()})
	}
	return result, nil
}

func executeGitHubPullFileIndex(ctx context.Context, client *github.Client, owner, repo string, request Request, capability integrationpackage.Capability, in githubCatalogInput) (Result, error) {
	if in.Limit < 1 || in.Limit > maximumGitHubPullFilePageSize || in.Cursor < 1 || in.Cursor > 10000 ||
		!sourceCommitPattern.MatchString(in.ExpectedHeadSHA) || !sourceCommitPattern.MatchString(in.ExpectedBaseSHA) ||
		in.ExpectedChangedFiles == nil || *in.ExpectedChangedFiles < 0 || *in.ExpectedChangedFiles > maximumGitHubPullFiles {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	offset := (in.Cursor - 1) * in.Limit
	if offset > *in.ExpectedChangedFiles || in.Cursor > 1 && offset == *in.ExpectedChangedFiles {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	readPinned := func() error {
		pull, err := githubRead(ctx, capability, func() (*github.PullRequest, *github.Response, error) {
			return client.PullRequests.Get(ctx, owner, repo, in.Number)
		})
		if err != nil {
			return err
		}
		view, err := projectGitHubPullRead(pull, in.Number)
		if err != nil {
			return err
		}
		if view.SHA != in.ExpectedHeadSHA || view.BaseSHA != in.ExpectedBaseSHA || view.ChangedFiles != *in.ExpectedChangedFiles {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		return nil
	}
	if err := readPinned(); err != nil {
		return Result{}, err
	}
	var response *github.Response
	files, err := githubRead(ctx, capability, func() ([]*github.CommitFile, *github.Response, error) {
		items, current, err := client.PullRequests.ListFiles(ctx, owner, repo, in.Number, &github.ListOptions{Page: in.Cursor, PerPage: in.Limit})
		response = current
		return items, current, err
	})
	if err != nil {
		return Result{}, err
	}
	remaining := *in.ExpectedChangedFiles - offset
	wantedCount := min(in.Limit, remaining)
	eof := len(files) == remaining
	if response == nil || len(files) != wantedCount || eof && response.NextPage != 0 || !eof && response.NextPage != in.Cursor+1 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	index, err := projectGitHubPullFiles(files)
	if err != nil {
		return Result{}, err
	}
	if err := readPinned(); err != nil {
		return Result{}, err
	}
	encoded, err := json.Marshal(index)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, "github-pull-files:"+strconv.Itoa(in.Number)+":"+in.ExpectedHeadSHA+":"+in.ExpectedBaseSHA+":"+strconv.Itoa(in.Cursor),
		githubPullFilePage{string(encoded), len(index), response.NextPage, in.ExpectedHeadSHA, in.ExpectedBaseSHA, *in.ExpectedChangedFiles, in.Limit, offset, eof})
}

// Список является указателем, не пакетным чтением всех полных описаний.
// Полное body возвращают отдельные read/create/update, без усечения текста.
type githubPullIndex struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Head   string `json:"head"`
	Base   string `json:"base"`
	SHA    string `json:"sha"`
	Draft  bool   `json:"draft"`
	URL    string `json:"url"`
}

func projectGitHubPullIndex(value *github.PullRequest) githubPullIndex {
	return githubPullIndex{value.GetNumber(), value.GetTitle(), value.GetState(), value.GetHead().GetRef(), value.GetBase().GetRef(), value.GetHead().GetSHA(), value.GetDraft(), value.GetHTMLURL()}
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
		return executeGitHubPullFileIndex(ctx, client, owner, repo, request, capability, in)
	case "github.pull_request.list":
		return githubCatalogPage(ctx, capability, request, in.Limit, in.Cursor, func() ([]githubPullIndex, *github.Response, error) {
			items, response, err := client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{State: in.State, Head: in.Head, Base: in.Base, ListOptions: options})
			views := make([]githubPullIndex, 0, len(items))
			for _, item := range items {
				views = append(views, projectGitHubPullIndex(item))
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
		view, err := projectGitHubPullRead(item, in.Number)
		if err != nil {
			return Result{}, err
		}
		return providerResult(request, "github-pull:"+strconv.Itoa(in.Number), view)
	case "github.pull_request.create", "github.pull_request.update":
		var item *github.PullRequest
		var response *github.Response
		var err error
		if request.Operation == "github.pull_request.create" {
			item, response, err = client.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{Title: github.Ptr(in.Title), Head: github.Ptr(in.Head), Base: github.Ptr(in.Base), Body: github.Ptr(in.Body), Draft: github.Ptr(in.Draft)})
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
