package integration

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestConfluenceServerCommentListingChecksScopeAndCursor validates exact parent and cursor, including a genuine empty page.
// TestConfluenceServerCommentListingChecksScopeAndCursor проверяет контейнер, смещение и штатную пустую страницу.
func TestConfluenceServerCommentListingChecksScopeAndCursor(t *testing.T) {
	for _, scenario := range []struct {
		name, parent, next string
		empty, valid       bool
	}{
		{name: "empty", empty: true, valid: true},
		{"next", "101", "/rest/api/content/101/child/comment?start=1&limit=2&location=footer", false, true},
		{"wrong container", "999", "", false, false},
		{"other page cursor", "101", "/rest/api/content/999/child/comment?start=1&limit=2", false, false},
		{"foreign cursor", "101", "https://other.example.invalid/rest/api/content/101/child/comment?start=1", false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-server-token")
			operation := "confluence.page.comment.list"
			request := atlassianServerInvocation(t, adapter, confluenceServerDefinition, operation, atlassianServerInput(operation), credential)
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if !strings.HasSuffix(r.URL.Path, "/child/comment") {
					return atlassianServerResponse(t, request, r), nil
				}
				items := make([]any, 0, 1)
				if !scenario.empty {
					items = append(items, confluenceServerCommentFixture("103", scenario.parent, "<p>Fixture</p>"))
				}
				body, _ := json.Marshal(map[string]any{"start": 0, "limit": 2, "size": len(items), "results": items,
					"_links": map[string]string{"next": scenario.next}})
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if (err == nil) != scenario.valid {
				t.Fatalf("listing result: %v", err)
			}
			if scenario.valid && result.Receipt.ProviderEffectRef == "" {
				t.Fatal("valid listing returned no proof")
			}
		})
	}
}

// TestConfluenceServerCommentWriteIsSingleAttempt covers wrong parent and a lost provider response without issuing a second POST.
// TestConfluenceServerCommentWriteIsSingleAttempt проверяет чужую страницу и потерю ответа без второго POST.
func TestConfluenceServerCommentWriteIsSingleAttempt(t *testing.T) {
	for _, foreign := range []bool{true, false} {
		adapter := testAdapter(t)
		credential := testCredential(t, adapter, "synthetic-server-token")
		operation := "confluence.page.comment.create"
		request := atlassianServerInvocation(t, adapter, confluenceServerDefinition, operation, atlassianServerInput(operation), credential)
		writes := 0
		adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method == http.MethodPost {
				writes++
				return nil, io.ErrUnexpectedEOF
			}
			if foreign {
				response := atlassianServerResponse(t, request, r)
				_ = response.Body.Close()
				body, _ := json.Marshal(confluenceServerPageFixture("999", "Another page", 1))
				response.Body = io.NopCloser(strings.NewReader(string(body)))
				return response, nil
			}
			return atlassianServerResponse(t, request, r), nil
		})}
		_, err := adapter.Execute(t.Context(), request)
		var unknown *UnknownOutcomeError
		if err == nil || foreign && (writes != 0 || errors.As(err, &unknown)) || !foreign && (writes != 1 || !errors.As(err, &unknown)) {
			t.Fatalf("foreign=%t writes=%d unexpected result: %v", foreign, writes, err)
		}
	}
}

// TestConfluenceServerCommentReadbackChecksExactObject covers a partial successful acknowledgment and a rejected write.
// TestConfluenceServerCommentReadbackChecksExactObject проверяет неполную квитанцию и явный отказ провайдера.
func TestConfluenceServerCommentReadbackChecksExactObject(t *testing.T) {
	for _, scenario := range []struct {
		name, parent string
		status       int
		valid        bool
	}{
		{"sparse acknowledgment", "101", http.StatusCreated, true},
		{"wrong readback parent", "999", http.StatusCreated, false},
		{"forbidden", "", http.StatusForbidden, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-server-token")
			operation := "confluence.page.comment.create"
			request := atlassianServerInvocation(t, adapter, confluenceServerDefinition, operation, atlassianServerInput(operation), credential)
			writes, readbacks := 0, 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				var body any
				status := http.StatusOK
				switch {
				case r.Method == http.MethodPost:
					writes++
					body = map[string]string{"id": "104", "type": "comment"}
					status = scenario.status
				case strings.HasSuffix(r.URL.Path, "/content/104"):
					readbacks++
					body = confluenceServerCommentFixture("104", scenario.parent, "<p>Fixture</p>\n"+confluenceServerMarker(request))
				default:
					return atlassianServerResponse(t, request, r), nil
				}
				raw, _ := json.Marshal(body)
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if (err == nil) != scenario.valid || writes != 1 || (readbacks == 1) != (scenario.status == http.StatusCreated) {
				t.Fatalf("writes=%d readbacks=%d: %v", writes, readbacks, err)
			}
			if scenario.valid && result.Receipt.ProviderEffectRef != "confluence-comment:104" {
				t.Fatalf("unexpected receipt: %s", result.Receipt.ProviderEffectRef)
			}
		})
	}
}
