package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCatalogPinnedAuthorityBeforeCredentials(t *testing.T) {
	adapter := testAdapter(t)
	adapter.credentials = nil
	for operation, raw := range catalogInputs() {
		for _, change := range []string{"version", "operation", "risk", "approval", "scope", "input"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				provider := strings.Split(operation, ".")[0]
				var input map[string]any
				_ = json.Unmarshal([]byte(raw), &input)
				request := invocationRequest(t, adapter.definitions[provider], operation, input, &CredentialRevision{})
				switch change {
				case "version":
					request.DefinitionVersion = "999.0.0"
				case "operation":
					request.Operation += ".unregistered"
				case "risk":
					request.Risk = "UNREGISTERED"
				case "approval":
					request.ApprovalPolicy = "UNREGISTERED"
				case "scope":
					request.ResourceScope = map[string]string{"foreign": "resource"}
				case "input":
					request.Input["url"] = "https://foreign.example.test/"
				}
				if _, err := adapter.Execute(t.Context(), request); err == nil {
					t.Fatal("untrusted invocation reached credential path")
				}
			})
		}
	}
}

func TestCatalogMutationProviderFailuresNeverReplay(t *testing.T) {
	for operation, raw := range catalogInputs() {
		provider := strings.Split(operation, ".")[0]
		if provider == "email" || provider == "synthetic" {
			continue
		}
		adapter := testAdapter(t)
		capability, ok := adapter.definitions[provider].Capability(operation)
		if !ok || capability.Risk == "READ" {
			continue
		}
		for _, failure := range []string{"server_error", "denied", "invalid_success"} {
			// Эти Jira endpoints подтверждают эффект без JSON response body.
			if failure == "invalid_success" && (operation == "jira.issue.link.write" || operation == "jira.issue.update_limited") {
				continue
			}
			if failure == "invalid_success" && len(capability.OutputFields) == 1 && capability.OutputFields[0].Key == "accepted" {
				continue
			}
			t.Run(operation+"/"+failure, func(t *testing.T) {
				adapter := testAdapter(t)
				credential := testCredential(t, adapter, "test-token")
				mutations := 0
				client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					response := &http.Response{Request: r, StatusCode: 200, Header: http.Header{}}
					body := ""
					if r.Method == "GET" {
						body = catalogResponse(t, provider, operation, r)
						if operation == "github.issue.update" {
							body = strings.ReplaceAll(body, `"title":"Title"`, `"title":"Before"`)
						}
					} else {
						mutations++
						if r.GetBody != nil {
							t.Error("mutation replay is enabled")
						}
						switch failure {
						case "server_error":
							response.StatusCode = 503
						case "denied":
							response.StatusCode = 403
						}
						body = "{"
					}
					response.Body = io.NopCloser(strings.NewReader(body))
					return response, nil
				})}
				adapter.providerHTTPClient, adapter.githubHTTPClient = client, client
				var input map[string]any
				_ = json.Unmarshal([]byte(raw), &input)
				_, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions[provider], operation, input, credential))
				if err == nil || mutations != 1 || IsUnknownOutcome(err) != (failure != "denied") {
					t.Fatalf("failure=%s err=%v mutations=%d", failure, err, mutations)
				}
			})
		}
	}
}

func TestCatalogResourceIdentifiersBeforeCredentialRead(t *testing.T) {
	for _, scenario := range []struct {
		operation string
		input     map[string]any
	}{
		{"github.repository.content.read", map[string]any{"path": "../other"}},
		{"github.commit.read", map[string]any{"ref": "../../other"}},
		{"gitlab.branch.read", map[string]any{"branch": ".."}},
		{"gitlab.commit.read", map[string]any{"ref": "../other"}},
		{"jira.issue.transition.apply", map[string]any{"issue_key": "OTHER-3", "transition_id": "4"}},
		{"jira.attachment.delete", map[string]any{"issue_key": "OPS-3", "attachment_id": "../4"}},
		{"confluence.page.comment.read", map[string]any{"page_id": "3", "comment_id": "../4"}},
		{"confluence.attachment.read", map[string]any{"page_id": "3", "attachment_id": "https://other/4"}},
	} {
		t.Run(scenario.operation, func(t *testing.T) {
			adapter := testAdapter(t)
			provider := strings.Split(scenario.operation, ".")[0]
			request := invocationRequest(t, adapter.definitions[provider], scenario.operation, scenario.input, &CredentialRevision{})
			adapter.credentials = nil
			if _, err := adapter.Execute(t.Context(), request); err == nil {
				t.Fatal("foreign identifier accepted")
			}
		})
	}
}

func TestGitHubFileNamesAreEscapedAndEmptyFilesRemainUsable(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" || r.URL.Path != "/repos/acme/repo/contents/src/a?b#c.txt" || r.URL.RawQuery != "" || r.URL.Fragment != "" {
			t.Fatal("file name changed request routing")
		}
		var body struct {
			Content string `json:"content"`
			Branch  string `json:"branch"`
			Message string `json:"message"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Content != "" || body.Branch != "main" || body.Message != "Empty file" {
			t.Fatal("empty file request changed")
		}
		return &http.Response{Request: r, StatusCode: 201, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"commit":{"sha":"abc"}}`))}, nil
	})}
	request := invocationRequest(t, adapter.definitions["github"], "github.repository.content.create", map[string]any{"path": "src/a?b#c.txt", "branch": "main", "message": "Empty file", "content_base64": ""}, credential)
	if _, err := adapter.Execute(t.Context(), request); err != nil {
		t.Fatal(err)
	}
}

func TestProviderFilesAreBoundedBeforeDecoding(t *testing.T) {
	for _, operation := range []string{"github.repository.content.read", "gitlab.repository.file.read", "jira.attachment.read", "confluence.attachment.read"} {
		t.Run(operation, func(t *testing.T) {
			adapter := testAdapter(t)
			provider := strings.Split(operation, ".")[0]
			credential := testCredential(t, adapter, "test-token")
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{Request: r, StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", maximumResponseBytes+1)))}, nil
			})}
			adapter.githubHTTPClient, adapter.providerHTTPClient = client, client
			var input map[string]any
			_ = json.Unmarshal([]byte(catalogInputs()[operation]), &input)
			if result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions[provider], operation, input, credential)); err == nil || result.Summary != "" || calls > 3 {
				t.Fatalf("unbounded response err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestGitLabPaginationUsesExactProviderCursor(t *testing.T) {
	for _, next := range []string{"", "3", "1", "10001", "opaque"} {
		t.Run("next="+next, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "test-token")
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{Request: r, StatusCode: 200, Header: http.Header{"X-Next-Page": {next}}, Body: io.NopCloser(strings.NewReader(`[{"name":"main","commit":{"id":"abc"}}]`))}, nil
			})}
			result, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions["gitlab"], "gitlab.branch.list", map[string]any{"limit": 1, "cursor": 2}, credential))
			if next != "" && next != "3" {
				if err == nil {
					t.Fatal("invalid cursor accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result.Summary, "next_cursor") != (next == "3") {
				t.Fatal("provider pagination terminal state lost")
			}
		})
	}
}

func TestJiraQueryScopePreservesSortingAndRejectsEscapes(t *testing.T) {
	for _, query := range []string{`status = Open`, `status = Open ORDER BY created DESC`, `summary ~ "ORDER BY" AND (status = Open OR status = Closed)`, `summary ~ "a\\\"b"`} {
		result, ok := scopedJiraQuery("OPS", query)
		if !ok || !strings.HasPrefix(result, `project = "OPS" AND (`) {
			t.Fatalf("valid query rejected: %q", query)
		}
		if query == `status = Open ORDER BY created DESC` && result != `project = "OPS" AND (status = Open ) ORDER BY created DESC` {
			t.Fatal("sorting not preserved")
		}
	}
	for _, query := range []string{`status = Open) OR project = OTHER`, `summary ~ "unterminated`, `(status = Open`, `ORDER BY created`} {
		if _, ok := scopedJiraQuery("OPS", query); ok {
			t.Fatal("unclosed query accepted")
		}
	}
}

func TestWorkflowInputsAreTypedAndBounded(t *testing.T) {
	inputs, err := githubWorkflowInputs(`{"name":"release","dry_run":true,"count":2}`)
	if err != nil || inputs["name"] != "release" || inputs["dry_run"] != true {
		t.Fatal("typed workflow inputs rejected")
	}
	for _, raw := range []string{`{"name":"one","name":"two"}`, `{"config":{"url":"https://other"}}`, `{"items":[]}`, `{"name":null}`, `{} {}`} {
		if _, err := githubWorkflowInputs(raw); err == nil {
			t.Fatal("unbounded workflow input accepted")
		}
	}
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body struct {
			Ref    string         `json:"ref"`
			Inputs map[string]any `json:"inputs"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Ref != "main" || body.Inputs["dry_run"] != true {
			t.Fatal("workflow inputs were not materialized")
		}
		return &http.Response{Request: r, StatusCode: 204, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	request := invocationRequest(t, adapter.definitions["github"], "github.actions.workflow.dispatch", map[string]any{"workflow_id": 4, "ref": "main", "workflow_inputs": `{"dry_run":true}`}, credential)
	if _, err := adapter.Execute(t.Context(), request); err != nil {
		t.Fatal(err)
	}
}

func TestRelatedResourcesNeverEscapeParentScope(t *testing.T) {
	for _, operation := range []string{"jira.attachment.read", "jira.attachment.delete", "jira.issue.link.read", "jira.issue.link.delete", "confluence.page.comment.update", "confluence.attachment.read", "confluence.attachment.delete"} {
		t.Run(operation, func(t *testing.T) {
			adapter := testAdapter(t)
			provider := strings.Split(operation, ".")[0]
			credential := testCredential(t, adapter, "test-token")
			calls := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" {
					t.Fatal("foreign resource mutation reached provider")
				}
				body := ""
				if provider == "jira" {
					if r.URL.Path != "/rest/api/3/issue/OPS-3" {
						t.Fatal("foreign Jira related resource endpoint reached")
					}
					body = `{"key":"OPS-3","fields":{"attachment":[{"id":"99","filename":"other","size":4}],"issuelinks":[{"id":"4","type":{"name":"Blocks"},"outwardIssue":{"key":"OTHER-4"}}]}}`
				} else if r.URL.Path == "/wiki/api/v2/pages/3" {
					body = `{"id":"3","spaceId":"42","title":"Title","status":"current","version":{"number":1}}`
				} else {
					if strings.Contains(r.URL.Path, "download") {
						t.Fatal("foreign attachment download reached")
					}
					body = `{"id":"4","pageId":"99","status":"current","version":{"number":1}}`
				}
				return &http.Response{Request: r, StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			var input map[string]any
			_ = json.Unmarshal([]byte(catalogInputs()[operation]), &input)
			if _, err := adapter.Execute(t.Context(), invocationRequest(t, adapter.definitions[provider], operation, input, credential)); err == nil || calls > 2 {
				t.Fatalf("foreign result err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestConfluenceReplyBindsVerifiedParent(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	calls := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"id":"3","spaceId":"42","version":{"number":1}}`
		if r.URL.Path == "/wiki/api/v2/footer-comments/4" {
			body = `{"id":"4","pageId":"3","version":{"number":1}}`
		}
		if r.Method == "POST" {
			var payload struct {
				PageID string `json:"pageId"`
				Parent string `json:"parentCommentId"`
			}
			if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.PageID != "" || payload.Parent != "4" || calls != 3 {
				t.Fatal("reply parent was not resolved")
			}
			body = `{"id":"5","pageId":"3","parentCommentId":"4","status":"current","version":{"number":1},"body":{"storage":{"value":"Text"}}}`
		}
		return &http.Response{Request: r, StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	request := invocationRequest(t, adapter.definitions["confluence"], "confluence.page.comment.create", map[string]any{"page_id": "3", "parent_comment_id": "4", "body": "Text"}, credential)
	if _, err := adapter.Execute(t.Context(), request); err != nil {
		t.Fatal(err)
	}
}
