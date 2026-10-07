package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubReviewLargeIdentifierRoundtrip(t *testing.T) {
	for _, operation := range []string{"github.pull_request.review.create", "github.pull_request.review.read", "github.pull_request.review.list"} {
		t.Run(operation, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "fixture-token")
			input := map[string]any{"pull_request_number": 1799}
			expectedMethod, expectedPath := "GET", "/repos/acme/repo/pulls/1799/reviews"
			if strings.HasSuffix(operation, "create") {
				input["event"] = "COMMENT"
				input["sha"] = strings.Repeat("a", 40)
				expectedMethod = "POST"
			}
			if strings.HasSuffix(operation, "read") {
				input["review_id"] = int64(5436151248)
				expectedPath += "/5436151248"
			}
			body := strings.Repeat("a", 1800)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != expectedMethod || r.URL.Path != expectedPath {
					t.Error("review method or exact resource changed")
				}
				w.Header().Set("Content-Type", "application/json")
				review := fmt.Sprintf(`{"id":5436151248,"body":"%s","state":"COMMENTED","commit_id":"%s"}`, body, strings.Repeat("a", 40))
				if strings.HasSuffix(operation, "list") {
					review = "[" + review + "]"
				}
				_, _ = io.WriteString(w, review)
			}))
			defer server.Close()
			adapter.githubBaseURL = mustParseURL(t, server.URL+"/")
			adapter.githubHTTPClient = server.Client()
			request := invocationRequest(t, adapter.definitions["github"], operation, input, credential)
			result, err := adapter.Execute(t.Context(), request)
			if err != nil || calls != 1 {
				t.Fatalf("review result err=%v calls=%d", err, calls)
			}
			var review githubReviewView
			if strings.HasSuffix(operation, "list") {
				var page struct {
					Items string `json:"items"`
					Count int    `json:"count"`
				}
				var reviews []githubReviewView
				if json.Unmarshal([]byte(result.Summary), &page) != nil || page.Count != 1 || json.Unmarshal([]byte(page.Items), &reviews) != nil || len(reviews) != 1 {
					t.Fatal("review list projection changed")
				}
				review = reviews[0]
			} else if json.Unmarshal([]byte(result.Summary), &review) != nil {
				t.Fatal("invalid review projection")
			}
			if review.ID != 5436151248 || review.Body != body || review.State != "COMMENTED" || review.CommitID != strings.Repeat("a", 40) || result.Receipt.ResponseDigest == "" {
				t.Fatal("review identity or receipt lost")
			}
			if !strings.HasSuffix(operation, "list") && result.Receipt.ProviderEffectRef != "github-review:5436151248" {
				t.Fatal("provider effect identity changed")
			}
		})
	}
}

func TestGitHubReviewUnsafeIdentifierRemainsUnknownWithoutRetry(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "fixture-token")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" {
			t.Error("unexpected recovery request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":9007199254740992,"body":"","state":"COMMENTED","commit_id":"abc"}`)
	}))
	defer server.Close()
	adapter.githubBaseURL = mustParseURL(t, server.URL+"/")
	adapter.githubHTTPClient = server.Client()
	request := invocationRequest(t, adapter.definitions["github"], "github.pull_request.review.create", map[string]any{"pull_request_number": 1799, "sha": "abc", "event": "COMMENT"}, credential)
	_, err := adapter.Execute(t.Context(), request)
	var unknown *UnknownOutcomeError
	if !errors.As(err, &unknown) || unknown.stage != "response_validation" || calls != 1 {
		t.Fatalf("unsafe response changed boundary err=%v calls=%d", err, calls)
	}
}
