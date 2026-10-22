package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// jiraIssue decodes only the issue identity and summary/description/status fields required by this adapter's projections.
// Presence of these fields does not establish project scope; callers compare the returned key separately.
// jiraIssue декодирует лишь идентичность задачи и поля summary, description и status, нужные проекциям адаптера.
// Наличие полей не устанавливает область проекта; возвращённый ключ отдельно сравнивает вызывающий.
type jiraIssue struct {
	ID, Key string
	Fields  struct {
		Summary     string          `json:"summary"`
		Description json.RawMessage `json:"description"`
		Status      struct {
			Name string `json:"name"`
		} `json:"status"`
	} `json:"fields"`
}

// testJira validates the pinned definition and probes the configured project endpoint with its registered health capability.
// It preserves the provider error and does not mutate issues or infer a grant from a successful HTTP response.
// testJira проверяет закреплённое определение и обращается к endpoint настроенного проекта с зарегистрированной health capability.
// Ошибка провайдера сохраняется; задачи не меняются и успешный HTTP-ответ не трактуется как grant.
func (adapter *Adapter) testJira(ctx context.Context, request Request, configuration map[string]string) error {
	definition, err := adapter.validateDefinition(request)
	if err != nil {
		return err
	}
	capability, _ := definition.Capability(definition.Spec.HealthCheck.Operation)
	_, err = adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodGet,
		"/rest/api/3/project/"+url.PathEscape(configuration["project_key"]), nil, nil, "")
	return err
}

// executeJira dispatches project/issue read, scoped JQL search and selected writes, delegating additional operations to the closed catalog.
// Callers already validate package/capability/input authority; local result checks bind projected issue keys to the configured project.
// executeJira направляет чтение проекта и задач, ограниченный проектом JQL и выбранные записи, передавая прочие операции закрытому каталогу.
// Вызывающий уже проверяет полномочия package, capability и входа; локальные проверки связывают ключи выдаваемых задач с настроенным проектом.
func (adapter *Adapter) executeJira(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	projectKey := configuration["project_key"]
	switch request.Operation {
	case "jira.project.read":
		body, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodGet,
			"/rest/api/3/project/"+url.PathEscape(projectKey), nil, nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider struct{ ID, Key, Name string }
		if decodeProviderJSON(body, &provider) != nil || provider.ID == "" || provider.Key != projectKey {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "jira-project:"+provider.ID, map[string]any{"id": provider.ID, "key": provider.Key, "name": provider.Name})
	case "jira.issue.search":
		var input struct {
			Query  string `json:"query"`
			Cursor string `json:"cursor"`
			Limit  int64  `json:"limit"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if input.Limit == 0 {
			input.Limit = 20
		}
		jql, valid := scopedJiraQuery(projectKey, input.Query)
		if !valid {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		query := url.Values{"jql": {jql}, "maxResults": {strconv.FormatInt(input.Limit, 10)}, "fields": {"summary,status"}}
		if input.Cursor != "" {
			query.Set("nextPageToken", input.Cursor)
		}
		body, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodGet, "/rest/api/3/search/jql", query, nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider struct {
			Issues        []jiraIssue `json:"issues"`
			NextPageToken string      `json:"nextPageToken"`
		}
		if decodeProviderJSON(body, &provider) != nil || int64(len(provider.Issues)) > input.Limit {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		projection := make([]map[string]string, 0, len(provider.Issues))
		for _, issue := range provider.Issues {
			if !jiraIssueInProject(issue.Key, projectKey) {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			projection = append(projection, map[string]string{"key": issue.Key, "summary": issue.Fields.Summary, "status": issue.Fields.Status.Name})
		}
		encoded, _ := json.Marshal(projection)
		output := map[string]any{"count": len(projection), "issues": string(encoded)}
		if provider.NextPageToken != "" {
			output["next_cursor"] = provider.NextPageToken
		}
		return providerResult(request, "jira-search:"+request.EffectKey, output)
	case "jira.issue.read":
		var input struct {
			IssueKey string `json:"issue_key"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil || !jiraIssueInProject(input.IssueKey, projectKey) {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		query := url.Values{"fields": {"summary,status,description"}}
		body, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodGet,
			"/rest/api/3/issue/"+url.PathEscape(input.IssueKey), query, nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider jiraIssue
		if decodeProviderJSON(body, &provider) != nil || provider.Key != input.IssueKey {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		projection := map[string]any{"key": provider.Key, "summary": provider.Fields.Summary, "status": provider.Fields.Status.Name}
		if description := compactJSON(provider.Fields.Description); description != "" && description != "null" {
			projection["description"] = description
		}
		return providerResult(request, "jira-issue:"+provider.Key, projection)
	case "jira.issue.create":
		return adapter.createJiraIssue(ctx, request, capability, configuration, canonicalInput)
	case "jira.issue.comment.write":
		return adapter.writeJiraComment(ctx, request, capability, configuration, canonicalInput)
	case "jira.issue.update_limited":
		return adapter.updateJiraIssue(ctx, request, capability, configuration, canonicalInput)
	case "jira.issue.link.write":
		return adapter.linkJiraIssues(ctx, request, capability, configuration, canonicalInput)
	default:
		return adapter.executeJiraCatalog(ctx, request, capability, configuration, canonicalInput)
	}
}

// createJiraIssue first searches the project's effect label, reuses one matching result or posts a typed issue with that label.
// Multiple matches or an invalid created identity fail closed; label lookup is reconciliation, not native provider exactly-once support.
// createJiraIssue сначала ищет метку эффекта в проекте, переиспользует одно совпадение либо создаёт типизированную задачу с этой меткой.
// Несколько совпадений или неверная созданная идентичность дают закрытый отказ; поиск метки — сверка, а не нативная гарантия exactly-once провайдера.
func (adapter *Adapter) createJiraIssue(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	var input struct {
		Summary     string `json:"summary"`
		Description string `json:"description"`
	}
	if decodeProviderJSON(canonicalInput, &input) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	label := "kodex-effect-" + request.EffectKey
	jql := fmt.Sprintf(`project = "%s" AND labels = "%s"`, configuration["project_key"], label)
	query := url.Values{"jql": {jql}, "maxResults": {"2"}, "fields": {"summary,status"}}
	body, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodGet, "/rest/api/3/search/jql", query, nil, "")
	if err != nil {
		return Result{}, err
	}
	var existing struct {
		Issues []jiraIssue `json:"issues"`
	}
	if decodeProviderJSON(body, &existing) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if len(existing.Issues) == 1 && jiraIssueInProject(existing.Issues[0].Key, configuration["project_key"]) {
		return providerResult(request, "jira-issue:"+existing.Issues[0].Key, map[string]any{"id": existing.Issues[0].ID, "key": existing.Issues[0].Key})
	}
	if len(existing.Issues) > 1 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	fields := map[string]any{
		"project":   map[string]string{"key": configuration["project_key"]},
		"issuetype": map[string]string{"name": configuration["issue_type"]},
		"summary":   input.Summary,
		"labels":    []string{label},
	}
	if input.Description != "" {
		fields["description"] = jiraADF(input.Description)
	}
	body, err = adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodPost, "/rest/api/3/issue", nil,
		map[string]any{"fields": fields}, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var created struct{ ID, Key string }
	if decodeProviderJSON(body, &created) != nil || created.ID == "" || !jiraIssueInProject(created.Key, configuration["project_key"]) {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, "jira-issue:"+created.Key, map[string]any{"id": created.ID, "key": created.Key})
}

// writeJiraComment checks the issue-key project syntax, appends an effect marker in ADF and requires a returned comment ID.
// It performs no independent user approval or automatic mutation retry; those remain governed by the invocation/provider boundaries.
// writeJiraComment проверяет проектный синтаксис ключа задачи, добавляет метку эффекта в ADF и требует идентификатор ответа комментария.
// Независимое одобрение пользователя и автоматический повтор изменения не выполняются; ими управляют границы invocation и провайдера.
func (adapter *Adapter) writeJiraComment(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	var input struct {
		IssueKey string `json:"issue_key"`
		Body     string `json:"body"`
	}
	if decodeProviderJSON(canonicalInput, &input) != nil || !jiraIssueInProject(input.IssueKey, configuration["project_key"]) {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	text := input.Body + "\n\n[kodex-effect:" + request.EffectKey + "]"
	body, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodPost,
		"/rest/api/3/issue/"+url.PathEscape(input.IssueKey)+"/comment", nil, map[string]any{"body": jiraADF(text)}, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var created struct {
		ID string `json:"id"`
	}
	if decodeProviderJSON(body, &created) != nil || created.ID == "" {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, "jira-comment:"+created.ID, map[string]any{"comment_id": created.ID, "issue_key": input.IssueKey})
}

// updateJiraIssue permits a project-shaped key and at least one nonempty summary/description, then sends only those limited fields.
// This helper has no issue-version CAS; it preserves the caller's effect key and the provider's failure outcome.
// updateJiraIssue допускает ключ формы проекта и хотя бы одно непустое summary или description, затем передаёт только эти ограниченные поля.
// CAS версии задачи здесь нет; ключ эффекта вызывающего и исход ошибки провайдера сохраняются.
func (adapter *Adapter) updateJiraIssue(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	var input struct {
		IssueKey    string `json:"issue_key"`
		Summary     string `json:"summary"`
		Description string `json:"description"`
	}
	if decodeProviderJSON(canonicalInput, &input) != nil || !jiraIssueInProject(input.IssueKey, configuration["project_key"]) ||
		(input.Summary == "" && input.Description == "") {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	fields := map[string]any{}
	if input.Summary != "" {
		fields["summary"] = input.Summary
	}
	if input.Description != "" {
		fields["description"] = jiraADF(input.Description)
	}
	_, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodPut,
		"/rest/api/3/issue/"+url.PathEscape(input.IssueKey), nil, map[string]any{"fields": fields}, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	return providerResult(request, "jira-issue:"+input.IssueKey, map[string]any{"key": input.IssueKey, "updated": true})
}

// linkJiraIssues requires both issue keys to match the configured project and submits one typed link request with the retained effect key.
// Key syntax checks do not prove the issues exist; provider and upstream capability policy remain authoritative for the operation.
// linkJiraIssues требует принадлежность синтаксиса обоих ключей настроенному проекту и передаёт один типизированный запрос связи с прежним ключом эффекта.
// Синтаксис не доказывает существование задач; провайдер и вышестоящая политика capability остаются авторитетными для операции.
func (adapter *Adapter) linkJiraIssues(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	var input struct {
		InwardIssueKey  string `json:"inward_issue_key"`
		OutwardIssueKey string `json:"outward_issue_key"`
		LinkType        string `json:"link_type"`
	}
	if decodeProviderJSON(canonicalInput, &input) != nil || !jiraIssueInProject(input.InwardIssueKey, configuration["project_key"]) ||
		!jiraIssueInProject(input.OutwardIssueKey, configuration["project_key"]) {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	_, err := adapter.configuredProviderJSON(ctx, request, capability, configuration, http.MethodPost, "/rest/api/3/issueLink", nil,
		map[string]any{"type": map[string]string{"name": input.LinkType}, "inwardIssue": map[string]string{"key": input.InwardIssueKey}, "outwardIssue": map[string]string{"key": input.OutwardIssueKey}}, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	return providerResult(request, "jira-link:"+request.EffectKey, map[string]any{
		"inward_issue_key": input.InwardIssueKey, "outward_issue_key": input.OutwardIssueKey, "linked": true,
	})
}

// jiraIssueInProject accepts the exact project prefix and a nonzero decimal suffix without resolving the issue remotely.
// jiraIssueInProject допускает точный префикс проекта и ненулевой десятичный суффикс без удалённого поиска задачи.
func jiraIssueInProject(issueKey, projectKey string) bool {
	if !strings.HasPrefix(issueKey, projectKey+"-") {
		return false
	}
	number := strings.TrimPrefix(issueKey, projectKey+"-")
	if number == "" {
		return false
	}
	for _, value := range number {
		if value < '0' || value > '9' {
			return false
		}
	}
	return strings.Trim(number, "0") != ""
}

// jiraADF wraps plain text in one version-1 ADF document/paragraph/text node without interpreting markup or commands.
// jiraADF оборачивает обычный текст в один ADF-документ версии 1 с узлами paragraph и text без интерпретации разметки или команд.
func jiraADF(text string) map[string]any {
	return map[string]any{"type": "doc", "version": 1, "content": []any{map[string]any{
		"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}},
	}}}
}

// compactJSON removes insignificant JSON whitespace or returns empty for absent/invalid input, preserving scalar/null representation.
// compactJSON убирает незначимые пробелы JSON либо возвращает пустое значение для отсутствующего или неверного входа, сохраняя представление scalar и null.
func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buffer bytes.Buffer
	if json.Compact(&buffer, raw) != nil {
		return ""
	}
	return buffer.String()
}
