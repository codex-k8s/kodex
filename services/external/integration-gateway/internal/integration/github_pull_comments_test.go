package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestGitHubPullCommentAuthorityAndCreateRejoin exercises all four PR-only operations, preserves ordinary Issue grant boundaries, refuses a comment from another PR, and proves a repeated create rejoins one marker without another POST.
// TestGitHubPullCommentAuthorityAndCreateRejoin проверяет четыре PR-only операции, сохраняет границы обычных Issue grants, отклоняет комментарий другого PR и доказывает возврат одного marker без второго POST при повторе create.
func TestGitHubPullCommentAuthorityAndCreateRejoin(t *testing.T) {
	for _, action := range []string{"create", "list", "read", "update"} {
		for _, scenario := range []struct {
			name                           string
			pullGrant, pullObject, foreign bool
			wantOK                         bool
		}{
			{name: "PR grant and PR object", pullGrant: true, pullObject: true, wantOK: true},
			{name: "PR grant cannot target Issue", pullGrant: true},
			{name: "Issue grant cannot target PR", pullObject: true},
			{name: "foreign comment", pullGrant: true, pullObject: true, foreign: true, wantOK: action == "create" || action == "list"},
		} {
			t.Run(action+"/"+scenario.name, func(t *testing.T) {
				adapter := testAdapter(t)
				credential := testCredential(t, adapter, "fixture-pr-comment-token")
				operation, numberKey := "github.issue.comment."+action, "issue_number"
				if scenario.pullGrant {
					operation, numberKey = "github.pull_request.comment."+action, "pull_request_number"
				}
				input := map[string]any{numberKey: 3}
				if action == "create" || action == "update" {
					input["body"] = "Ответ на замечание"
				}
				if action == "read" || action == "update" {
					input["comment_id"] = 4
				}
				request := invocationRequest(t, adapter.definitions["github"], operation, input, credential)
				request.ApprovalPolicy = "NONE"
				writes, stored := 0, ""
				adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Host != "api.github.com" || !strings.HasPrefix(r.URL.Path, "/repos/acme/repo/") {
						t.Fatal("PR comment escaped the granted repository")
					}
					body := ""
					switch {
					case r.URL.Path == "/repos/acme/repo/issues/3":
						body = `{"number":3}`
						if scenario.pullObject {
							body = `{"number":3,"pull_request":{}}`
						}
					case r.URL.Path == "/repos/acme/repo/issues/3/comments" && r.Method == "GET":
						body = `[]`
						if stored != "" {
							encoded, _ := json.Marshal([]map[string]any{{"id": 4, "body": stored}})
							body = string(encoded)
						}
					case r.URL.Path == "/repos/acme/repo/issues/3/comments" && r.Method == "POST":
						writes++
						var value struct{ Body string }
						if json.NewDecoder(r.Body).Decode(&value) != nil {
							t.Fatal("invalid comment body")
						}
						stored, body = value.Body, `{"id":4}`
					case r.URL.Path == "/repos/acme/repo/issues/comments/4":
						issueURL := "https://api.github.com/repos/acme/repo/issues/3"
						if scenario.foreign {
							issueURL = "https://api.github.com/repos/acme/repo/issues/99"
						}
						if r.Method == "PATCH" {
							writes++
						}
						encoded, _ := json.Marshal(map[string]any{"id": 4, "body": "Ответ на замечание", "issue_url": issueURL})
						body = string(encoded)
					default:
						t.Fatal("unexpected PR comment endpoint")
					}
					return &http.Response{Request: r, StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
				})}
				_, err := adapter.Execute(t.Context(), request)
				if (err == nil) != scenario.wantOK || !scenario.wantOK && writes != 0 {
					t.Fatalf("comment authority changed: writes=%d error_nil=%t", writes, err == nil)
				}
				if scenario.wantOK && action == "create" {
					if _, err := adapter.Execute(t.Context(), request); err != nil || writes != 1 {
						t.Fatal("comment create repeated the external write")
					}
				}
			})
		}
	}
}
