package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubPullRequestDraftUsesTypedProviderRequest(t *testing.T) {
	for _, test := range []struct {
		name  string
		draft *bool
		want  bool
	}{
		{name: "omitted defaults to false"},
		{name: "explicit false", draft: new(false)},
		{name: "explicit true", draft: new(true), want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "test-token")
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/repo/pulls" {
					t.Error("pull create escaped the canonical provider route")
				}
				var payload struct {
					Title string `json:"title"`
					Head  string `json:"head"`
					Base  string `json:"base"`
					Draft *bool  `json:"draft"`
				}
				if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Draft == nil || *payload.Draft != test.want || payload.Title != "Title" || payload.Head != "feature" || payload.Base != "main" {
					t.Error("typed pull request draft or repository inputs changed")
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"number": 3, "title": "Title", "body": "Text", "state": "open", "draft": test.want,
					"head": map[string]string{"ref": "feature", "sha": "abc"},
					"base": map[string]string{"ref": "main"}, "html_url": "https://github.com/acme/repo/pull/3",
				})
			}))
			defer server.Close()
			adapter.githubBaseURL = mustParseURL(t, server.URL+"/")
			adapter.githubHTTPClient = server.Client()
			input := map[string]any{"title": "Title", "head": "feature", "base": "main"}
			if test.draft != nil {
				input["draft"] = *test.draft
			}
			request := invocationRequest(t, adapter.definitions["github"], "github.pull_request.create", input, credential)
			request.ApprovalPolicy = "NONE"
			result, err := adapter.Execute(t.Context(), request)
			var view githubPullView
			if err != nil || calls != 1 || json.Unmarshal([]byte(result.Summary), &view) != nil || view.Draft != test.want || view.Number != 3 || view.SHA != "abc" || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest || result.Receipt.ProviderEffectRef != "github-pull:3" {
				t.Fatalf("draft result or exact receipt changed: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestGitHubPullRequestDraftInvalidInputHasNoProviderEffect(t *testing.T) {
	for _, test := range []struct {
		name  string
		input map[string]any
	}{
		{name: "string", input: map[string]any{"draft": "true"}},
		{name: "number", input: map[string]any{"draft": 1}},
		{name: "null", input: map[string]any{"draft": nil}},
		{name: "unknown alias", input: map[string]any{"is_draft": true}},
		{name: "caller authority", input: map[string]any{"draft": true, "actor": "owner"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "test-token")
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			adapter.githubBaseURL = mustParseURL(t, server.URL+"/")
			adapter.githubHTTPClient = server.Client()
			input := map[string]any{"title": "Title", "head": "feature", "base": "main"}
			request := invocationRequest(t, adapter.definitions["github"], "github.pull_request.create", input, credential)
			request.ApprovalPolicy = "NONE"
			for key, value := range test.input {
				request.Input[key] = value
			}
			if _, err := adapter.Execute(t.Context(), request); err == nil || calls != 0 {
				t.Fatal("invalid draft input reached the provider")
			}
		})
	}
}
