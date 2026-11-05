package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// jiraServerIssue projects only the Server/DC fields needed for exact project eligibility and bounded output.
// jiraServerIssue хранит лишь Server/DC поля для точного допуска проекта и ограниченного результата.
type jiraServerIssue struct {
	ID, Key string
	Fields  struct {
		Summary     string
		Description *string
		Labels      []string
		Project     struct{ Key string }
		Status      struct{ Name string }
	}
}

// jiraServerSearchPage requires Server offset metadata; Cloud nextPageToken cannot serve as a substitute.
// jiraServerSearchPage требует Server offset metadata; Cloud nextPageToken не заменяет их.
type jiraServerSearchPage struct {
	StartAt, MaxResults, Total *int
	Issues                     []jiraServerIssue
}

// jiraServerTransition records the provider's available transition and whether unsupported additional fields are mandatory.
// jiraServerTransition хранит доступный переход и признак обязательных дополнительных полей, не поддержанных этим профилем.
type jiraServerTransition struct {
	ID, Name string
	Fields   map[string]struct{ Required bool }
}

// jiraServerProjectKey accepts ordinary Jira project keys before they are placed in fixed JQL literals or paths.
// jiraServerProjectKey проверяет обычный ключ проекта перед включением в фиксированный JQL literal или путь.
func jiraServerProjectKey(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] < 'A' || value[0] > 'Z' {
		return false
	}
	for _, character := range value {
		if !(character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_') {
			return false
		}
	}
	return true
}

// jiraServerLiteral escapes a bounded data literal and rejects control characters; it does not accept a JQL expression.
// jiraServerLiteral экранирует data literal и отклоняет управляющие символы; JQL-выражения не принимаются.
func jiraServerLiteral(value string) (string, bool) {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return "", false
		}
	}
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value), true
}

// jiraServerJSON builds only a fixed Server v2 path beneath the separately configured context, preserving credential/retry boundaries.
// jiraServerJSON строит фиксированный Server v2 путь под заданным context, сохраняя credential/retry boundaries.
func (adapter *Adapter) jiraServerJSON(ctx context.Context, request Request, capability integrationpackage.Capability,
	configuration map[string]string, method, path string, query url.Values, body any, effect string) ([]byte, error) {
	prefix, err := atlassianServerContext(configuration)
	if err != nil || !jiraServerProjectKey(configuration["project_key"]) {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	return adapter.configuredProviderJSON(ctx, request, capability, configuration, method, prefix+"/rest/api/2"+path, query, body, effect)
}

// jiraServerProject checks the real provider project key before exposing metadata or reporting connection health.
// jiraServerProject проверяет реальный ключ проекта перед выдачей metadata либо сообщением о здоровье подключения.
func (adapter *Adapter) jiraServerProject(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (map[string]any, error) {
	body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, "/project/"+url.PathEscape(configuration["project_key"]), nil, nil, "")
	if err != nil {
		return nil, err
	}
	var value struct{ ID, Key, Name string }
	if decodeProviderJSON(body, &value) != nil || value.ID == "" || value.Key != configuration["project_key"] || value.Name == "" {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return map[string]any{"id": value.ID, "key": value.Key, "name": value.Name}, nil
}

// readJiraServerIssue resolves one issue and verifies both returned key and project before a later write can use it.
// readJiraServerIssue разрешает задачу и проверяет возвращённые ключ и проект до последующей записи.
func (adapter *Adapter) readJiraServerIssue(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, issueKey string) (jiraServerIssue, error) {
	if !jiraIssueInProject(issueKey, configuration["project_key"]) {
		return jiraServerIssue{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, "/issue/"+url.PathEscape(issueKey),
		url.Values{"fields": {"project,summary,status,description,labels"}}, nil, "")
	if err != nil {
		return jiraServerIssue{}, err
	}
	var value jiraServerIssue
	if decodeProviderJSON(body, &value) != nil || value.ID == "" || value.Fields.Project.Key == "" {
		return jiraServerIssue{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if value.Key != issueKey || value.Fields.Project.Key != configuration["project_key"] {
		return jiraServerIssue{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	return value, nil
}

// jiraServerSearch fetches one explicitly bounded v2 page and verifies provider metadata and every returned project.
// jiraServerSearch получает одну ограниченную v2 страницу и проверяет metadata и проект каждой задачи.
func (adapter *Adapter) jiraServerSearch(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, jql string, offset, limit int) (jiraServerSearchPage, string, error) {
	query := url.Values{"jql": {jql}, "startAt": {strconv.Itoa(offset)}, "maxResults": {strconv.Itoa(limit)}, "fields": {"project,summary,status,description,labels"}}
	body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, "/search", query, nil, "")
	if err != nil {
		return jiraServerSearchPage{}, "", err
	}
	var page jiraServerSearchPage
	if decodeProviderJSON(body, &page) != nil || page.StartAt == nil || *page.StartAt != offset || page.MaxResults == nil ||
		*page.MaxResults < 1 || *page.MaxResults > limit || len(page.Issues) > *page.MaxResults || page.Total == nil {
		return page, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	next, err := atlassianServerNext(offset, len(page.Issues), limit, *page.Total)
	if err != nil {
		return page, "", err
	}
	seen := make(map[string]bool, len(page.Issues))
	for _, issue := range page.Issues {
		if issue.ID == "" || !jiraIssueInProject(issue.Key, configuration["project_key"]) || issue.Fields.Project.Key != configuration["project_key"] || seen[issue.Key] {
			return page, "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		seen[issue.Key] = true
	}
	return page, next, nil
}

// executeJiraServer exposes only the separately shipped limited Server/DC surface; it never falls back to a Cloud operation.
// executeJiraServer исполняет только отдельную ограниченную Server/DC поверхность и не делает fallback на Cloud.
func (adapter *Adapter) executeJiraServer(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, input []byte) (Result, error) {
	if _, err := atlassianServerContext(configuration); err != nil || !jiraServerProjectKey(configuration["project_key"]) {
		return Result{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	switch request.Operation {
	case "jira.identity.read":
		body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, "/myself", nil, nil, "")
		if err != nil {
			return Result{}, err
		}
		var value struct{ Key, Name, DisplayName string }
		if decodeProviderJSON(body, &value) != nil || value.Key == "" || value.Name == "" || value.DisplayName == "" {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "jira-identity:"+value.Key, map[string]any{"id": value.Key, "username": value.Name, "display_name": value.DisplayName})
	case "jira.project.read":
		value, err := adapter.jiraServerProject(ctx, request, capability, configuration)
		if err != nil {
			return Result{}, err
		}
		return providerResult(request, "jira-project:"+value["id"].(string), value)
	case "jira.issue.search":
		var value struct {
			Text, Cursor string
			Limit        int
		}
		if decodeProviderJSON(input, &value) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if value.Limit == 0 {
			value.Limit = 20
		}
		offset, err := atlassianServerOffset(value.Cursor)
		text, valid := jiraServerLiteral(value.Text)
		if err != nil || !valid || value.Limit < 1 || value.Limit > 50 {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		jql := fmt.Sprintf(`project = "%s" AND text ~ "%s" ORDER BY key ASC`, configuration["project_key"], text)
		page, next, err := adapter.jiraServerSearch(ctx, request, capability, configuration, jql, offset, value.Limit)
		if err != nil {
			return Result{}, err
		}
		issues := make([]map[string]string, 0, len(page.Issues))
		for _, issue := range page.Issues {
			if issue.Fields.Summary == "" || issue.Fields.Status.Name == "" {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			issues = append(issues, map[string]string{"key": issue.Key, "summary": issue.Fields.Summary, "status": issue.Fields.Status.Name})
		}
		encoded, _ := json.Marshal(issues)
		output := map[string]any{"issues": string(encoded), "count": len(issues)}
		if next != "" {
			output["next_cursor"] = next
		}
		return providerResult(request, "jira-search:"+request.EffectKey, output)
	case "jira.issue.read":
		var value struct {
			IssueKey string `json:"issue_key"`
		}
		if decodeProviderJSON(input, &value) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		issue, err := adapter.readJiraServerIssue(ctx, request, capability, configuration, value.IssueKey)
		if err != nil {
			return Result{}, err
		}
		output := map[string]any{"key": issue.Key, "summary": issue.Fields.Summary, "status": issue.Fields.Status.Name}
		if issue.Fields.Description != nil && *issue.Fields.Description != "" {
			output["description"] = *issue.Fields.Description
		}
		return providerResult(request, "jira-issue:"+issue.Key, output)
	case "jira.issue.create":
		return adapter.createJiraServerIssue(ctx, request, capability, configuration, input)
	case "jira.issue.comment.write":
		var value struct {
			IssueKey string `json:"issue_key"`
			Body     string
		}
		if decodeProviderJSON(input, &value) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if _, err := adapter.readJiraServerIssue(ctx, request, capability, configuration, value.IssueKey); err != nil {
			return Result{}, err
		}
		body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodPost, "/issue/"+url.PathEscape(value.IssueKey)+"/comment", nil,
			map[string]string{"body": value.Body + "\n\n[kodex-effect:" + request.EffectKey + "]"}, request.EffectKey)
		if err != nil {
			return Result{}, err
		}
		var created struct{ ID string }
		if decodeProviderJSON(body, &created) != nil || created.ID == "" {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "jira-comment:"+created.ID, map[string]any{"comment_id": created.ID, "issue_key": value.IssueKey})
	case "jira.issue.transition.list", "jira.issue.transition.apply":
		return adapter.jiraServerTransitions(ctx, request, capability, configuration, input)
	default:
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

// createJiraServerIssue reconciles one exact effect label and payload before a single plaintext v2 create.
// createJiraServerIssue сверяет единственную точную метку эффекта и payload перед одним plaintext v2 create.
func (adapter *Adapter) createJiraServerIssue(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, input []byte) (Result, error) {
	var value struct{ Summary, Description string }
	if decodeProviderJSON(input, &value) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	label := "kodex-effect-" + request.EffectKey
	escaped, valid := jiraServerLiteral(label)
	if !valid || len(label) > 255 || strings.ContainsRune(label, ' ') {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	jql := fmt.Sprintf(`project = "%s" AND labels = "%s"`, configuration["project_key"], escaped)
	page, _, err := adapter.jiraServerSearch(ctx, request, capability, configuration, jql, 0, 2)
	if err != nil {
		return Result{}, err
	}
	if *page.Total > 1 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if len(page.Issues) == 1 {
		issue := page.Issues[0]
		description := ""
		if issue.Fields.Description != nil {
			description = *issue.Fields.Description
		}
		found := false
		for _, item := range issue.Fields.Labels {
			found = found || item == label
		}
		if !found || issue.Fields.Summary != value.Summary || description != value.Description {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "jira-issue:"+issue.Key, map[string]any{"id": issue.ID, "key": issue.Key})
	}
	fields := map[string]any{"project": map[string]string{"key": configuration["project_key"]},
		"issuetype": map[string]string{"name": configuration["issue_type"]}, "summary": value.Summary, "labels": []string{label}}
	if value.Description != "" {
		fields["description"] = value.Description
	}
	body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodPost, "/issue", nil, map[string]any{"fields": fields}, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	var created struct{ ID, Key string }
	if decodeProviderJSON(body, &created) != nil || created.ID == "" || !jiraIssueInProject(created.Key, configuration["project_key"]) {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, "jira-issue:"+created.Key, map[string]any{"id": created.ID, "key": created.Key})
}

// jiraServerTransitions resolves current project eligibility and available IDs; apply admits only status-only transitions.
// jiraServerTransitions проверяет проект и доступные IDs; apply допускает лишь переход без дополнительных обязательных полей.
func (adapter *Adapter) jiraServerTransitions(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, input []byte) (Result, error) {
	var value struct {
		IssueKey     string `json:"issue_key"`
		TransitionID string `json:"transition_id"`
	}
	if decodeProviderJSON(input, &value) != nil || request.Operation == "jira.issue.transition.apply" && !decimalID(value.TransitionID) {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if _, err := adapter.readJiraServerIssue(ctx, request, capability, configuration, value.IssueKey); err != nil {
		return Result{}, err
	}
	path := "/issue/" + url.PathEscape(value.IssueKey) + "/transitions"
	body, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodGet, path, url.Values{"expand": {"transitions.fields"}}, nil, "")
	if err != nil {
		return Result{}, err
	}
	var provider struct{ Transitions []jiraServerTransition }
	if decodeProviderJSON(body, &provider) != nil || len(provider.Transitions) > 100 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	items := make([]map[string]any, 0, len(provider.Transitions))
	seen, allowed := make(map[string]bool), false
	for _, transition := range provider.Transitions {
		if !decimalID(transition.ID) || transition.Name == "" || transition.Fields == nil || seen[transition.ID] {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		seen[transition.ID] = true
		required := false
		for _, field := range transition.Fields {
			required = required || field.Required
		}
		allowed = allowed || transition.ID == value.TransitionID && !required
		items = append(items, map[string]any{"id": transition.ID, "name": transition.Name, "requires_fields": required})
	}
	if request.Operation == "jira.issue.transition.list" {
		encoded, _ := json.Marshal(items)
		return providerResult(request, "jira-transitions:"+value.IssueKey, map[string]any{"items": string(encoded), "count": len(items)})
	}
	if !allowed {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if _, err := adapter.jiraServerJSON(ctx, request, capability, configuration, http.MethodPost, path, nil,
		map[string]any{"transition": map[string]string{"id": value.TransitionID}}, request.EffectKey); err != nil {
		return Result{}, err
	}
	return providerResult(request, "jira-transition:"+value.IssueKey+":"+value.TransitionID, map[string]any{"accepted": true})
}
