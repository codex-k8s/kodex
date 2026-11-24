package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestJiraServerProjectSearch applies optional filters inside the configured project and validates each result.
// TestJiraServerProjectSearch применяет фильтры только в настроенном проекте и проверяет каждый результат.
func TestJiraServerProjectSearch(t *testing.T) {
	for _, scenario := range []struct {
		name, text, assignee, clauses, observed, project string
		valid                                            bool
	}{
		{name: "project", valid: true},
		{name: "assignee", assignee: "sample.user", clauses: ` AND assignee = "sample.user"`, observed: "Sample.User", valid: true},
		{name: "combined", text: "fixture", assignee: "sample.user", clauses: ` AND text ~ "fixture" AND assignee = "sample.user"`, observed: "sample.user", valid: true},
		{name: "escaped literal", assignee: `sample" OR project = "OTHER`, clauses: ` AND assignee = "sample\" OR project = \"OTHER"`},
		{name: "wrong assignee", assignee: "sample.user", clauses: ` AND assignee = "sample.user"`, observed: "other.user"},
		{name: "wrong project", project: "OTHER"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-server-token")
			input := map[string]any{"limit": 2}
			if scenario.text != "" {
				input["text"] = scenario.text
			}
			if scenario.assignee != "" {
				input["assignee_username"] = scenario.assignee
			}
			request := atlassianServerInvocation(t, adapter, jiraServerDefinition, "jira.issue.search", input, credential)
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				query := r.URL.Query()
				fields := "project,summary,status"
				if scenario.assignee != "" {
					fields += ",assignee"
				}
				if query.Get("jql") != `project = "DEMO"`+scenario.clauses+" ORDER BY key ASC" || query.Get("fields") != fields || query.Get("startAt") != "0" || query.Get("maxResults") != "2" {
					t.Fatalf("unexpected project search query: %v", query)
				}
				issue := jiraServerIssueFixture()
				resultFields := issue["fields"].(map[string]any)
				if scenario.observed != "" {
					resultFields["assignee"] = map[string]string{"name": scenario.observed}
				}
				if scenario.project != "" {
					resultFields["project"] = map[string]string{"key": scenario.project}
				}
				body, _ := json.Marshal(map[string]any{"startAt": 0, "maxResults": 2, "total": 4, "issues": []any{issue}})
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if (err == nil) != scenario.valid {
				t.Fatalf("unexpected search result: %v", err)
			}
			if scenario.valid && !strings.Contains(result.Summary, `"next_cursor":"1"`) {
				t.Fatalf("missing next offset: %s", result.Summary)
			}
		})
	}
}
