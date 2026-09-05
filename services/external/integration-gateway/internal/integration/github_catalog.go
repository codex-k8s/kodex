package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

type githubCatalogInput struct {
	Path           string `json:"path"`
	Ref            string `json:"ref"`
	SHA            string `json:"sha"`
	Branch         string `json:"branch"`
	Content        string `json:"content_base64"`
	Message        string `json:"message"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	State          string `json:"state"`
	Head           string `json:"head"`
	Base           string `json:"base"`
	Event          string `json:"event"`
	MergeMethod    string `json:"merge_method"`
	Number         int    `json:"pull_request_number"`
	IssueNumber    int    `json:"issue_number"`
	CommentID      int64  `json:"comment_id"`
	ReviewID       int64  `json:"review_id"`
	CheckID        int64  `json:"check_run_id"`
	WorkflowID     int64  `json:"workflow_id"`
	WorkflowInputs string `json:"workflow_inputs"`
	RunID          int64  `json:"run_id"`
	JobID          int64  `json:"job_id"`
	Limit          int    `json:"limit"`
	Cursor         int    `json:"cursor"`
}

type githubContentView struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	SHA     string `json:"sha"`
	Size    int    `json:"size"`
	Content string `json:"content_base64,omitempty"`
}

type githubBranchView struct {
	Name      string `json:"name"`
	SHA       string `json:"sha"`
	Protected bool   `json:"protected"`
}

type githubCommitView struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	URL     string `json:"url"`
}

type githubFileChange struct {
	Filename         string  `json:"filename"`
	PreviousFilename string  `json:"previous_filename,omitempty"`
	SHA              string  `json:"sha"`
	Status           string  `json:"status"`
	Additions        int     `json:"additions"`
	Deletions        int     `json:"deletions"`
	Patch            *string `json:"patch,omitempty"`
}

func projectGitHubFiles(files []*github.CommitFile) ([]githubFileChange, error) {
	result := make([]githubFileChange, 0, len(files))
	for _, file := range files {
		if file == nil || !validRepositoryPath(file.GetFilename(), false) || file.GetPreviousFilename() != "" && !validRepositoryPath(file.GetPreviousFilename(), false) {
			return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		result = append(result, githubFileChange{file.GetFilename(), file.GetPreviousFilename(), file.GetSHA(), file.GetStatus(), file.GetAdditions(), file.GetDeletions(), file.Patch})
	}
	return result, nil
}

// SDK получает только проверенный путь внутри закреплённого репозитория.
func validRepositoryPath(value string, allowEmpty bool) bool {
	if value == "" {
		return allowEmpty
	}
	if strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func githubCatalogPage[T any](ctx context.Context, capability integrationpackage.Capability, request Request, limit, page int, call func() ([]T, *github.Response, error)) (Result, error) {
	next := 0
	items, err := githubRead(ctx, capability, func() ([]T, *github.Response, error) {
		items, response, err := call()
		if response != nil {
			next = response.NextPage
		}
		return items, response, err
	})
	if err != nil {
		return Result{}, err
	}
	if len(items) > limit || next != 0 && (next != page+1 || next > 10000) {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if items == nil {
		items = []T{}
	}
	body, err := json.Marshal(items)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, request.Operation+":"+strconv.Itoa(page), struct {
		Items string `json:"items"`
		Count int    `json:"count"`
		Next  int    `json:"next_cursor,omitempty"`
	}{string(body), len(items), next})
}

func (adapter *Adapter) executeGitHubCatalog(ctx context.Context, client *github.Client, owner, repo string, request Request, capability integrationpackage.Capability, raw []byte) (Result, error) {
	var in githubCatalogInput
	if json.Unmarshal(raw, &in) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Cursor == 0 {
		in.Cursor = 1
	}
	options := github.ListOptions{Page: in.Cursor, PerPage: in.Limit}
	switch request.Operation {
	case "github.repository.content.read", "github.repository.content.list":
		var directory []*github.RepositoryContent
		file, err := githubRead(ctx, capability, func() (*github.RepositoryContent, *github.Response, error) {
			file, entries, response, err := client.Repositories.GetContents(ctx, owner, repo, in.Path, &github.RepositoryContentGetOptions{Ref: in.Ref})
			directory = entries
			return file, response, err
		})
		if err != nil {
			return Result{}, err
		}
		if request.Operation == "github.repository.content.read" {
			if file == nil || file.GetPath() != in.Path || file.GetType() != "file" || file.GetEncoding() != "base64" || file.GetSize() > maximumResponseBytes {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			content, err := file.GetContent()
			if err != nil || len(content) != file.GetSize() {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			return providerResult(request, "github-content:"+file.GetSHA(), githubContentView{file.GetPath(), file.GetType(), file.GetSHA(), file.GetSize(), base64.StdEncoding.EncodeToString([]byte(content))})
		}
		if file != nil || len(directory) > 1000 {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		items := make([]githubContentView, 0, len(directory))
		for _, entry := range directory {
			prefix := in.Path
			if prefix != "" {
				prefix += "/"
			}
			if entry == nil || !strings.HasPrefix(entry.GetPath(), prefix) || !validRepositoryPath(entry.GetPath(), false) || strings.Contains(strings.TrimPrefix(entry.GetPath(), prefix), "/") {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			items = append(items, githubContentView{Path: entry.GetPath(), Type: entry.GetType(), SHA: entry.GetSHA(), Size: entry.GetSize()})
		}
		// Contents API не имеет pagination: выдаём ограниченный полный каталог.
		return githubCatalogPage(ctx, capability, request, 1000, 1, func() ([]githubContentView, *github.Response, error) { return items, nil, nil })
	case "github.repository.content.create", "github.repository.content.update", "github.repository.content.delete":
		content, err := base64.StdEncoding.Strict().DecodeString(in.Content)
		if err != nil || len(content) > maximumResponseBytes {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		options := &github.RepositoryContentFileOptions{Message: github.Ptr(in.Message), Content: content, Branch: github.Ptr(in.Branch)}
		if in.SHA != "" {
			options.SHA = github.Ptr(in.SHA)
		}
		var result *github.RepositoryContentResponse
		var response *github.Response
		switch request.Operation {
		case "github.repository.content.create":
			result, response, err = client.Repositories.CreateFile(ctx, owner, repo, url.PathEscape(in.Path), options)
		case "github.repository.content.update":
			result, response, err = client.Repositories.UpdateFile(ctx, owner, repo, url.PathEscape(in.Path), options)
		case "github.repository.content.delete":
			result, response, err = client.Repositories.DeleteFile(ctx, owner, repo, url.PathEscape(in.Path), options)
		}
		if err := githubMutationError(response, err); err != nil {
			return Result{}, err
		}
		if result == nil || result.Commit.GetSHA() == "" {
			return Result{}, &UnknownOutcomeError{}
		}
		return providerResult(request, "github-commit:"+result.Commit.GetSHA(), struct {
			SHA  string `json:"sha"`
			Path string `json:"path"`
		}{result.Commit.GetSHA(), in.Path})
	case "github.branch.list":
		return githubCatalogPage(ctx, capability, request, in.Limit, in.Cursor, func() ([]githubBranchView, *github.Response, error) {
			branches, response, err := client.Repositories.ListBranches(ctx, owner, repo, &github.BranchListOptions{ListOptions: options})
			views := make([]githubBranchView, 0, len(branches))
			for _, branch := range branches {
				views = append(views, githubBranchView{branch.GetName(), branch.GetCommit().GetSHA(), branch.GetProtected()})
			}
			return views, response, err
		})
	case "github.branch.read":
		branch, err := githubRead(ctx, capability, func() (*github.Branch, *github.Response, error) {
			return client.Repositories.GetBranch(ctx, owner, repo, in.Branch, 0)
		})
		if err != nil {
			return Result{}, err
		}
		if branch == nil || branch.GetName() != in.Branch || branch.GetCommit().GetSHA() == "" {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "github-branch:"+branch.GetName(), githubBranchView{branch.GetName(), branch.GetCommit().GetSHA(), branch.GetProtected()})
	case "github.branch.create":
		ref, response, err := client.Git.CreateRef(ctx, owner, repo, &github.Reference{Ref: github.Ptr("refs/heads/" + in.Branch), Object: &github.GitObject{SHA: github.Ptr(in.SHA)}})
		if err := githubMutationError(response, err); err != nil {
			return Result{}, err
		}
		if ref == nil || ref.GetRef() != "refs/heads/"+in.Branch || ref.GetObject().GetSHA() != in.SHA {
			return Result{}, &UnknownOutcomeError{}
		}
		return providerResult(request, "github-branch:"+in.Branch, githubBranchView{Name: in.Branch, SHA: in.SHA})
	case "github.branch.delete":
		response, err := client.Git.DeleteRef(ctx, owner, repo, "heads/"+in.Branch)
		return githubCommandResult(request, response, err)
	case "github.commit.list":
		return githubCatalogPage(ctx, capability, request, in.Limit, in.Cursor, func() ([]githubCommitView, *github.Response, error) {
			commits, response, err := client.Repositories.ListCommits(ctx, owner, repo, &github.CommitsListOptions{SHA: in.Ref, Path: in.Path, ListOptions: options})
			views := make([]githubCommitView, 0, len(commits))
			for _, commit := range commits {
				views = append(views, githubCommitView{commit.GetSHA(), commit.GetCommit().GetMessage(), commit.GetHTMLURL()})
			}
			return views, response, err
		})
	case "github.commit.read":
		next := 0
		commit, err := githubRead(ctx, capability, func() (*github.RepositoryCommit, *github.Response, error) {
			commit, response, err := client.Repositories.GetCommit(ctx, owner, repo, url.PathEscape(in.Ref), &options)
			if response != nil {
				next = response.NextPage
			}
			return commit, response, err
		})
		if err != nil {
			return Result{}, err
		}
		if commit == nil || commit.GetSHA() == "" {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		files, err := projectGitHubFiles(commit.Files)
		if err != nil || len(files) > in.Limit || next != 0 && (next != in.Cursor+1 || next > 10000) {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		encoded, err := json.Marshal(files)
		if err != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "github-commit:"+commit.GetSHA(), struct {
			SHA     string `json:"sha"`
			Message string `json:"message"`
			URL     string `json:"url"`
			Files   string `json:"files"`
			Count   int    `json:"count"`
			Next    int    `json:"next_cursor,omitempty"`
		}{commit.GetSHA(), commit.GetCommit().GetMessage(), commit.GetHTMLURL(), string(encoded), len(files), next})
	default:
		return adapter.executeGitHubCollaboration(ctx, client, owner, repo, request, capability, in, options)
	}
}

func githubCommandResult(request Request, response *github.Response, err error) (Result, error) {
	if err := githubMutationError(response, err); err != nil {
		return Result{}, err
	}
	return providerResult(request, request.Operation+":"+request.EffectKey, struct {
		Accepted bool `json:"accepted"`
	}{true})
}
