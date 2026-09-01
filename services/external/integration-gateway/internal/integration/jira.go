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

func (adapter *Adapter) testJira(ctx context.Context, request Request, configuration map[string]string) error {
	definition := adapter.definitions["jira"]
	capability, _ := definition.Capability(definition.Spec.HealthCheck.Operation)
	_, err := adapter.jiraJSON(ctx, request, capability, configuration, http.MethodGet,
		"/rest/api/3/project/"+url.PathEscape(configuration["project_key"]), nil, nil, "")
	return err
}

func (adapter *Adapter) executeJira(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	projectKey := configuration["project_key"]
	switch request.Operation {
	case "jira.project.read":
		body, err := adapter.jiraJSON(ctx, request, capability, configuration, http.MethodGet,
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
			Query string `json:"query"`
			Limit int64  `json:"limit"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		if input.Limit == 0 {
			input.Limit = 20
		}
		jql := fmt.Sprintf(`project = "%s" AND (%s)`, strings.ReplaceAll(projectKey, `"`, `\"`), input.Query)
		query := url.Values{"jql": {jql}, "maxResults": {strconv.FormatInt(input.Limit, 10)}, "fields": {"summary,status"}}
		body, err := adapter.jiraJSON(ctx, request, capability, configuration, http.MethodGet, "/rest/api/3/search/jql", query, nil, "")
		if err != nil {
			return Result{}, err
		}
		var provider struct {
			Issues []jiraIssue `json:"issues"`
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
		return providerResult(request, "jira-search:"+request.EffectKey, map[string]any{"count": len(projection), "issues": string(encoded)})
	case "jira.issue.read":
		var input struct {
			IssueKey string `json:"issue_key"`
		}
		if decodeProviderJSON(canonicalInput, &input) != nil || !jiraIssueInProject(input.IssueKey, projectKey) {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		query := url.Values{"fields": {"summary,status,description"}}
		body, err := adapter.jiraJSON(ctx, request, capability, configuration, http.MethodGet,
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
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

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
	body, err := adapter.jiraJSON(ctx, request, capability, configuration, http.MethodGet, "/rest/api/3/search/jql", query, nil, "")
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
	body, err = adapter.jiraJSON(ctx, request, capability, configuration, http.MethodPost, "/rest/api/3/issue", nil,
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

func (adapter *Adapter) writeJiraComment(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (Result, error) {
	var input struct {
		IssueKey string `json:"issue_key"`
		Body     string `json:"body"`
	}
	if decodeProviderJSON(canonicalInput, &input) != nil || !jiraIssueInProject(input.IssueKey, configuration["project_key"]) {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	text := input.Body + "\n\n[kodex-effect:" + request.EffectKey + "]"
	body, err := adapter.jiraJSON(ctx, request, capability, configuration, http.MethodPost,
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
	_, err := adapter.jiraJSON(ctx, request, capability, configuration, http.MethodPut,
		"/rest/api/3/issue/"+url.PathEscape(input.IssueKey), nil, map[string]any{"fields": fields}, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	return providerResult(request, "jira-issue:"+input.IssueKey, map[string]any{"key": input.IssueKey, "updated": true})
}

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
	_, err := adapter.jiraJSON(ctx, request, capability, configuration, http.MethodPost, "/rest/api/3/issueLink", nil,
		map[string]any{"type": map[string]string{"name": input.LinkType}, "inwardIssue": map[string]string{"key": input.InwardIssueKey}, "outwardIssue": map[string]string{"key": input.OutwardIssueKey}}, request.EffectKey)
	if err != nil {
		return Result{}, err
	}
	return providerResult(request, "jira-link:"+request.EffectKey, map[string]any{
		"inward_issue_key": input.InwardIssueKey, "outward_issue_key": input.OutwardIssueKey, "linked": true,
	})
}

func (adapter *Adapter) jiraJSON(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, method, path string, query url.Values, body any, effectKey string) ([]byte, error) {
	return adapter.callProvider(ctx, providerCall{
		BaseURL: configuration["base_url"], Method: method, Path: path, Query: query, Body: body,
		AuthScheme: configuration["auth_scheme"], Username: configuration["username"], Credential: request.Credential,
		EffectKey: effectKey, Capability: capability,
	})
}

func jiraIssueInProject(issueKey, projectKey string) bool {
	return strings.HasPrefix(issueKey, projectKey+"-") && len(issueKey) > len(projectKey)+1
}

func jiraADF(text string) map[string]any {
	return map[string]any{"type": "doc", "version": 1, "content": []any{map[string]any{
		"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}},
	}}}
}

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
