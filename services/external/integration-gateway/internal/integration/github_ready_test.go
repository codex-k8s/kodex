package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestGitHubReadyPinsRepositoryAndHead proves the closed GraphQL mutation uses only the node resolved from the granted REST repository, rejects stale or incomplete inputs, and reconciles an uncertain write through reads without repeating it.
// TestGitHubReadyPinsRepositoryAndHead доказывает получение GraphQL node только из выданного REST-репозитория, отказ устаревших и неполных входов и восстановление неопределённой записи чтением без её повтора.
func TestGitHubReadyPinsRepositoryAndHead(t *testing.T) {
	for _, scenario := range []struct {
		name, before, after, node                    string
		draft, uncertain, wantOK, staysDraft, denied bool
		wantWrites                                   int
	}{
		{name: "draft becomes ready", before: "a", after: "a", node: "PR_fixture", draft: true, wantOK: true, wantWrites: 1},
		{name: "already ready reads only", before: "a", after: "a", node: "PR_fixture", wantOK: true},
		{name: "stale head cannot write", before: "b", after: "b", node: "PR_fixture", draft: true},
		{name: "missing node cannot write", before: "a", after: "a", draft: true},
		{name: "changed head after write is uncertain", before: "a", after: "b", node: "PR_fixture", draft: true, wantWrites: 1},
		{name: "lost write response reconciles", before: "a", after: "a", node: "PR_fixture", draft: true, uncertain: true, wantOK: true, wantWrites: 1},
		{name: "lost response without proof remains unknown", before: "a", after: "a", node: "PR_fixture", draft: true, uncertain: true, staysDraft: true, wantWrites: 1},
		{name: "GraphQL failure with HTTP200 remains unknown", before: "a", after: "a", node: "PR_fixture", draft: true, staysDraft: true, wantWrites: 1},
		{name: "forbidden is not retried", before: "a", after: "a", node: "PR_fixture", draft: true, denied: true, wantWrites: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "fixture-ready-token")
			reads, writes := 0, 0
			adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.github.com" {
					t.Fatal("ready operation escaped GitHub API")
				}
				status, body := http.StatusOK, ""
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/repo/pulls/3":
					reads++
					head, draft := scenario.before, scenario.draft
					if writes > 0 {
						head, draft = scenario.after, scenario.staysDraft
					}
					encoded, _ := json.Marshal(map[string]any{"number": 3, "node_id": scenario.node, "state": "open", "draft": draft, "head": map[string]string{"sha": strings.Repeat(head, 40)}, "html_url": "https://github.com/acme/repo/pull/3"})
					body = string(encoded)
				case r.Method == http.MethodPost && r.URL.Path == "/graphql":
					writes++
					var payload struct {
						Query     string `json:"query"`
						Variables struct {
							Input struct {
								PullRequestID string `json:"pullRequestId"`
							} `json:"input"`
						} `json:"variables"`
					}
					if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Variables.Input.PullRequestID != "PR_fixture" || !strings.Contains(payload.Query, "markPullRequestReadyForReview") || strings.Contains(payload.Query, "mergePullRequest") {
						t.Fatal("ready mutation was not the closed server-resolved operation")
					}
					body = `{"data":{"markPullRequestReadyForReview":{"pullRequest":{"id":"PR_fixture"}}}}`
					if scenario.uncertain {
						status = http.StatusBadGateway
						body = `{}`
					}
					if scenario.denied {
						status, body = http.StatusForbidden, `{}`
					}
				default:
					t.Fatal("ready operation used an unexpected repository route")
				}
				return &http.Response{Request: r, StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			request := invocationRequest(t, adapter.definitions["github"], "github.pull_request.ready", map[string]any{"pull_request_number": 3, "expected_head_sha": strings.Repeat("a", 40)}, credential)
			request.ApprovalPolicy = "NONE"
			result, err := adapter.Execute(t.Context(), request)
			if writes != scenario.wantWrites || (err == nil) != scenario.wantOK || reads == 0 {
				t.Fatalf("ready boundary changed: writes=%d reads=%d error_nil=%t", writes, reads, err == nil)
			}
			if scenario.wantWrites > 0 && !scenario.wantOK && IsUnknownOutcome(err) == scenario.denied {
				t.Fatal("post-write drift did not preserve uncertain outcome")
			}
			if scenario.wantOK && (result.Receipt.ProviderEffectRef != "github-pull:3" || result.Receipt.EffectKey != request.EffectKey) {
				t.Fatal("ready result lost the native receipt pins")
			}
		})
	}
}

// TestGitHubReadyRecoveryCannotMutate proves uncertain recovery reads only the granted PR and does not attempt Ready when Draft is still observed.
// TestGitHubReadyRecoveryCannotMutate доказывает только чтение выданного PR при восстановлении и отсутствие попытки Ready при сохранённом Draft.
func TestGitHubReadyRecoveryCannotMutate(t *testing.T) {
	for _, draft := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "still draft"}[draft], func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "fixture-ready-recovery-token")
			reads := 0
			adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.String() != "https://api.github.com/repos/acme/repo/pulls/3" {
					t.Fatal("Ready recovery attempted a mutation or escaped repository scope")
				}
				reads++
				body, _ := json.Marshal(map[string]any{"number": 3, "node_id": "PR_fixture", "state": "open", "draft": draft, "head": map[string]string{"sha": strings.Repeat("a", 40)}, "html_url": "https://github.com/acme/repo/pull/3"})
				return &http.Response{Request: r, StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			request := invocationRequest(t, adapter.definitions["github"], "github.pull_request.ready", map[string]any{"pull_request_number": 3, "expected_head_sha": strings.Repeat("a", 40)}, credential)
			request.ApprovalPolicy = "NONE"
			_, err := adapter.Recover(t.Context(), request)
			if reads != 1 || (err == nil) == draft || draft && !IsUnknownOutcome(err) {
				t.Fatal("Ready recovery lost the read-only outcome boundary")
			}
		})
	}
}
