package integration

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestAtlassianServerAuthorityRejectsBeforeCredentials corrupts every published operation's protected tuple before credential loading.
// TestAtlassianServerAuthorityRejectsBeforeCredentials искажает protected tuple каждой операции до загрузки credential.
func TestAtlassianServerAuthorityRejectsBeforeCredentials(t *testing.T) {
	for _, key := range []string{jiraServerDefinition, confluenceServerDefinition} {
		definition := serverDefinitionFixture(t, key)
		for _, capability := range definition.Spec.Capabilities {
			for _, changed := range []string{"version", "definition", "operation", "scope", "input", "grant"} {
				t.Run(key+"/"+capability.Key+"/"+changed, func(t *testing.T) {
					adapter := testAdapter(t)
					request := atlassianServerInvocation(t, adapter, key, capability.Key, atlassianServerInput(capability.Key), &CredentialRevision{})
					switch changed {
					case "version":
						request.DefinitionVersion = "999.0.0"
					case "definition":
						request.DefinitionDigest = strings.Repeat("0", 64)
					case "operation":
						request.Operation += ".unknown"
					case "scope":
						request.ResourceScope["context_path"] = "/foreign"
					case "input":
						request.Input["url"] = "https://other.example.invalid/"
					case "grant":
						request.GrantVersion = 0
					}
					adapter.credentials = nil
					_, err := adapter.Execute(t.Context(), request)
					var safe *SafeError
					if !errors.As(err, &safe) || safe.Code == "INTEGRATION_CREDENTIAL_UNAVAILABLE" {
						t.Fatalf("authority denial did not precede credential boundary: %v", err)
					}
				})
			}
		}
	}
}

// TestAtlassianServerReadFailuresAndRateLimit keep auth/not-found failures single-attempt and bound transient retry to the same Server route.
// TestAtlassianServerReadFailuresAndRateLimit не повторяет auth/not-found и ограничивает transient retry тем же Server route.
func TestAtlassianServerReadFailuresAndRateLimit(t *testing.T) {
	for _, key := range []string{jiraServerDefinition, confluenceServerDefinition} {
		operation := "jira.project.read"
		if key == confluenceServerDefinition {
			operation = "confluence.space.read"
		}
		for _, status := range []int{401, 403, 404, 429} {
			t.Run(key+"/"+http.StatusText(status), func(t *testing.T) {
				adapter := testAdapter(t)
				credential := testCredential(t, adapter, "synthetic-server-token")
				request := atlassianServerInvocation(t, adapter, key, operation, map[string]any{}, credential)
				calls := 0
				adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
					}
					return atlassianServerResponse(t, request, r), nil
				})}
				_, err := adapter.Execute(t.Context(), request)
				if status == 429 {
					if err != nil || calls != 2 {
						t.Fatalf("bounded rate-limit recovery failed: calls=%d, %v", calls, err)
					}
				} else if err == nil || calls != 1 {
					t.Fatalf("definitive failure was retried: calls=%d, %v", calls, err)
				}
			})
		}
	}
}

// TestAtlassianServerWritesPreserveUnknown executes every new mutation once under transport/server/malformed-success failures.
// TestAtlassianServerWritesPreserveUnknown выполняет каждую mutation один раз при transport/server/malformed-success ошибке.
func TestAtlassianServerWritesPreserveUnknown(t *testing.T) {
	for _, key := range []string{jiraServerDefinition, confluenceServerDefinition} {
		for _, capability := range serverDefinitionFixture(t, key).Spec.Capabilities {
			if capability.Risk == "READ" {
				continue
			}
			for _, failure := range []string{"transport", "server", "malformed", "conflict"} {
				t.Run(key+"/"+capability.Key+"/"+failure, func(t *testing.T) {
					adapter := testAdapter(t)
					credential := testCredential(t, adapter, "synthetic-server-token")
					request := atlassianServerInvocation(t, adapter, key, capability.Key, atlassianServerInput(capability.Key), credential)
					mutations := 0
					adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
						if r.Method == http.MethodGet {
							return atlassianServerResponse(t, request, r), nil
						}
						mutations++
						if failure == "transport" {
							return nil, io.ErrUnexpectedEOF
						}
						status, body := 200, `{`
						if failure == "server" {
							status = 503
						}
						if failure == "conflict" {
							status = 409
						}
						if capability.Key == "jira.issue.transition.apply" && failure == "malformed" {
							// Этот endpoint допускает пустое успешное тело; моделируем потерю чтения тела после accepted status.
							return &http.Response{StatusCode: 204, Header: http.Header{}, Body: &serverLostResponseBody{}, Request: r}, nil
						}
						return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
					})}
					_, err := adapter.Execute(t.Context(), request)
					if err == nil || mutations != 1 || IsUnknownOutcome(err) != (failure != "conflict") {
						t.Fatalf("wrong mutation outcome: count=%d, %v", mutations, err)
					}
				})
			}
		}
	}
}

// serverLostResponseBody simulates an unreadable success body without carrying provider payload or credentials.
// serverLostResponseBody моделирует нечитаемое success body без provider payload и credentials.
type serverLostResponseBody struct{}

// Read always reports an uncertain body read, not a definitive no-effect response.
// Read всегда сообщает uncertain body read, а не definitive no-effect.
func (*serverLostResponseBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// Close performs no action because the synthetic reader owns no external resource.
// Close ничего не делает: synthetic reader не владеет внешним ресурсом.
func (*serverLostResponseBody) Close() error { return nil }

// TestConfluenceServerRejectsForeignSpaceBeforeBodyRead prevents storage fetch or write after a foreign metadata result.
// TestConfluenceServerRejectsForeignSpaceBeforeBodyRead запрещает storage fetch и write после чужого space в metadata.
func TestConfluenceServerRejectsForeignSpaceBeforeBodyRead(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "synthetic-server-token")
	request := atlassianServerInvocation(t, adapter, confluenceServerDefinition, "confluence.page.read", map[string]any{"page_id": "101"}, credential)
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || strings.Contains(r.URL.Query().Get("expand"), "body.storage") {
			t.Fatal("foreign page reached storage or mutation")
		}
		page := confluenceServerPageFixture("101", "Foreign", 2)
		page["space"] = map[string]string{"key": "OTHER"}
		raw, _ := json.Marshal(page)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})}
	if _, err := adapter.Execute(t.Context(), request); err == nil || calls != 1 {
		t.Fatalf("foreign page accepted: calls=%d, %v", calls, err)
	}
}

// TestConfluenceServerNextRejectsOtherDestinations validates hints without treating them as destinations or scope authority.
// TestConfluenceServerNextRejectsOtherDestinations проверяет hints без выдачи им destination либо scope authority.
func TestConfluenceServerNextRejectsOtherDestinations(t *testing.T) {
	start, limit := 0, 1
	configuration := map[string]string{"base_url": "https://confluence.example.invalid", "context_path": "/docs", "space_key": "DEMO"}
	query := url.Values{"spaceKey": {"DEMO"}}
	page := confluenceServerPages{Start: &start, Limit: &limit, Results: []confluenceServerPage{{ID: "101"}}}
	for _, next := range []string{"/rest/api/content?start=1&limit=1&spaceKey=DEMO", "https://confluence.example.invalid/docs/rest/api/content?start=1"} {
		page.Links.Next = next
		if got, err := confluenceServerNext(configuration, query, page); err != nil || got != "1" {
			t.Fatalf("valid next offset rejected: %q, %v", got, err)
		}
	}
	for _, next := range []string{"https://other.example.invalid/rest/api/content?start=1", "/rest/api/content?start=2", "/rest/api/content?start=1&spaceKey=OTHER", "/rest/api/content?start=1&start=2", "/other?start=1", "/rest/api/content?start=1&cursor=cloud"} {
		page.Links.Next = next
		if _, err := confluenceServerNext(configuration, query, page); err == nil {
			t.Fatalf("unsafe next accepted: %q", next)
		}
	}
}
