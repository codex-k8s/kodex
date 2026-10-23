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

// githubPullView projects full pull-request text and branch identity for individual read or mutation results, not compact list entries.
// RU: githubPullView проецирует полный текст pull request и идентичность веток для отдельного чтения либо изменения, а не компактного списка.
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

// projectGitHubPull copies SDK getter values without adding read-only required fields; operation-specific callers validate the response first.
// RU: projectGitHubPull копирует значения SDK getters без дополнительных обязательных read-полей; ответ предварительно проверяет вызывающая операция.
func projectGitHubPull(value *github.PullRequest) githubPullView {
	return githubPullView{value.GetNumber(), value.GetTitle(), value.GetBody(), value.GetState(), value.GetHead().GetRef(), value.GetBase().GetRef(), value.GetHead().GetSHA(), value.GetDraft(), value.GetHTMLURL()}
}

// githubPullReadView adds exact base revision and changed-file count to a full read so subsequent file indexing can pin its source independently of mutation replies.
// RU: githubPullReadView добавляет точную base-ревизию и число изменённых файлов для закрепления источника последующего индекса независимо от mutation-ответов.
type githubPullReadView struct {
	githubPullView
	BaseSHA      string `json:"base_sha"`
	ChangedFiles int    `json:"changed_files"`
}

// projectGitHubPullRead requires the selected number, head/base commits, bounded explicit changed-file count and a known open/closed state before projecting a full read.
// RU: projectGitHubPullRead требует выбранный номер, head/base commits, явно заданное ограниченное число файлов и известное состояние open/closed до проекции полного чтения.
func projectGitHubPullRead(value *github.PullRequest, number int) (githubPullReadView, error) {
	if value == nil || value.GetNumber() != number || value.Head == nil || value.Base == nil ||
		!sourceCommitPattern.MatchString(value.Head.GetSHA()) || !sourceCommitPattern.MatchString(value.Base.GetSHA()) ||
		value.ChangedFiles == nil || value.GetChangedFiles() < 0 || value.GetChangedFiles() > 2147483647 ||
		(value.GetState() != "open" && value.GetState() != "closed") {
		return githubPullReadView{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return githubPullReadView{projectGitHubPull(value), value.Base.GetSHA(), value.GetChangedFiles()}, nil
}

// maximumGitHubPullFilePageSize keeps each pinned file-index response small; it is not the provider's general pagination maximum.
// RU: maximumGitHubPullFilePageSize удерживает закреплённую страницу file-индекса маленькой, а не задаёт общий предел пагинации провайдера.
const maximumGitHubPullFilePageSize = 4

// maximumGitHubPullFiles bounds the claimed complete pull-request file count accepted by the pinned index operation.
// RU: maximumGitHubPullFiles ограничивает заявленное полное число файлов pull request, допустимое операцией закреплённого индекса.
const maximumGitHubPullFiles = 3000

// maximumGitHubFilePathBytes bounds UTF-8 path bytes before repository-path and control-character checks.
// RU: maximumGitHubFilePathBytes ограничивает байты UTF-8 пути до проверки repository-path и управляющих символов.
const maximumGitHubFilePathBytes = 1024

// maximumGitHubFileChangeCount limits change counters to the largest exactly representable JSON/JavaScript safe integer.
// RU: maximumGitHubFileChangeCount ограничивает счётчики изменений максимальным точно представимым безопасным целым JSON/JavaScript.
const maximumGitHubFileChangeCount = int64(9007199254740991)

// githubPullFileIndex contains validated file metadata, not a loaded patch or file content; nullable SHA preserves an absent provider blob identity.
// RU: githubPullFileIndex содержит проверенные метаданные файла, а не загруженный patch либо содержимое; nullable SHA сохраняет отсутствие provider blob-идентичности.
type githubPullFileIndex struct {
	Filename         string  `json:"filename"`
	PreviousFilename string  `json:"previous_filename,omitempty"`
	SHA              *string `json:"sha"`
	Status           string  `json:"status"`
	Additions        int     `json:"additions"`
	Deletions        int     `json:"deletions"`
}

// githubPullFilePage binds a serialized metadata page, offset and EOF to exact head/base revisions, total count and page size for continuation checks.
// RU: githubPullFilePage связывает сериализованную страницу метаданных, offset и EOF с точными head/base ревизиями, общим числом и размером страницы для продолжения.
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

// validGitHubIndexPath requires a bounded valid UTF-8 repository path without ASCII controls, with emptiness allowed only for explicitly optional metadata.
// RU: validGitHubIndexPath требует ограниченный корректный UTF-8 путь репозитория без ASCII control-символов, разрешая пустоту только явно необязательным метаданным.
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

// projectGitHubPullFiles validates every file's unique path, optional SHA, known status and nonnegative safe counters whose additions/deletions sum matches changes.
// RU: projectGitHubPullFiles проверяет уникальный путь, необязательный SHA, известный статус и безопасные неотрицательные счётчики с равенством additions+deletions=changes.
// Renames require a previous path; any invalid item rejects the whole page, while absent SHAs and empty valid pages remain explicit.
// RU: Переименование требует предыдущий путь; неверный элемент отклоняет всю страницу, а отсутствие SHA и корректная пустая страница сохраняются явно.
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

// executeGitHubPullFileIndex reads one small file-metadata page only when exact expected head, base and total pins match full pull-request reads both before and after listing.
// RU: executeGitHubPullFileIndex читает маленькую страницу file-метаданных только при совпадении ожидаемых head, base и total с полным PR-чтением до и после списка.
// Count, offset, EOF and sequential next-page checks reject truncated, stale or inconsistent pages without returning partial index data or patches.
// RU: Проверки count, offset, EOF и последовательной next page отклоняют усечённые, устаревшие либо несогласованные страницы без частичного индекса и patch.
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

// githubPullIndex is a compact list pointer without body; complete descriptions remain available through separate read/create/update operations without truncation.
// RU: githubPullIndex — компактный указатель списка без body; полные описания доступны отдельным read/create/update операциям без усечения.
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

// projectGitHubPullIndex copies only compact PR identity and branch metadata, deliberately excluding body rather than truncating the original description.
// RU: projectGitHubPullIndex копирует только компактную PR-идентичность и данные веток, намеренно исключая body вместо усечения исходного описания.
func projectGitHubPullIndex(value *github.PullRequest) githubPullIndex {
	return githubPullIndex{value.GetNumber(), value.GetTitle(), value.GetState(), value.GetHead().GetRef(), value.GetBase().GetRef(), value.GetHead().GetSHA(), value.GetDraft(), value.GetHTMLURL()}
}

// githubReviewView projects review identity, complete body, state and reviewed commit without exposing unrelated SDK metadata.
// RU: githubReviewView проецирует идентичность review, полный body, состояние и проверенный commit без посторонних SDK-метаданных.
type githubReviewView struct {
	ID       int64  `json:"id"`
	Body     string `json:"body"`
	State    string `json:"state"`
	CommitID string `json:"commit_id"`
}

// projectGitHubReview copies safe review fields through SDK getters; operation-specific identity and outcome checks remain with its caller.
// RU: projectGitHubReview копирует безопасные поля review через SDK getters; идентичность и результат операции проверяет вызывающий.
func projectGitHubReview(value *github.PullRequestReview) githubReviewView {
	return githubReviewView{value.GetID(), value.GetBody(), value.GetState(), value.GetCommitID()}
}

// githubCommentView contains only a comment's provider ID and full body after the operation has checked its selected issue association.
// RU: githubCommentView содержит только provider ID комментария и полный body после проверки связи с выбранным issue в соответствующей операции.
type githubCommentView struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// executeGitHubCollaboration dispatches PR/file/review and issue-comment operations inside the validated repository, then delegates other known operations to automation.
// RU: executeGitHubCollaboration направляет PR/file/review и issue-comment операции внутри проверенного репозитория, передавая другие известные операции в automation.
// Reads use bounded retry and operation-specific projections; mutations run once, preserve uncertain outcomes and do not acquire owner approval here.
// RU: Чтения используют ограниченный повтор и проекции операции; изменения выполняются один раз, сохраняют неопределённость и не получают owner approval здесь.
// Individual comment reads or mutations verify the exact issue URL, and full PR reads retain stronger source pins than compact lists or mutation replies.
// RU: Отдельное чтение либо изменение комментария проверяет точный issue URL, а полный PR-read сохраняет более сильные source-привязки, чем список либо mutation-ответ.
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
