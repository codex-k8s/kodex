package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestGitHubPullIndexLargeProviderPagesPreservePagination(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	calls := 0
	adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if r.Method != "GET" || r.URL.Host != "api.github.com" || r.URL.Path != "/repos/acme/repo/pulls" ||
			r.URL.Query().Get("state") != "all" || r.URL.Query().Get("per_page") != "20" || page < 1 || page > 2 {
			t.Fatal("repository scope, filter or provider pagination changed")
		}
		items := make([]map[string]any, 20)
		for i := range items {
			items[i] = map[string]any{"number": (page-1)*20 + i + 1, "title": "Изменение", "body": strings.Repeat("описание ", 600),
				"state": "closed", "head": map[string]string{"ref": "feature", "sha": strings.Repeat("a", 40)},
				"base": map[string]string{"ref": "main"}, "html_url": "https://github.com/acme/repo/pull/1"}
		}
		body, _ := json.Marshal(items)
		if len(body) <= maximumResponseBytes {
			t.Fatal("fixture no longer reproduces the oversized upstream response")
		}
		header := http.Header{}
		if page == 1 {
			header.Set("Link", `<https://api.github.com/repos/acme/repo/pulls?state=all&per_page=20&page=2>; rel="next"`)
		}
		return &http.Response{Request: r, StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	seen := map[int]bool{}
	for page := 1; page <= 2; page++ {
		result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.pull_request.list",
			map[string]any{"state": "all", "cursor": page}, credential))
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Items string `json:"items"`
			Count int    `json:"count"`
			Next  int    `json:"next_cursor"`
		}
		var items []map[string]any
		if json.Unmarshal([]byte(result.Summary), &envelope) != nil || json.Unmarshal([]byte(envelope.Items), &items) != nil ||
			envelope.Count != 20 || len(items) != 20 || len(result.Summary) > maximumResponseBytes ||
			page == 1 && envelope.Next != 2 || page == 2 && envelope.Next != 0 {
			t.Fatal("compact index lost its items, bound or exact next cursor")
		}
		for _, item := range items {
			if _, exists := item["body"]; exists {
				t.Fatal("full PR descriptions must be read separately, not repeated in the index")
			}
			number := int(item["number"].(float64))
			if seen[number] || item["sha"] != strings.Repeat("a", 40) || item["title"] != "Изменение" {
				t.Fatal("index lost identity, head SHA or duplicated a row")
			}
			seen[number] = true
		}
	}
	if calls != 2 || len(seen) != 40 {
		t.Fatal("index silently dropped provider rows or repeated successful reads")
	}
}

func TestGitHubPullReadStillReturnsEntireDescription(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	text := strings.Repeat("Полное описание 🙂\n", 250)
	adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]any{"number": 3, "title": "Изменение", "body": text, "state": "open",
			"head": map[string]string{"ref": "feature", "sha": strings.Repeat("a", 40)}, "base": map[string]string{"ref": "main"},
			"html_url": "https://github.com/acme/repo/pull/3"})
		return &http.Response{Request: r, StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], "github.pull_request.read",
		map[string]any{"pull_request_number": 3}, credential))
	var view githubPullView
	if err != nil || json.Unmarshal([]byte(result.Summary), &view) != nil || view.Body != text {
		t.Fatal("dedicated PR read truncated or removed the full description")
	}
}

type githubTrackedResponseBody struct {
	io.Reader
	closed bool
}

func (body *githubTrackedResponseBody) Close() error { body.closed = true; return nil }

func TestGitHubOversizedProviderResponseFailsOnceAndClosesBody(t *testing.T) {
	for _, operation := range []string{"github.pull_request.list", "github.pull_request.create"} {
		t.Run(operation, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "test-token")
			calls := 0
			var body *githubTrackedResponseBody
			adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				body = &githubTrackedResponseBody{Reader: strings.NewReader(strings.Repeat(" ", (2<<20)+1))}
				return &http.Response{Request: r, StatusCode: 200, Header: http.Header{}, Body: body}, nil
			})}
			input := map[string]any{"state": "all"}
			if operation == "github.pull_request.create" {
				input = map[string]any{"title": "Изменение", "head": "feature", "base": "main", "body": "Text", "draft": true}
			}
			result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["github"], operation, input, credential))
			var safe *SafeError
			var unknown *UnknownOutcomeError
			valid := errors.As(err, &safe) && safe.Code == "INTEGRATION_RESPONSE_INVALID"
			if operation == "github.pull_request.create" {
				valid = errors.As(err, &unknown)
			}
			if !valid || calls != 1 || body == nil || !body.closed || result.Summary != "" {
				t.Fatal(fmt.Sprintf("response boundary changed: calls=%d closed=%t", calls, body != nil && body.closed))
			}
		})
	}
}
