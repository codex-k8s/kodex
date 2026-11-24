package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestJiraServerCommentReadsBindExistingIssue validates paging and denies duplicate IDs, foreign issues and mismatched comment readback.
// TestJiraServerCommentReadsBindExistingIssue проверяет пагинацию и отклоняет дубли ID, чужие задачи и несовпадающий комментарий.
func TestJiraServerCommentReadsBindExistingIssue(t *testing.T) {
	for _, scenario := range []struct {
		name, operation, key string
		commentID            string
		duplicate, moved     bool
		valid                bool
		commentReads         int
	}{
		{name: "page", operation: "jira.issue.comment.list", key: "DEMO-1", valid: true, commentReads: 1},
		{name: "foreign", operation: "jira.issue.comment.list", key: "OTHER-1"},
		{name: "duplicate", operation: "jira.issue.comment.list", key: "DEMO-1", duplicate: true, commentReads: 1},
		{name: "moved", operation: "jira.issue.comment.list", key: "DEMO-1", moved: true, commentReads: 1},
		{name: "other ID", operation: "jira.issue.comment.read", key: "DEMO-1", commentID: "9", commentReads: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-server-token")
			input := atlassianServerInput(scenario.operation)
			input["issue_key"] = scenario.key
			request := atlassianServerInvocation(t, adapter, jiraServerDefinition, scenario.operation, input, credential)
			commentReads, issueReads := 0, 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var payload any
				if strings.Contains(r.URL.Path, "/comment") {
					commentReads++
					if scenario.commentID != "" {
						payload = jiraServerCommentFixture(scenario.commentID)
					} else {
						query := r.URL.Query()
						if query.Get("startAt") != "0" || query.Get("maxResults") != "2" || query.Get("orderBy") != "created" {
							t.Fatalf("unexpected comment page: %v", query)
						}
						rows := []any{jiraServerCommentFixture("3")}
						if scenario.duplicate {
							rows = append(rows, jiraServerCommentFixture("3"))
						}
						payload = map[string]any{"startAt": 0, "maxResults": 2, "total": 4, "comments": rows}
					}
				} else {
					issueReads++
					issue := jiraServerIssueFixture()
					if scenario.moved && issueReads == 2 {
						issue["key"] = "OTHER-1"
					}
					payload = issue
				}
				raw, _ := json.Marshal(payload)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if (err == nil) != scenario.valid || commentReads != scenario.commentReads {
				t.Fatalf("provider reads=%d result=%v", commentReads, err)
			}
			if scenario.valid && (!strings.Contains(result.Summary, `"next_cursor":"1"`) || strings.Contains(result.Summary, "synthetic-author")) {
				t.Fatalf("unexpected comment projection: %s", result.Summary)
			}
		})
	}
}
