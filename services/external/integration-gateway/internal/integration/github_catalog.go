package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

// githubCatalogInput decodes the registered GitHub catalog's operation-specific fields, including explicit pointer-valued optional limits/pins.
// It is a transport shape, not a grant; package schemas and selected operation handlers enforce field eligibility and scope separately.
// githubCatalogInput декодирует поля зарегистрированного каталога GitHub, включая указатели для явно заданных необязательных лимитов и привязок.
// Это форма транспорта, а не grant; допустимость полей и область отдельно проверяют схемы package и обработчики выбранных операций.
type githubCatalogInput struct {
	Path                  string `json:"path"`
	Ref                   string `json:"ref"`
	SHA                   string `json:"sha"`
	Branch                string `json:"branch"`
	Content               string `json:"content_base64"`
	Message               string `json:"message"`
	Title                 string `json:"title"`
	Body                  string `json:"body"`
	State                 string `json:"state"`
	Head                  string `json:"head"`
	Base                  string `json:"base"`
	Draft                 bool   `json:"draft"`
	Event                 string `json:"event"`
	MergeMethod           string `json:"merge_method"`
	Number                int    `json:"pull_request_number"`
	IssueNumber           int    `json:"issue_number"`
	CommentID             int64  `json:"comment_id"`
	ReviewID              int64  `json:"review_id"`
	CheckID               int64  `json:"check_run_id"`
	WorkflowID            int64  `json:"workflow_id"`
	WorkflowInputs        string `json:"workflow_inputs"`
	RunID                 int64  `json:"run_id"`
	JobID                 int64  `json:"job_id"`
	Limit                 int    `json:"limit"`
	Cursor                int    `json:"cursor"`
	OffsetBytes           int64  `json:"offset_bytes"`
	MaximumBytes          *int   `json:"maximum_bytes"`
	ExpectedSHA           string `json:"expected_sha"`
	ExpectedCatalogDigest string `json:"expected_catalog_digest"`
	ExpectedHeadSHA       string `json:"expected_head_sha"`
	ExpectedBaseSHA       string `json:"expected_base_sha"`
	ExpectedChangedFiles  *int   `json:"expected_changed_files"`
}

// githubContentView carries compact repository-entry metadata without embedding file contents or conferring access to that path.
// githubContentView переносит компактные метаданные записи репозитория без содержимого файла или выдачи доступа к пути.
type githubContentView struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int    `json:"size"`
}

// githubBranchView projects a branch name, commit SHA and protection flag rather than exposing the complete SDK object.
// githubBranchView проецирует имя ветки, SHA коммита и флаг защиты вместо выдачи полного SDK-объекта.
type githubBranchView struct {
	Name      string `json:"name"`
	SHA       string `json:"sha"`
	Protected bool   `json:"protected"`
}

// githubCommitView holds the commit identity/message/URL used by list/read projections; URL is output metadata, not a new fetch target.
// githubCommitView хранит идентичность, сообщение и URL коммита для проекций списка и чтения; URL — метаданные ответа, не новое назначение запроса.
type githubCommitView struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	URL     string `json:"url"`
}

// githubFileChange models commit-file metadata and an optional provider patch; callers choose which operation may expose that data.
// githubFileChange описывает метаданные файла коммита и необязательный patch провайдера; допустимость выдачи определяет вызывающая операция.
type githubFileChange struct {
	Filename         string  `json:"filename"`
	PreviousFilename string  `json:"previous_filename,omitempty"`
	SHA              string  `json:"sha"`
	Status           string  `json:"status"`
	Additions        int     `json:"additions"`
	Deletions        int     `json:"deletions"`
	Patch            *string `json:"patch,omitempty"`
}

// projectGitHubFiles rejects nil entries and unsafe current/previous paths, then copies the selected SDK fields into independent values.
// It does not independently verify blob SHA or patch completeness and does not retrieve file contents.
// projectGitHubFiles отклоняет nil-записи и небезопасные текущие и прежние пути, затем копирует выбранные поля SDK в независимые значения.
// SHA blob и полнота patch независимо не проверяются, содержимое файлов не загружается.
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

// validRepositoryPath checks a lexical relative path without leading slash, empty/dot components, backslash or NUL/CR/LF.
// Empty input follows allowEmpty; this predicate performs no URL decoding, existence lookup or repository authorization.
// validRepositoryPath проверяет лексический относительный путь без начального слеша, пустых и точечных компонентов, обратного слеша или NUL, CR и LF.
// Пустой вход зависит от allowEmpty; декодирование URL, поиск существования и полномочия репозитория не проверяются.
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

// githubCatalogPage applies bounded read retry, checks returned count/NextPage and normalizes a nil SDK slice before shared projection.
// Only an exact next page within 10000 is accepted; provider-specific selection and upstream repository/capability scope are unchanged.
// githubCatalogPage применяет ограниченный повтор чтения, проверяет количество и NextPage ответа и нормализует nil-срез SDK до общей проекции.
// Принимается только точная следующая страница в пределах 10000; выбор данных провайдера и вышестоящая область репозитория и capability не меняются.
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
	return catalogPageProjection(request, page, items, next)
}

// executeGitHubCatalog dispatches pinned-repository content, branch and commit operations, delegating directory/content and collaboration paths explicitly.
// It preserves operation-specific defaults, selected response checks and unknown mutation outcomes; authority comes from upstream request validation.
// executeGitHubCatalog направляет операции содержимого, веток и коммитов закреплённого репозитория, явно делегируя каталог, чтение файлов и совместную работу.
// Сохраняются специфичные умолчания, выбранные проверки ответа и неизвестные исходы изменений; полномочия происходят из вышестоящей проверки запроса.
func (adapter *Adapter) executeGitHubCatalog(ctx context.Context, client *github.Client, owner, repo string, request Request, capability integrationpackage.Capability, raw []byte) (Result, error) {
	var in githubCatalogInput
	if json.Unmarshal(raw, &in) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	// Cursor каталога — offset, не provider page; ноль сохраняется.
	if request.Operation == "github.repository.content.list" {
		return executeGitHubDirectoryList(ctx, client, owner, repo, request, capability, in)
	}
	if in.Limit == 0 {
		in.Limit = 20
		if request.Operation == "github.pull_request.file.list" {
			in.Limit = maximumGitHubPullFilePageSize
		}
	}
	if in.Cursor == 0 {
		in.Cursor = 1
	}
	options := github.ListOptions{Page: in.Cursor, PerPage: in.Limit}
	switch request.Operation {
	case "github.repository.content.read":
		return executeGitHubContentRead(ctx, client, owner, repo, request, capability, in)
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

// githubCommandResult applies mutation error/unknown-outcome classification before producing the existing accepted receipt projection.
// It does not independently reconcile an ambiguous provider result or retry the command.
// githubCommandResult классифицирует ошибку и неизвестный исход изменения до создания прежней проекции accepted и квитанции.
// Неопределённый исход провайдера независимо не сверяется и команда не повторяется.
func githubCommandResult(request Request, response *github.Response, err error) (Result, error) {
	if err := githubMutationError(response, err); err != nil {
		return Result{}, err
	}
	return providerResult(request, request.Operation+":"+request.EffectKey, struct {
		Accepted bool `json:"accepted"`
	}{true})
}
