package integration

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestGitHubAutonomousContentPreservesMergeGate exercises the real SDK boundary and permits one file write only after fresh nondefault, unprotected branch metadata is known; missing metadata and read failures never produce a mutation.
// TestGitHubAutonomousContentPreservesMergeGate проверяет настоящий SDK boundary и допускает единственную запись файла только после свежего подтверждения обычной незащищённой ветки; неизвестные metadata и ошибки чтения не приводят к изменению.
func TestGitHubAutonomousContentPreservesMergeGate(t *testing.T) {
	for _, scenario := range []struct {
		name, branch, repository, branchResponse string
		readStatus                               int
		wantWrites                               int
	}{
		{name: "feature", branch: "feature/1858", repository: `{"default_branch":"main"}`, branchResponse: `{"name":"feature/1858","protected":false}`, wantWrites: 1},
		{name: "default", branch: "feature/1858", repository: `{"default_branch":"feature/1858"}`},
		{name: "protected", branch: "feature/1858", repository: `{"default_branch":"main"}`, branchResponse: `{"name":"feature/1858","protected":true}`},
		{name: "unknown-repository", branch: "feature/1858", repository: `{}`},
		{name: "unknown-protection", branch: "feature/1858", repository: `{"default_branch":"main"}`, branchResponse: `{"name":"feature/1858"}`},
		{name: "foreign-branch", branch: "feature/1858", repository: `{"default_branch":"main"}`, branchResponse: `{"name":"other","protected":false}`},
		{name: "read-failure", branch: "feature/1858", repository: `{}`, readStatus: http.StatusForbidden},
		{name: "ambiguous-ref", branch: "refs/heads/main", repository: `{"default_branch":"main"}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "fixture-autonomous-content-token")
			request := invocationRequest(t, adapter.definitions["github"], "github.repository.content.update", map[string]any{"path": "docs/requirements.md", "branch": scenario.branch, "message": "Обновить требования", "content_base64": "VGV4dA==", "sha": strings.Repeat("a", 40)}, credential)
			request.ApprovalPolicy = "NONE"
			writes := 0
			adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.github.com" || !strings.HasPrefix(r.URL.Path, "/repos/acme/repo") {
					t.Fatal("autonomous content escaped the granted repository")
				}
				status, body := http.StatusOK, scenario.repository
				switch r.Method {
				case http.MethodGet:
					if scenario.readStatus != 0 {
						status = scenario.readStatus
					}
					if r.URL.Path != "/repos/acme/repo" {
						if r.URL.Path != "/repos/acme/repo/branches/feature/1858" {
							t.Fatal("autonomous branch check changed its exact route")
						}
						body = scenario.branchResponse
					}
				case http.MethodPut:
					writes++
					body = `{"commit":{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}`
				default:
					t.Fatal("unexpected autonomous provider method")
				}
				return &http.Response{Request: r, StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if writes != scenario.wantWrites || (err == nil) != (scenario.wantWrites == 1) || scenario.wantWrites == 0 && result.Summary != "" {
				t.Fatalf("autonomous file boundary writes=%d want=%d error_nil=%t", writes, scenario.wantWrites, err == nil)
			}
		})
	}
}
