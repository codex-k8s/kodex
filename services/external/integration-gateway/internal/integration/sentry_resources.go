package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// sentryProviderID decodes the canonical string-or-integer ID shape used by Sentry release project metadata without passing through floating point.
// sentryProviderID разбирает канонический string-or-integer ID в metadata проектов release без преобразования через floating point.
type sentryProviderID string

// errSentryIdentifier is the fixed fail-closed diagnostic for an invalid provider ID.
// errSentryIdentifier задаёт фиксированный fail-closed diagnostic некорректного provider ID.
var errSentryIdentifier = errors.New("Sentry provider identifier is invalid")

// UnmarshalJSON rejects fractional, negative, exponent, null and noncanonical values before an identifier can establish membership.
// UnmarshalJSON отклоняет дробные, отрицательные, exponent, null и неканонические значения до использования идентификатора для membership.
func (value *sentryProviderID) UnmarshalJSON(raw []byte) error {
	text := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &text) != nil {
			return errSentryIdentifier
		}
	}
	if !sentryDecimal(text) {
		return errSentryIdentifier
	}
	*value = sentryProviderID(text)
	return nil
}

// sentryComment keeps comment text and author coordinates for bounded projection and exact current-user create readback, excluding names, email and mentions.
// sentryComment сохраняет текст и координаты автора для ограниченной проекции и exact current-user create readback, исключая имена, email и mentions.
type sentryComment struct {
	ID   string `json:"id"`
	Data struct {
		Text string `json:"text"`
	} `json:"data"`
	User *struct {
		ID string `json:"id"`
	} `json:"user"`
}

// sentryRelease retains the complete project membership only for eligibility, never for exposing other project identities through a project-scoped connection.
// sentryRelease сохраняет полный состав проектов только для допуска, не выдавая чужие project identities через project-scoped connection.
type sentryRelease struct {
	Version  string  `json:"version"`
	Ref      *string `json:"ref"`
	Status   string  `json:"status"`
	Projects []struct {
		ID   sentryProviderID `json:"id"`
		Slug string           `json:"slug"`
	} `json:"projects"`
}

// sentryAlert retains only public display/state fields after a separate authoritative project-scope lookup; provider configuration remains untrusted and unprojected.
// sentryAlert сохраняет только display/state после отдельного authoritative project-scope lookup; provider configuration остаётся недоверенной и не выдаётся.
type sentryAlert struct {
	OrganizationID string `json:"organizationId"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Enabled        *bool  `json:"enabled"`
}

// sentryPageQuery derives bounded per_page/cursor values from a validated invocation and never forwards a provider URL or arbitrary query parameter.
// sentryPageQuery выводит ограниченные per_page/cursor из проверенного invocation и не передаёт provider URL либо произвольные query parameters.
func sentryPageQuery(input map[string]any) (url.Values, int, error) {
	limit := 20
	if raw, present := input["limit"]; present {
		switch value := raw.(type) {
		case int:
			limit = value
		case float64:
			limit = int(value)
		case json.Number:
			parsed, err := strconv.Atoi(string(value))
			if err != nil {
				return nil, 0, &SafeError{Code: providerRequestRejectedCode}
			}
			limit = parsed
		default:
			return nil, 0, &SafeError{Code: providerRequestRejectedCode}
		}
	}
	if limit < 1 || limit > 50 {
		return nil, 0, &SafeError{Code: providerRequestRejectedCode}
	}
	query := url.Values{"per_page": {strconv.Itoa(limit)}}
	if cursor, present := input["cursor"].(string); present {
		if !sentryCursor(cursor) {
			return nil, 0, &SafeError{Code: providerRequestRejectedCode}
		}
		query.Set("cursor", cursor)
	}
	return query, limit, nil
}

// sentryListResult validates continuation without following its URL and rejects a repeated cursor instead of claiming a complete truncated page.
// sentryListResult проверяет continuation без перехода по URL и отклоняет повторный cursor вместо заявления о полной усечённой странице.
func sentryListResult(response *providerResponse, origin, path string, query url.Values, items any) (any, error) {
	cursor, err := sentryNextCursor(response.Header, origin, path)
	if err != nil || cursor != "" && cursor == query.Get("cursor") {
		return nil, &SafeError{Code: providerResponseInvalidCode}
	}
	return map[string]any{"items": items, "next_cursor": cursor}, nil
}

// sentryVersion admits only bounded literal release versions that cannot change the path structure or smuggle URL/selector syntax.
// sentryVersion допускает только ограниченные буквальные release versions без изменения структуры пути и URL/selector syntax.
func sentryVersion(value string) bool {
	if len(value) < 1 || len(value) > 128 || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._@+-", char)) {
			return false
		}
	}
	return true
}

// sentryReleaseMatches requires current selected-project membership and bounded metadata; shared release writes are deliberately absent from project capabilities.
// sentryReleaseMatches требует текущую принадлежность выбранному проекту и ограниченные metadata; запись общего release отсутствует среди project capabilities.
func sentryReleaseMatches(release sentryRelease, project sentryProject, version string) bool {
	if !sentryVersion(release.Version) || version != "" && release.Version != version || len(release.Projects) < 1 || len(release.Projects) > 100 || release.Ref != nil && len(*release.Ref) > 500 {
		return false
	}
	found := false
	seen := map[string]bool{}
	for _, member := range release.Projects {
		if !sentryDecimal(string(member.ID)) || !integrationpackage.ValidSentrySlug(member.Slug) || seen[string(member.ID)] {
			return false
		}
		seen[string(member.ID)] = true
		found = found || string(member.ID) == project.ID && member.Slug == project.Slug
	}
	return found
}

// sentryAlertProjectEligible resolves every workflow's complete project scope before returning project-filtered data; all-project, foreign and shared scopes are hidden consistently in list/detail paths.
// sentryAlertProjectEligible разрешает полную область workflow до project-filtered выдачи; all-project, чужие и общие области одинаково скрываются в list/detail.
func (adapter *Adapter) sentryAlertProjectEligible(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, project sentryProject, id string) (bool, error) {
	response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, "/api/0/organizations/"+configuration["organization_slug"]+"/workflows/"+id+"/project-scope/", nil, nil)
	if err != nil {
		return false, err
	}
	var scope struct {
		ProjectIDs  *[]string `json:"projectIds"`
		IncludesAll *bool     `json:"includesAllProjects"`
	}
	if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeTypedProviderJSON(response.Body, &scope) != nil || scope.ProjectIDs == nil || scope.IncludesAll == nil || len(*scope.ProjectIDs) > 100 {
		return false, &SafeError{Code: providerResponseInvalidCode}
	}
	return !*scope.IncludesAll && len(*scope.ProjectIDs) == 1 && (*scope.ProjectIDs)[0] == project.ID, nil
}

// sentryProjectResource executes the additional closed project reads and issue comment/assignment writes; shared release/workflow mutations require a separate explicit organization connection.
// sentryProjectResource исполняет дополнительные project reads и comment/assignment writes; изменения общих release/workflow требуют отдельного явного organization connection.
func (adapter *Adapter) sentryProjectResource(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, project sentryProject) (any, string, error) {
	orgPath := "/api/0/organizations/" + configuration["organization_slug"]
	projectPath := "/api/0/projects/" + configuration["organization_slug"] + "/" + configuration["project_slug"]
	providerRef := "sentry-project:" + project.ID
	switch request.Operation {
	case "sentry.issue.event.list", "sentry.issue.comment.list", "sentry.issue.comment.create", "sentry.issue.assignment.update":
		id, _ := request.Input["issue_id"].(string)
		if !sentryDecimal(id) {
			return nil, "", &SafeError{Code: providerRequestRejectedCode}
		}
		issue, err := adapter.sentryReadIssue(ctx, request, capability, configuration, project, id)
		if err != nil {
			return nil, "", err
		}
		path := orgPath + "/issues/" + id
		providerRef = "sentry-issue:" + id
		if request.Operation == "sentry.issue.assignment.update" || request.Operation == "sentry.issue.comment.create" {
			metadata, err := adapter.sentryToken(ctx, request, capability, configuration)
			if err != nil {
				return nil, "", err
			}
			if !sentryScopeAllows(metadata, request.Operation) {
				return nil, "", &SafeError{Code: providerRequestRejectedCode}
			}
			if request.Operation == "sentry.issue.assignment.update" {
				assignee, _ := request.Input["assignee"].(string)
				kind, actorID, valid := strings.Cut(assignee, ":")
				if assignee == "unassigned" {
					kind, actorID = "", ""
				} else if !valid || (kind != "user" && kind != "team") || !sentryDecimal(actorID) {
					return nil, "", &SafeError{Code: providerRequestRejectedCode}
				}
				target := assignee
				if assignee == "unassigned" {
					target = ""
				}
				response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodPut, path+"/", nil, map[string]string{"assignedTo": target})
				if err != nil {
					return nil, "", err
				}
				issue = sentryIssue{}
				if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeTypedProviderJSON(response.Body, &issue) != nil || !sentryIssueMatches(issue, project, id) || actorID == "" && issue.AssignedTo != nil || actorID != "" && (issue.AssignedTo == nil || issue.AssignedTo.ID != actorID || issue.AssignedTo.Type != kind) {
					return nil, "", &UnknownOutcomeError{stage: "response_body"}
				}
				return map[string]string{"id": id, "assignee": assignee}, providerRef, nil
			}
			text, _ := request.Input["text"].(string)
			response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodPost, path+"/comments/", nil, map[string]string{"text": text})
			if err != nil {
				return nil, "", err
			}
			var comment sentryComment
			if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil || decodeTypedProviderJSON(response.Body, &comment) != nil || !sentryDecimal(comment.ID) || comment.Data.Text != text || comment.User == nil || comment.User.ID != metadata.UserID {
				return nil, "", &UnknownOutcomeError{stage: "response_body"}
			}
			return map[string]string{"id": comment.ID, "text": comment.Data.Text}, "sentry-comment:" + comment.ID, nil
		}
		query, limit, err := sentryPageQuery(request.Input)
		if err != nil {
			return nil, "", err
		}
		suffix := "/comments/"
		if request.Operation == "sentry.issue.event.list" {
			suffix = "/events/"
		}
		path += suffix
		response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, path, query, nil)
		if err != nil {
			return nil, "", err
		}
		if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil {
			return nil, "", &SafeError{Code: providerResponseInvalidCode}
		}
		items := []map[string]string{}
		seen := map[string]bool{}
		if request.Operation == "sentry.issue.event.list" {
			var events []struct {
				EventID   string `json:"eventID"`
				ProjectID string `json:"projectID"`
				Title     string `json:"title"`
			}
			if decodeTypedProviderJSON(response.Body, &events) != nil || events == nil || len(events) > limit {
				return nil, "", &SafeError{Code: providerResponseInvalidCode}
			}
			for _, event := range events {
				if len(event.EventID) != 32 || event.ProjectID != project.ID || event.Title == "" || len(event.Title) > 2000 || seen[event.EventID] {
					return nil, "", &SafeError{Code: providerResponseInvalidCode}
				}
				seen[event.EventID] = true
				items = append(items, map[string]string{"event_id": event.EventID, "title": event.Title})
			}
		} else {
			var comments []sentryComment
			if decodeTypedProviderJSON(response.Body, &comments) != nil || comments == nil || len(comments) > limit {
				return nil, "", &SafeError{Code: providerResponseInvalidCode}
			}
			for _, comment := range comments {
				if !sentryDecimal(comment.ID) || len(comment.Data.Text) > 5000 || seen[comment.ID] {
					return nil, "", &SafeError{Code: providerResponseInvalidCode}
				}
				seen[comment.ID] = true
				items = append(items, map[string]string{"id": comment.ID, "text": comment.Data.Text})
			}
		}
		result, err := sentryListResult(response, configuration["base_url"], path, query, items)
		return result, providerRef, err
	case "sentry.release.list", "sentry.release.read":
		path := projectPath + "/releases/"
		query, limit, err := sentryPageQuery(request.Input)
		if err != nil {
			return nil, "", err
		}
		version := ""
		if request.Operation == "sentry.release.read" {
			version, _ = request.Input["version"].(string)
			if !sentryVersion(version) {
				return nil, "", &SafeError{Code: providerRequestRejectedCode}
			}
			path += url.PathEscape(version) + "/"
			query = nil
		}
		response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, path, query, nil)
		if err != nil {
			return nil, "", err
		}
		if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil {
			return nil, "", &SafeError{Code: providerResponseInvalidCode}
		}
		releases := []sentryRelease{}
		if version != "" {
			var release sentryRelease
			if decodeTypedProviderJSON(response.Body, &release) != nil {
				return nil, "", &SafeError{Code: providerResponseInvalidCode}
			}
			releases = append(releases, release)
		} else if decodeTypedProviderJSON(response.Body, &releases) != nil || releases == nil || len(releases) > limit {
			return nil, "", &SafeError{Code: providerResponseInvalidCode}
		}
		items := []map[string]string{}
		seen := map[string]bool{}
		for _, release := range releases {
			if !sentryReleaseMatches(release, project, version) || seen[release.Version] {
				return nil, "", &SafeError{Code: providerResponseInvalidCode}
			}
			seen[release.Version] = true
			item := map[string]string{"version": release.Version, "status": release.Status}
			if release.Ref != nil {
				item["ref"] = *release.Ref
			}
			items = append(items, item)
		}
		if version != "" {
			return items[0], "sentry-release:" + version, nil
		}
		result, err := sentryListResult(response, configuration["base_url"], path, query, items)
		return result, providerRef, err
	case "sentry.alert.list", "sentry.alert.read":
		path := orgPath + "/workflows/"
		query, limit, err := sentryPageQuery(request.Input)
		if err != nil {
			return nil, "", err
		}
		query.Set("project", project.ID)
		id := ""
		if request.Operation == "sentry.alert.read" {
			id, _ = request.Input["alert_id"].(string)
			if !sentryDecimal(id) {
				return nil, "", &SafeError{Code: providerRequestRejectedCode}
			}
			eligible, err := adapter.sentryAlertProjectEligible(ctx, request, capability, configuration, project, id)
			if err != nil {
				return nil, "", err
			}
			if !eligible {
				return nil, "", &SafeError{Code: providerRequestRejectedCode}
			}
			path += id + "/"
			query = nil
		}
		response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, path, query, nil)
		if err != nil {
			return nil, "", err
		}
		if integrationpackage.ValidateCredentialPermissionJSON(response.Body) != nil {
			return nil, "", &SafeError{Code: providerResponseInvalidCode}
		}
		alerts := []sentryAlert{}
		if id != "" {
			var alert sentryAlert
			if decodeTypedProviderJSON(response.Body, &alert) != nil || alert.ID != id {
				return nil, "", &SafeError{Code: providerResponseInvalidCode}
			}
			alerts = append(alerts, alert)
		} else if decodeTypedProviderJSON(response.Body, &alerts) != nil || alerts == nil || len(alerts) > limit {
			return nil, "", &SafeError{Code: providerResponseInvalidCode}
		}
		items := []map[string]any{}
		seen := map[string]bool{}
		for _, alert := range alerts {
			if !sentryDecimal(alert.ID) || alert.OrganizationID != project.Organization.ID || alert.Name == "" || len(alert.Name) > 2000 || alert.Enabled == nil || seen[alert.ID] {
				return nil, "", &SafeError{Code: providerResponseInvalidCode}
			}
			seen[alert.ID] = true
			if id == "" {
				eligible, err := adapter.sentryAlertProjectEligible(ctx, request, capability, configuration, project, alert.ID)
				if err != nil {
					return nil, "", err
				}
				if !eligible {
					continue
				}
			}
			items = append(items, map[string]any{"id": alert.ID, "name": alert.Name, "enabled": *alert.Enabled})
		}
		if id != "" {
			return items[0], "sentry-alert:" + id, nil
		}
		result, err := sentryListResult(response, configuration["base_url"], path, query, items)
		return result, providerRef, err
	default:
		return nil, "", &SafeError{Code: providerCapabilityUnsupportedCode}
	}
}
