package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// sentryProject retains only canonical identity and display fields, excluding DSN, memberships and provider settings.
// sentryProject сохраняет только канонические идентификаторы и название, исключая DSN, участников и настройки провайдера.
type sentryProject struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Organization struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	} `json:"organization"`
}

// sentryIssue retains the project boundary on every detail/list/mutation response before producing a safe projection.
// sentryIssue сохраняет границу проекта в каждом ответе detail/list/mutation перед безопасной проекцией.
type sentryIssue struct {
	AssignedTo *struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"assignedTo"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Project struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	} `json:"project"`
}

// sentryDecimal admits canonical positive identifiers only; it cannot carry a path, query, selector or URL.
// sentryDecimal допускает только канонические положительные идентификаторы без пути, query, селектора или URL.
func sentryDecimal(value string) bool {
	if len(value) < 1 || len(value) > 32 || value[0] == '0' {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// sentryJSON selects the dedicated public CONNECT client for Cloud or the mandatory pinned native client for self-hosted; neither route can fall back to core/direct HTTPS.
// sentryJSON выбирает отдельный public CONNECT для Cloud либо обязательный pinned native для self-hosted; fallback к core/direct HTTPS отсутствует.
func (adapter *Adapter) sentryJSON(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, method, path string, query url.Values, body any) (*providerResponse, error) {
	client := adapter.openAPIHTTPClient
	if strings.HasPrefix(request.DefinitionKey, "sentry-self-hosted") {
		client = adapter.nativeHTTPClient
	}
	if client == nil {
		return nil, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	return adapter.callProviderResponse(ctx, providerCall{BaseURL: configuration["base_url"], Method: method, Path: path, Query: query, Body: body, AuthScheme: "BEARER", Credential: request.Credential, Capability: capability, Client: client, permissionEffect: request.permissionEffect})
}

// sentryReadProject checks the configured organization and project against current provider identity without projecting any credential-bearing settings.
// sentryReadProject сверяет выбранные организацию и проект с текущими данными провайдера без выдачи настроек с реквизитами.
func (adapter *Adapter) sentryReadProject(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (sentryProject, error) {
	path := "/api/0/projects/" + configuration["organization_slug"] + "/" + configuration["project_slug"] + "/"
	response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, path, nil, nil)
	if err != nil {
		return sentryProject{}, err
	}
	var project sentryProject
	if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &project) != nil || !sentryDecimal(project.ID) || !sentryDecimal(project.Organization.ID) || project.Slug != configuration["project_slug"] || project.Organization.Slug != configuration["organization_slug"] || project.Name == "" || len(project.Name) > 1000 {
		return sentryProject{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return project, nil
}

// sentryIssueMatches refuses missing/foreign project coordinates, unexpected identifiers and unsupported status values before data or effect evidence is returned.
// sentryIssueMatches отклоняет отсутствующие/чужие координаты проекта, неожиданные идентификаторы и неизвестные статусы до выдачи данных или evidence эффекта.
func sentryIssueMatches(issue sentryIssue, project sentryProject, expected string) bool {
	return sentryDecimal(issue.ID) && (expected == "" || issue.ID == expected) && issue.Project.ID == project.ID && issue.Project.Slug == project.Slug && issue.Title != "" && len(issue.Title) <= 2000 && (issue.Status == "resolved" || issue.Status == "unresolved" || issue.Status == "ignored")
}

// sentryReadIssue uses an organization-scoped fixed endpoint and then verifies project identity; a payload ID never establishes ownership.
// sentryReadIssue использует фиксированный endpoint организации и затем сверяет проект; идентификатор из payload не доказывает владение.
func (adapter *Adapter) sentryReadIssue(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, project sentryProject, issueID string) (sentryIssue, error) {
	response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, "/api/0/organizations/"+configuration["organization_slug"]+"/issues/"+issueID+"/", nil, nil)
	if err != nil {
		return sentryIssue{}, err
	}
	var issue sentryIssue
	if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &issue) != nil || !sentryIssueMatches(issue, project, issueID) {
		return sentryIssue{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return issue, nil
}

// sentryScopes reads the current authenticated token's own metadata under a fixed GET and rejects ambiguous or incomplete scope lists; it does not expose identity or scopes to the model.
// sentryScopes читает собственные metadata текущего токена фиксированным GET и отклоняет неоднозначный/неполный список; identity и scopes модели не выдаются.
func (adapter *Adapter) sentryScopes(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (bool, error) {
	metadata, err := adapter.sentryToken(ctx, request, capability, configuration)
	return sentryScopeAllows(metadata, request.Operation), err
}

// sentryTokenMetadata keeps current scope and user-presence observations inside the consumer; it carries no reusable authority or raw identity payload.
// sentryTokenMetadata сохраняет текущие scope и user-presence observations внутри consumer без многоразовой authority и raw identity payload.
type sentryTokenMetadata struct {
	Scopes map[string]bool
	UserID string
}

// sentryToken validates the fixed current-token metadata response before any provider permission is used; malformed or ambiguous fields never imply sufficient authority.
// sentryToken проверяет фиксированные metadata текущего токена до использования provider permissions; повреждённые и неоднозначные поля не означают достаточных полномочий.
func (adapter *Adapter) sentryToken(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (sentryTokenMetadata, error) {
	response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, "/api/0/", nil, nil)
	if err != nil {
		return sentryTokenMetadata{}, err
	}
	var metadata struct {
		Auth *struct {
			Scopes *[]string `json:"scopes"`
		} `json:"auth"`
		User *struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &metadata) != nil || metadata.Auth == nil || metadata.Auth.Scopes == nil || len(*metadata.Auth.Scopes) > 64 {
		return sentryTokenMetadata{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	result := sentryTokenMetadata{Scopes: map[string]bool{}}
	for _, scope := range *metadata.Auth.Scopes {
		if scope == "" || len(scope) > 100 || result.Scopes[scope] || strings.ContainsAny(scope, "\r\n\x00") {
			return sentryTokenMetadata{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		result.Scopes[scope] = true
	}
	if metadata.User != nil {
		if !sentryDecimal(metadata.User.ID) {
			return sentryTokenMetadata{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		result.UserID = metadata.User.ID
	}
	return result, nil
}

// sentryScopeAllows checks only the finite operation-to-provider-scope mapping; provider object checks and owner grants remain independently mandatory.
// sentryScopeAllows проверяет только конечное соответствие операции provider scope; проверки provider object и owner grants остаются независимо обязательными.
func sentryScopeAllows(metadata sentryTokenMetadata, operation string) bool {
	switch operation {
	case "sentry.issue.status.update", "sentry.issue.assignment.update":
		return metadata.Scopes["event:write"] || metadata.Scopes["event:admin"]
	case "sentry.organization.release.create", "sentry.organization.release.update":
		return metadata.Scopes["project:releases"] || metadata.Scopes["project:write"] || metadata.Scopes["project:admin"] || metadata.Scopes["org:ci"]
	case "sentry.organization.alert.create", "sentry.organization.alert.update", "sentry.organization.alert.delete":
		return metadata.Scopes["alerts:write"] || metadata.Scopes["org:read"] || metadata.Scopes["org:write"] || metadata.Scopes["org:admin"]
	case "sentry.issue.comment.create":
		return metadata.UserID != "" && (metadata.Scopes["event:write"] || metadata.Scopes["event:admin"])
	default:
		return false
	}
}

// sentryNextCursor extracts only bounded numeric cursor metadata after verifying the exact same origin/path; provider Link URLs are never followed.
// sentryNextCursor извлекает только ограниченный числовой cursor после сверки origin/path; URL из provider Link никогда не вызывается.
func sentryNextCursor(header http.Header, origin, path string) (string, error) {
	next := ""
	for _, part := range strings.Split(header.Get("Link"), ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		if !strings.Contains(part, `results="true"`) {
			continue
		}
		if next != "" {
			return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		start, end := strings.Index(part, "<"), strings.Index(part, ">")
		if start < 0 || end <= start {
			return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		target, err := url.Parse(part[start+1 : end])
		base, baseErr := url.Parse(origin)
		if err != nil || baseErr != nil || target.Scheme != base.Scheme || target.Host != base.Host || target.User != nil || target.Fragment != "" || target.Path != path || target.RawPath != "" {
			return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		next = target.Query().Get("cursor")
		if !sentryCursor(next) {
			return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	}
	return next, nil
}

// sentryCursor accepts only the provider's bounded decimal cursor triple and prevents URL/query injection or unlimited metadata.
// sentryCursor допускает только ограниченную десятичную тройку cursor, исключая URL/query injection и неограниченные metadata.
func sentryCursor(value string) bool {
	if len(value) < 5 || len(value) > 128 {
		return false
	}
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if len(part) < 1 || len(part) > 40 {
			return false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

// executeSentry executes the closed first vertical: project health, bounded issue list/detail, projected event detail and one approved issue-status mutation. Comments, assignment, releases and alerts require subsequent explicit contracts.
// executeSentry исполняет закрытую первую вертикаль: здоровье проекта, ограниченные list/detail ошибок, проекцию события и одно одобренное изменение статуса. Комментарии, назначения, релизы и alerts требуют следующих явных контрактов.
func (adapter *Adapter) executeSentry(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string) (Result, error) {
	if request.DefinitionKey == "sentry-organization" || request.DefinitionKey == "sentry-self-hosted-organization" {
		return adapter.executeSentryOrganization(ctx, request, capability, configuration)
	}
	if (request.DefinitionKey != "sentry" && request.DefinitionKey != "sentry-self-hosted") || !integrationpackage.ValidSentrySlug(configuration["organization_slug"]) || !integrationpackage.ValidSentrySlug(configuration["project_slug"]) {
		return Result{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	contract, exists := integrationpackage.CredentialResourcePermissionContractFor(request.DefinitionKey, request.CapabilityKey)
	if !exists || contract.Operation != request.Operation || request.PersonalCredentialAdmission && request.ResourcePermissionProfile != contract.Profile {
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	project, err := adapter.sentryReadProject(ctx, request, capability, configuration)
	if err != nil {
		return Result{}, err
	}
	var projection any
	providerRef := "sentry-project:" + project.ID
	switch request.Operation {
	case "sentry.project.read":
		projection = map[string]string{"id": project.ID, "slug": project.Slug, "name": project.Name, "organization_slug": project.Organization.Slug}
	case "sentry.issue.list":
		path := "/api/0/projects/" + configuration["organization_slug"] + "/" + configuration["project_slug"] + "/issues/"
		limit := 20
		if raw, ok := request.Input["limit"]; ok {
			switch value := raw.(type) {
			case float64:
				limit = int(value)
			case int:
				limit = value
			case json.Number:
				parsed, err := strconv.Atoi(string(value))
				if err != nil {
					return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
				}
				limit = parsed
			}
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		if value, ok := request.Input["query"].(string); ok {
			query.Set("query", value)
		}
		if value, ok := request.Input["cursor"].(string); ok {
			if !sentryCursor(value) {
				return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
			}
			query.Set("cursor", value)
		}
		response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, path, query, nil)
		if err != nil {
			return Result{}, err
		}
		var issues []sentryIssue
		if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &issues) != nil || issues == nil || len(issues) > limit {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		items := make([]map[string]string, 0, len(issues))
		seen := map[string]bool{}
		for _, issue := range issues {
			if !sentryIssueMatches(issue, project, "") || seen[issue.ID] {
				return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
			}
			seen[issue.ID] = true
			items = append(items, map[string]string{"id": issue.ID, "title": issue.Title, "status": issue.Status})
		}
		cursor, err := sentryNextCursor(response.Header, configuration["base_url"], path)
		if err != nil {
			return Result{}, err
		}
		if cursor != "" && cursor == query.Get("cursor") {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		projection = map[string]any{"items": items, "next_cursor": cursor}
	case "sentry.issue.read", "sentry.issue.status.update":
		issueID, _ := request.Input["issue_id"].(string)
		if !sentryDecimal(issueID) {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		issue, err := adapter.sentryReadIssue(ctx, request, capability, configuration, project, issueID)
		if err != nil {
			return Result{}, err
		}
		if request.Operation == "sentry.issue.status.update" {
			writable, err := adapter.sentryScopes(ctx, request, capability, configuration)
			if err != nil {
				return Result{}, err
			}
			if !writable {
				return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
			}
			status, _ := request.Input["status"].(string)
			response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodPut, "/api/0/organizations/"+configuration["organization_slug"]+"/issues/"+issueID+"/", nil, map[string]string{"status": status})
			if err != nil {
				return Result{}, err
			}
			issue = sentryIssue{}
			if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &issue) != nil || !sentryIssueMatches(issue, project, issueID) || issue.Status != status {
				return Result{}, &UnknownOutcomeError{stage: "response_body"}
			}
		}
		providerRef = "sentry-issue:" + issue.ID
		projection = map[string]string{"id": issue.ID, "title": issue.Title, "status": issue.Status, "project_id": project.ID}
	case "sentry.event.read":
		eventID, _ := request.Input["event_id"].(string)
		if len(eventID) != 32 {
			return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		for _, char := range eventID {
			if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
				return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
			}
		}
		response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, "/api/0/projects/"+configuration["organization_slug"]+"/"+configuration["project_slug"]+"/events/"+eventID+"/", nil, nil)
		if err != nil {
			return Result{}, err
		}
		var event struct {
			EventID   string `json:"eventID"`
			ProjectID string `json:"projectID"`
			Title     string `json:"title"`
			Platform  string `json:"platform"`
		}
		if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeProviderJSON(response.Body, &event) != nil || event.EventID != eventID || event.ProjectID != project.ID || event.Title == "" || len(event.Title) > 2000 || len(event.Platform) > 100 {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		providerRef = "sentry-event:" + event.EventID
		projection = map[string]string{"event_id": event.EventID, "project_id": event.ProjectID, "title": event.Title, "platform": event.Platform}
	default:
		projection, providerRef, err = adapter.sentryProjectResource(ctx, request, capability, configuration, project)
		if err != nil {
			return Result{}, err
		}
	}
	text, err := json.Marshal(projection)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, providerRef, map[string]string{"text": string(text)})
}
