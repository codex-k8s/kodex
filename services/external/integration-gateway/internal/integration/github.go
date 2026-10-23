package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
)

// githubRead retries only eligible SDK read failures under the capability attempt/backoff policy and preserves local SafeError validation failures immediately.
// RU: githubRead повторяет только допустимые SDK-ошибки чтения по бюджету capability и сразу сохраняет локальные ошибки проверки SafeError.
// Rate-limit response metadata receives its own safe classification; callers must not consume the returned partial value when an error is present.
// RU: Метаданные ограничения частоты получают отдельную безопасную классификацию; вызывающий не должен использовать частичный результат при наличии ошибки.
func githubRead[T any](ctx context.Context, capability integrationpackage.Capability, call func() (T, *github.Response, error)) (T, error) {
	for attempt := 1; ; attempt++ {
		value, response, err := call()
		if err == nil {
			return value, nil
		}
		var safe *SafeError
		if errors.As(err, &safe) {
			// Локальная проверка ответа не является временной сетевой ошибкой.
			return value, safe
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

// githubRateLimited recognizes HTTP 429 or a 403 with exhausted-rate or Retry-After headers, not every forbidden response.
// RU: githubRateLimited распознаёт HTTP 429 либо 403 с заголовками исчерпанного лимита или Retry-After, а не любой запрет доступа.
func githubRateLimited(response *github.Response) bool {
	return response != nil && (response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden &&
		(response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != ""))
}

// githubMutationError preserves nil success but treats missing, server-error or nominally successful responses with SDK errors as potentially applied effects.
// RU: githubMutationError сохраняет nil-успех, но считает отсутствие ответа, server-ошибку либо формально успешный ответ с SDK-ошибкой возможно выполненным эффектом.
// Other failures use closed rate-limit/status classes; this classifier never retries a mutation.
// RU: Остальные ошибки используют закрытые rate-limit/status классы; классификатор не повторяет изменение.
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

// findGitHubIssueByMarker scans at most one hundred sequential hundred-item pages and returns the first non-pull-request issue whose body contains the effect marker.
// RU: findGitHubIssueByMarker просматривает не более ста последовательных страниц по сто элементов и возвращает первый обычный issue с effect-маркером в body.
// Confirmed exhaustion returns nil; a skipped next page or exhausted search budget fails closed instead of proving absence or uniqueness.
// RU: Подтверждённый конец возвращает nil; пропуск страницы либо исчерпание бюджета закрыто отклоняются, не доказывая отсутствия или уникальности.
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

// githubIssueMatchesUpdate accepts only a non-pull-request issue matching every requested nonempty field; omitted fields remain unconstrained.
// RU: githubIssueMatchesUpdate принимает только обычный issue с совпадением каждого заданного непустого поля; пропущенные поля не ограничиваются.
func githubIssueMatchesUpdate(issue *github.Issue, title, body, state string) bool {
	return issue != nil && !issue.IsPullRequest() && (title == "" || issue.GetTitle() == title) &&
		(body == "" || issue.GetBody() == body) && (state == "" || issue.GetState() == state)
}

// listGitHubIssues applies the existing open/first-page/thirty-item defaults, reads one repository page and excludes pull requests from the safe issue index.
// RU: listGitHubIssues применяет прежние defaults open, первой страницы и тридцати элементов, читает страницу репозитория и исключает pull request из безопасного issue-индекса.
// Positive issue identity is required; the returned JSON string and optional next cursor do not claim full bodies or complete repository enumeration.
// RU: Требуется положительная идентичность issue; JSON-строка и необязательный next cursor не означают полные body либо перечисление всего репозитория.
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

// readGitHubIssue fetches the claimed repository issue number through bounded read retries, rejects a mismatched number and projects an optional full body.
// RU: readGitHubIssue читает назначенный номер issue репозитория с ограниченными повторами, отклоняет несовпавший номер и проецирует необязательный полный body.
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

// createGitHubIssueComment first verifies the selected ordinary issue, then searches bounded sequential comment pages for the effect marker before creating once.
// RU: createGitHubIssueComment сначала проверяет выбранный обычный issue, затем ищет effect-маркер на ограниченных последовательных страницах комментариев до единственного создания.
// An uncertain create receives read-only marker reconciliation; the result requires a positive comment ID and never triggers a blind second creation.
// RU: Неопределённое создание получает read-only поиск маркера; результат требует положительного comment ID и не запускает слепое повторное создание.
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
