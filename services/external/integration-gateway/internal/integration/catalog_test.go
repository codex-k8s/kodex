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
		"github.pull_request.file.list":        `{"pull_request_number":3,"limit":1,"cursor":2}`,
		"gitlab.merge_request.diff.list":       `{"merge_request_iid":3,"limit":1,"cursor":2}`,
		"confluence.space.list":                `{}`,
		"confluence.page.descendant.list":      `{"page_id":"3","limit":1,"cursor":"cursor-2"}`,
		"confluence.page.comment.list":         `{"page_id":"3","limit":1,"cursor":"cursor-2"}`,
		"confluence.page.comment.read":         `{"page_id":"3","comment_id":"4"}`,
		"confluence.page.comment.create":       `{"page_id":"3","body":"Text"}`,
		"confluence.page.comment.update":       `{"page_id":"3","comment_id":"4","expected_version":1,"body":"Text"}`,
		"confluence.page.comment.delete":       `{"page_id":"3","comment_id":"4","expected_version":1}`,
		"confluence.attachment.list":           `{"page_id":"3","limit":1,"cursor":"cursor-2"}`,
		"confluence.attachment.read":           `{"page_id":"3","attachment_id":"4"}`,
		"confluence.attachment.delete":         `{"page_id":"3","attachment_id":"4"}`,
		"jira.project.user.search":             `{"query":"test","limit":1,"cursor":1}`,
		"jira.project.user.read":               `{"account_id":"account-4"}`,
		"jira.issue.transition.list":           `{"issue_key":"OPS-3"}`,
		"jira.issue.transition.apply":          `{"issue_key":"OPS-3","transition_id":"4"}`,
		"jira.issue.comment.list":              `{"issue_key":"OPS-3","limit":1,"cursor":1}`,
		"jira.issue.comment.read":              `{"issue_key":"OPS-3","comment_id":"4"}`,
		"jira.issue.comment.update":            `{"issue_key":"OPS-3","comment_id":"4","body":"Text"}`,
		"jira.issue.comment.delete":            `{"issue_key":"OPS-3","comment_id":"4"}`,
		"jira.issue.link.list":                 `{"issue_key":"OPS-3"}`,
		"jira.issue.link.read":                 `{"issue_key":"OPS-3","link_id":"4"}`,
		"jira.issue.link.delete":               `{"issue_key":"OPS-3","link_id":"4"}`,
		"jira.attachment.list":                 `{"issue_key":"OPS-3"}`,
		"jira.attachment.read":                 `{"issue_key":"OPS-3","attachment_id":"4"}`,
		"jira.attachment.upload":               `{"issue_key":"OPS-3","file_name":"a.txt","media_type":"text/plain","content_base64":"VGV4dA=="}`,
		"jira.attachment.delete":               `{"issue_key":"OPS-3","attachment_id":"4"}`,
		"gitlab.branch.list":                   `{"limit":1,"cursor":2}`,
		"gitlab.branch.read":                   `{"branch":"main"}`,
		"gitlab.branch.delete":                 `{"branch":"feature"}`,
		"gitlab.commit.list":                   `{"ref":"main","limit":1,"cursor":2}`,
		"gitlab.commit.read":                   `{"ref":"abc"}`,
		"gitlab.commit.diff":                   `{"ref":"abc","limit":1,"cursor":2}`,
		"gitlab.repository.tree.list":          `{"ref":"main","limit":1,"cursor":2}`,
		"gitlab.issue.note.list":               `{"issue_iid":3,"limit":1,"cursor":2}`,
		"gitlab.issue.note.read":               `{"issue_iid":3,"note_id":4}`,
		"gitlab.issue.note.create":             `{"issue_iid":3,"body":"Text"}`,
		"gitlab.issue.note.update":             `{"issue_iid":3,"note_id":4,"body":"Text"}`,
		"gitlab.issue.note.delete":             `{"issue_iid":3,"note_id":4}`,
		"gitlab.merge_request.list":            `{"limit":1,"cursor":2}`,
		"gitlab.merge_request.update":          `{"merge_request_iid":3,"title":"Title"}`,
		"gitlab.merge_request.merge":           `{"merge_request_iid":3,"sha":"abc"}`,
		"gitlab.merge_request.discussion.list": `{"merge_request_iid":3,"limit":1,"cursor":2}`,
		"gitlab.pipeline.list":                 `{"limit":1,"cursor":2}`,
		"gitlab.pipeline.cancel":               `{"pipeline_id":3}`,
		"gitlab.job.list":                      `{"pipeline_id":3,"limit":1,"cursor":2}`,
		"gitlab.job.read":                      `{"job_id":4}`,
		"gitlab.job.retry":                     `{"job_id":4}`,
		"gitlab.job.cancel":                    `{"job_id":4}`,
		"gitlab.job.trace.read":                `{"job_id":4}`,
		"github.repository.content.list":       `{"path":"src","ref":"main"}`,
		"github.repository.content.read":       `{"path":"src/a.txt","ref":"main"}`,
		"github.repository.content.create":     `{"path":"src/a.txt","branch":"main","message":"Change","content_base64":"VGV4dA=="}`,
		"github.repository.content.update":     `{"path":"src/a.txt","branch":"main","message":"Change","content_base64":"VGV4dA==","sha":"abc"}`,
		"github.repository.content.delete":     `{"path":"src/a.txt","branch":"main","message":"Change","sha":"abc"}`,
		"github.branch.list":                   `{"limit":1,"cursor":2}`,
		"github.branch.read":                   `{"branch":"main"}`,
		"github.branch.create":                 `{"branch":"feature","sha":"abc"}`,
		"github.branch.delete":                 `{"branch":"feature"}`,
		"github.commit.list":                   `{"ref":"main","limit":1,"cursor":2}`,
		"github.commit.read":                   `{"ref":"abc"}`,
		"github.pull_request.list":             `{"limit":1,"cursor":2}`,
		"github.pull_request.read":             `{"pull_request_number":3}`,
		"github.pull_request.create":           `{"title":"Title","head":"feature","base":"main"}`,
		"github.pull_request.update":           `{"pull_request_number":3,"title":"Title"}`,
		"github.pull_request.merge":            `{"pull_request_number":3,"sha":"abc","merge_method":"merge"}`,
		"github.pull_request.review.list":      `{"pull_request_number":3,"limit":1,"cursor":2}`,
		"github.pull_request.review.read":      `{"pull_request_number":3,"review_id":4}`,
		"github.pull_request.review.create":    `{"pull_request_number":3,"sha":"abc","event":"COMMENT","body":"Text"}`,
		"github.issue.comment.list":            `{"issue_number":3,"limit":1,"cursor":2}`,
		"github.issue.comment.read":            `{"issue_number":3,"comment_id":4}`,
		"github.issue.comment.update":          `{"issue_number":3,"comment_id":4,"body":"Text"}`,
		"github.issue.comment.delete":          `{"issue_number":3,"comment_id":4}`,
		"github.check_run.list":                `{"ref":"abc","limit":1,"cursor":2}`,
		"github.check_run.read":                `{"check_run_id":4}`,
		"github.actions.workflow.list":         `{"limit":1,"cursor":2}`,
		"github.actions.workflow.read":         `{"workflow_id":4}`,
		"github.actions.workflow.dispatch":     `{"workflow_id":4,"ref":"main"}`,
		"github.actions.run.list":              `{"limit":1,"cursor":2}`,
		"github.actions.run.read":              `{"run_id":4}`,
		"github.actions.run.rerun":             `{"run_id":4}`,
		"github.actions.run.cancel":            `{"run_id":4}`,
		"github.actions.job.list":              `{"run_id":4,"limit":1,"cursor":2}`,
		"github.actions.job.read":              `{"run_id":4,"job_id":5}`,
		"github.repository.metadata.read":      `{}`, "github.issue.list": `{"limit":1,"cursor":2}`,
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
		"email.mailbox.list":        `{}`,
		"email.message.list":        `{"cursor":"cursor-2"}`,
		"email.message.search":      `{"query":"Title","cursor":"cursor-2"}`,
		"email.message.read":        `{"uid":"uid-1"}`,
		"email.attachment.read":     `{"uid":"uid-1","attachment_index":0}`,
		"email.message.reply":       `{"to":"recipient@example.test","subject":"Title","body_text":"Text","source_uid":"uid-1"}`,
		"email.message.reply_all":   `{"to":"recipient@example.test","subject":"Title","body_text":"Text","source_uid":"uid-1","cc":"[\"copy@example.test\"]"}`,
		"email.message.forward":     `{"to":"recipient@example.test","subject":"Title","body_text":"Text","source_uid":"uid-1","attachments":"[{\"filename\":\"a.txt\",\"content_type\":\"text/plain\",\"content_base64\":\"VGV4dA==\"}]"}`,
		"email.message.delete":      `{"uid":"uid-1"}`,
		"email.thread.read":         `{"thread_id":"source@example.test"}`,
		"email.attachment.list":     `{"uid":"1","uid_validity":1}`,
		"email.message.mark_read":   `{"uid":"1","uid_validity":1}`,
		"email.message.mark_unread": `{"uid":"1","uid_validity":1}`,
		"email.message.move":        `{"uid":"1","uid_validity":1,"destination_folder":"Archive"}`,
		"email.message.archive":     `{"uid":"1","uid_validity":1}`,
		"email.draft.create":        `{"to":"recipient@example.test","subject":"Title","body_text":"Text"}`,
		"email.draft.update":        `{"uid":"1","uid_validity":1,"expected_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","to":"recipient@example.test","subject":"Title","body_text":"Text"}`,
		"email.draft.delete":        `{"uid":"1","uid_validity":1}`,
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
					expectedToken := "Bearer test-token"
					if key == "email" {
						expectedToken = "Bearer fixture-fence"
					}
					if r.Header.Get("Authorization") != expectedToken {
						t.Error("missing authorization")
					}
					body := catalogResponse(t, key, capability.Operation, r)
					w.Header().Set("Content-Type", "application/json")
					if key == "github" && strings.HasSuffix(capability.Operation, ".list") && capability.Operation != "github.repository.content.list" {
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
					expectedHost := key + ".example.test"
					if key == "email" {
						expectedHost = "email-bridge.kodex-system.svc.cluster.local"
					}
					if r.URL.Scheme != "https" || r.URL.Host != expectedHost {
						t.Error("provider escaped configured origin")
					}
					clone := r.Clone(r.Context())
					endpoint := *r.URL
					endpoint.Scheme = "http"
					endpoint.Host = strings.TrimPrefix(server.URL, "http://")
					clone.URL = &endpoint
					return server.Client().Transport.RoundTrip(clone)
				})}
				adapter.emailHTTPClient = adapter.providerHTTPClient
				request := invocationRequest(t, definition, capability.Key, input, credential)
				result, err := adapter.Execute(t.Context(), request)
				if err != nil || calls == 0 || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest {
					t.Fatalf("operation result: %v, calls=%d", err, calls)
				}
				if capability.Operation != "email.mailbox.list" && capability.Operation != "email.attachment.list" && (strings.HasSuffix(capability.Operation, ".search") || strings.HasSuffix(capability.Operation, ".list")) && request.Input["cursor"] != nil {
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
	if body, ok := confluenceExtendedResponse(t, operation, r); ok {
		return body
	}
	if body, ok := jiraExtendedResponse(t, operation, r); ok {
		return body
	}
	if body, ok := gitLabExtendedResponse(t, operation, r); ok {
		return body
	}
	if body, ok := githubExtendedResponse(t, operation, r); ok {
		return body
	}
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
		if operation == "email.delivery.health.read" {
			return `{"status":"ready"}`
		}
		if operation == "email.message.delete" || operation == "email.draft.delete" {
			return `{"message_id":"3","status":"deleted"}`
		}
		if operation == "email.mailbox.list" {
			return `{"status":"ok","mailboxes":["mailbox"]}`
		}
		if operation == "email.message.list" || operation == "email.message.search" || operation == "email.thread.read" {
			return `{"status":"ok","headers":[{"uid":"uid-1","from":"sender@example.test","to":"recipient@example.test","subject":"Title","size":10}],"next_cursor":"cursor-3"}`
		}
		if operation == "email.message.read" || operation == "email.attachment.read" || operation == "email.attachment.list" {
			return `{"status":"ok","body_text":"Text","attachments":[{"filename":"a.txt","content_type":"text/plain","content_base64":"VGV4dA=="}]}`
		}
		return `{"message_id":"3","status":"accepted"}`
	}
	t.Fatal("unhandled provider")
	return ""
}

func TestEveryMutationPreservesUnknownOutcome(t *testing.T) {
	for operation, raw := range catalogInputs() {
		provider := strings.Split(operation, ".")[0]
		if provider == "synthetic" {
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
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
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
				body := catalogResponse(t, provider, operation, r)
				if operation == "github.issue.update" {
					body = strings.ReplaceAll(body, `"title":"Title"`, `"title":"Before"`)
				}
				return &http.Response{Request: r, StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			adapter.providerHTTPClient, adapter.githubHTTPClient, adapter.emailHTTPClient = client, client, client
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
			adapter.providerHTTPClient, adapter.githubHTTPClient, adapter.emailHTTPClient = client, client, client
			var input map[string]any
			_ = json.Unmarshal([]byte(raw), &input)
			_, err := adapter.Execute(t.Context(), invocationRequest(t, definition, operation, input, credential))
			if provider == "email" {
				if err == nil || calls != 1 {
					t.Fatal("email rate limit must fail closed without retry")
				}
				return
			}
			expectedCalls := 2
			if strings.HasPrefix(operation, "confluence.") && operation != "confluence.space.list" && operation != "confluence.space.read" && operation != "confluence.page.read" && operation != "confluence.page.search" {
				expectedCalls = 3
			}
			if operation == "confluence.attachment.read" {
				expectedCalls = 4
			}
			if operation == "github.issue.comment.list" || operation == "github.issue.comment.read" || operation == "jira.attachment.read" {
				expectedCalls = 3
			}
			if err != nil || calls != expectedCalls {
				t.Fatalf("rate retry err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestEmailNotReadyCannotSend(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "test-token")
	calls := 0
	adapter.emailHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/v1/mailbox-operations" {
			t.Fatal("email readiness boundary bypassed")
		}
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":"UNAVAILABLE"}`))}, nil
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
