package integration

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integrationfixture"
)

// Каждый advertised operation обязан иметь отдельный положительный сценарий.
func catalogInputs() map[string]string {
	return map[string]string{
		"github.repository.metadata.read": `{}`, "github.issue.list": `{"limit":1,"cursor":2}`,
		"github.issue.read": `{"issue_number":3}`, "github.issue.create": `{"title":"Title"}`,
		"github.issue.update": `{"issue_number":3,"title":"Title"}`, "github.issue.comment.create": `{"issue_number":3,"body":"Text"}`,
		"gitlab.project.metadata.read": `{}`, "gitlab.repository.file.read": `{"file_path":"a.txt","ref":"main"}`,
		"gitlab.issue.read": `{"issue_iid":3}`, "gitlab.issue.list": `{"limit":1,"cursor":2}`,
		"gitlab.issue.create": `{"title":"Title"}`, "gitlab.issue.update": `{"issue_iid":3,"title":"Title"}`,
		"gitlab.merge_request.read":              `{"merge_request_iid":3}`,
		"gitlab.merge_request.create":            `{"source_branch":"feature","target_branch":"main","title":"Title"}`,
		"gitlab.merge_request.discussion.create": `{"merge_request_iid":3,"body":"Text"}`,
		"gitlab.branch.create":                   `{"branch":"feature","ref":"main"}`,
		"gitlab.commit.create":                   `{"branch":"feature","action":"create","file_path":"a.txt","content":"Text","commit_message":"Title"}`,
		"gitlab.pipeline.read":                   `{"pipeline_id":3}`, "gitlab.pipeline.retry": `{"pipeline_id":3}`,
		"jira.project.read": `{}`, "jira.issue.search": `{"query":"status = Open","limit":1,"cursor":"cursor-2"}`,
		"jira.issue.read": `{"issue_key":"OPS-3"}`, "jira.issue.create": `{"summary":"Title"}`,
		"jira.issue.comment.write":  `{"issue_key":"OPS-3","body":"Text"}`,
		"jira.issue.update_limited": `{"issue_key":"OPS-3","summary":"Title"}`,
		"jira.issue.link.write":     `{"inward_issue_key":"OPS-3","outward_issue_key":"OPS-4","link_type":"Blocks"}`,
		"confluence.space.read":     `{}`, "confluence.page.search": `{"title":"Title","limit":1,"cursor":"cursor-2"}`,
		"confluence.page.read": `{"page_id":"3"}`, "confluence.page.create": `{"title":"Title","body":"Text","parent_id":"3"}`,
		"confluence.page.update":       `{"page_id":"3","title":"Title","expected_version":1}`,
		"confluence.attachment.upload": `{"page_id":"3","file_name":"a.txt","media_type":"text/plain","content_base64":"VGV4dA=="}`,
		"email.delivery.health.read":   `{}`, "email.message.send": `{"to":"recipient@example.test","subject":"Title","body_text":"Text"}`,
		"email.message.status.read": `{"message_id":"3"}`,
		"synthetic.journal.read":    `{}`, "synthetic.journal.write": `{"value":"Text"}`,
	}
}

func TestEveryAdvertisedOperation(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	inputs := catalogInputs()
	for key, definition := range definitions {
		if !definition.ExecutableBy(integrationpackage.OwnerIntegrationGateway, integrationpackage.RouteManagedMCP) {
			continue
		}
		for _, capability := range definition.Spec.Capabilities {
			t.Run(capability.Operation, func(t *testing.T) {
				raw, ok := inputs[capability.Operation]
				if !ok {
					t.Fatal("advertised operation lacks component scenario")
				}
				var input map[string]any
				if err := json.Unmarshal([]byte(raw), &input); err != nil {
					t.Fatal(err)
				}
				adapter := testAdapter(t)
				var credential *CredentialRevision
				if definition.Spec.Credential != nil {
					credential = testCredential(t, adapter, "test-token")
				}
				calls := 0
				synthetic := integrationfixture.NewHandler(integrationfixture.NewStore())
				synthetic.SetReady(true)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if key == "synthetic" {
						synthetic.ServeHTTP(w, r)
						return
					}
					if r.Header.Get("Authorization") != "Bearer test-token" {
						t.Error("missing authorization")
					}
					body := catalogResponse(t, key, capability.Operation, r)
					w.Header().Set("Content-Type", "application/json")
					if capability.Operation == "github.issue.list" {
						w.Header().Set("Link", `<https://api.github.com/repos/acme/repo/issues?page=3>; rel="next"`)
					}
					_, _ = io.WriteString(w, body)
				}))
				defer server.Close()
				adapter.githubBaseURL = mustParseURL(t, server.URL+"/")
				adapter.githubHTTPClient = server.Client()
				adapter.syntheticBaseURL = mustParseURL(t, server.URL)
				adapter.syntheticClient = server.Client()
				adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Scheme != "https" || r.URL.Host != key+".example.test" {
						t.Error("provider escaped configured origin")
					}
					clone := r.Clone(r.Context())
					endpoint := *r.URL
					endpoint.Scheme = "http"
					endpoint.Host = strings.TrimPrefix(server.URL, "http://")
					clone.URL = &endpoint
					return server.Client().Transport.RoundTrip(clone)
				})}
				request := invocationRequest(t, definition, capability.Key, input, credential)
				result, err := adapter.Execute(t.Context(), request)
				if err != nil || calls == 0 || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest {
					t.Fatalf("operation result: %v, calls=%d", err, calls)
				}
				if strings.HasSuffix(capability.Operation, ".search") || strings.HasSuffix(capability.Operation, ".list") {
					if !strings.Contains(result.Summary, "next_cursor") {
						t.Fatal("pagination cursor lost")
					}
				}
			})
		}
	}
}

func catalogResponse(t *testing.T, provider, operation string, r *http.Request) string {
	t.Helper()
	path := r.URL.Path
	switch provider {
	case "github":
		if !strings.HasPrefix(path, "/repos/acme/repo") {
			t.Fatal("wrong repository")
		}
		if strings.HasSuffix(path, "/comments") {
			if r.Method == "GET" {
				return `[]`
			}
			return `{"id":4}`
		}
		if strings.HasSuffix(path, "/issues") && r.Method == "GET" {
			if operation == "github.issue.create" {
				return `[]`
			}
			if r.URL.Query().Get("page") != "2" {
				t.Error("page lost")
			}
			return `[{"id":3,"number":3,"title":"Title","state":"open"}]`
		}
		if path == "/repos/acme/repo" {
			return `{"id":1,"full_name":"acme/repo","default_branch":"main","visibility":"private","private":true,"archived":false}`
		}
		return `{"id":3,"number":3,"title":"Title","body":"Text","state":"open"}`
	case "gitlab":
		if !strings.HasPrefix(r.URL.EscapedPath(), "/api/v4/projects/group%2Fproject") {
			t.Fatal("wrong project")
		}
		if strings.Contains(path, "/repository/files/") {
			return `{"file_path":"a.txt","ref":"main","blob_id":"blob","encoding":"base64","content":"VGV4dA=="}`
		}
		if strings.HasSuffix(path, "/notes") {
			return `{"id":4}`
		}
		if strings.HasSuffix(path, "/branches") {
			return `{"name":"feature","commit":{"id":"commit"}}`
		}
		if strings.HasSuffix(path, "/commits") {
			return `{"id":"commit","short_id":"commit","web_url":"https://gitlab.example.test/commit"}`
		}
		if strings.Contains(path, "/pipelines/") {
			return `{"id":3,"status":"running","ref":"main","sha":"commit","web_url":"https://gitlab.example.test/pipeline"}`
		}
		if strings.Contains(path, "/merge_requests") {
			return `{"iid":3,"title":"Title","state":"opened","source_branch":"feature","target_branch":"main","web_url":"https://gitlab.example.test/mr"}`
		}
		issue := `{"id":3,"iid":3,"title":"Title","state":"opened","web_url":"https://gitlab.example.test/issue"}`
		if strings.HasSuffix(path, "/issues") && r.Method == "GET" {
			if operation == "gitlab.issue.create" {
				return `[]`
			}
			if r.URL.Query().Get("page") != "2" {
				t.Error("page lost")
			}
			return "[" + issue + "]"
		}
		if strings.Contains(path, "/issues") {
			return issue
		}
		return `{"id":1,"path_with_namespace":"group/project","default_branch":"main","visibility":"private","archived":false}`
	case "jira":
		if strings.HasSuffix(path, "/search/jql") {
			if operation == "jira.issue.create" {
				return `{"issues":[]}`
			}
			if r.URL.Query().Get("nextPageToken") != "cursor-2" {
				t.Error("cursor lost")
			}
			return `{"issues":[{"id":"3","key":"OPS-3","fields":{"summary":"Title","status":{"name":"Open"}}}],"nextPageToken":"cursor-3"}`
		}
		if strings.Contains(path, "/project/") {
			return `{"id":"1","key":"OPS","name":"Project"}`
		}
		if strings.HasSuffix(path, "/comment") {
			return `{"id":"4"}`
		}
		return `{"id":"3","key":"OPS-3","fields":{"summary":"Title","status":{"name":"Open"}}}`
	case "confluence":
		if strings.HasSuffix(path, "/attachment") {
			if r.Header.Get("X-Atlassian-Token") != "nocheck" || r.ParseMultipartForm(64<<10) != nil {
				t.Error("invalid multipart")
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
			} else {
				defer file.Close()
				data, _ := io.ReadAll(file)
				if string(data) != "Text" || header.Filename != "a.txt" {
					t.Error("file changed")
				}
			}
			return `{"results":[{"id":"4","title":"a.txt","metadata":{"mediaType":"text/plain"}}]}`
		}
		if strings.Contains(path, "/spaces/") {
			return `{"id":"42","key":"OPS","name":"Space"}`
		}
		page := `{"id":"3","spaceId":"42","title":"Title","status":"current","version":{"number":1},"body":{"storage":{"value":"Text"}}}`
		if operation == "confluence.page.update" && r.Method == "PUT" {
			return strings.Replace(page, `"number":1`, `"number":2`, 1)
		}
		if operation == "confluence.page.search" {
			if r.URL.Query().Get("cursor") != "cursor-2" {
				t.Error("cursor lost")
			}
			return `{"results":[` + page + `],"_links":{"next":"/wiki/api/v2/pages?cursor=cursor-3"}}`
		}
		return page
	case "email":
		if path == "/v1/health" {
			return `{"status":"ready"}`
		}
		return `{"message_id":"3","status":"accepted"}`
	}
	t.Fatal("unhandled provider")
	return ""
}

func TestEveryMutationPreservesUnknownOutcome(t *testing.T) {
	for operation, raw := range catalogInputs() {
		provider := strings.Split(operation, ".")[0]
		if provider == "github" || provider == "synthetic" {
			continue
		}
		adapter := testAdapter(t)
		definition := adapter.definitions[provider]
		capability, _ := definition.Capability(operation)
		if capability.Risk == "READ" {
			continue
		}
		t.Run(operation, func(t *testing.T) {
			credential := testCredential(t, adapter, "test-token")
			mutations := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					mutations++
					if r.GetBody != nil {
						t.Error("implicit transport retry enabled")
					}
					return nil, errors.New("response lost")
				}
				if strings.Contains(r.URL.Path, "by-idempotency-key") {
					return nil, errors.New("reconciliation unavailable")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(catalogResponse(t, provider, operation, r)))}, nil
			})}
			var input map[string]any
			_ = json.Unmarshal([]byte(raw), &input)
			_, err := adapter.Execute(t.Context(), invocationRequest(t, definition, operation, input, credential))
			if !IsUnknownOutcome(err) || mutations != 1 {
				t.Fatalf("unknown result=%v mutations=%d", err, mutations)
			}
		})
	}
}

func TestScopeDeniedBeforeCredentialRead(t *testing.T) {
	for operation, raw := range catalogInputs() {
		t.Run(operation, func(t *testing.T) {
			adapter := testAdapter(t)
			definition := adapter.definitions[strings.Split(operation, ".")[0]]
			var credential *CredentialRevision
			if definition.Spec.Credential != nil {
				credential = &CredentialRevision{}
			}
			var input map[string]any
			_ = json.Unmarshal([]byte(raw), &input)
			request := invocationRequest(t, definition, operation, input, credential)
			request.ResourceScopeDigest = strings.Repeat("0", 64)
			adapter.credentials = nil
			_, err := adapter.Execute(t.Context(), request)
			if _, code := Outcome(err); code != "INTEGRATION_REQUEST_REJECTED" {
				t.Fatalf("scope error=%v", err)
			}
		})
	}
}

func TestReadOperationsHandleRateLimits(t *testing.T) {
	for operation, raw := range catalogInputs() {
		provider := strings.Split(operation, ".")[0]
		if provider == "synthetic" {
			continue
		}
		adapter := testAdapter(t)
		definition := adapter.definitions[provider]
		capability, _ := definition.Capability(operation)
		if capability.Risk != "READ" {
			continue
		}
		t.Run(operation, func(t *testing.T) {
			credential := testCredential(t, adapter, "test-token")
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				response := &http.Response{Request: r, Header: http.Header{}, StatusCode: 200}
				body := ""
				if calls == 1 {
					response.StatusCode = 429
					response.Header.Set("Retry-After", "0")
					body = `{}`
				} else {
					body = catalogResponse(t, provider, operation, r)
				}
				response.Body = io.NopCloser(strings.NewReader(body))
				return response, nil
			})}
			adapter.providerHTTPClient, adapter.githubHTTPClient = client, client
			var input map[string]any
			_ = json.Unmarshal([]byte(raw), &input)
			if _, err := adapter.Execute(t.Context(), invocationRequest(t, definition, operation, input, credential)); err != nil || calls != 2 {
				t.Fatalf("rate retry err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestEmailNotReadyCannotSend(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Query().Get("sender") != "sender@example.test" {
			t.Fatal("email readiness boundary bypassed")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"status":"not_ready"}`))}, nil
	})}
	var input map[string]any
	_ = json.Unmarshal([]byte(catalogInputs()["email.message.send"]), &input)
	if _, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["email"], "email.message.send", input, credential)); err == nil || calls != 1 {
		t.Fatal("unready bridge accepted send")
	}
}

func TestConfluenceForeignSpaceCannotMutate(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("foreign space was mutated")
		}
		body := strings.Replace(catalogResponse(t, "confluence", "confluence.page.update", r), `"spaceId":"42"`, `"spaceId":"43"`, 1)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	for _, operation := range []string{"confluence.page.update", "confluence.attachment.upload", "confluence.page.create"} {
		var input map[string]any
		_ = json.Unmarshal([]byte(catalogInputs()[operation]), &input)
		if _, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["confluence"], operation, input, credential)); err == nil {
			t.Fatal("foreign space accepted")
		}
	}
}
