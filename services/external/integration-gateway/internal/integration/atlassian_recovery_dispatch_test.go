package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// atlassianRecoveryFixture supplies only synthetic Server metadata at fixed real adapter routes.
// It never authorizes a platform lease, reads a live key or accepts a mutation during recovery.
// atlassianRecoveryFixture задаёт только synthetic Server metadata для fixed adapter routes.
// Platform lease не разрешается, live key не читается, mutation при recovery не принимается.
func atlassianRecoveryFixture(t *testing.T, request Request, r *http.Request, scenario string, calls int) *http.Response {
	t.Helper()
	if calls > 16 || r.Method != http.MethodGet || r.URL.Scheme != "https" {
		t.Fatal("recovery escaped finite GET-only boundary")
	}
	if request.Configuration["auth_scheme"] == "BASIC" {
		username, password, ok := r.BasicAuth()
		if !ok || username != "synthetic-user" || password != "synthetic-server-token" {
			t.Fatal("recovery Basic identity changed")
		}
	} else if r.Header.Get("Authorization") != "Bearer synthetic-server-token" {
		t.Fatal("recovery key changed")
	}
	prefix := request.Configuration["context_path"].(string)
	host := strings.TrimPrefix(request.Configuration["base_url"].(string), "https://")
	if r.URL.Host != host || !strings.HasPrefix(r.URL.Path, prefix+"/rest/api/") {
		t.Fatal("recovery left configured host/context")
	}
	path := strings.TrimPrefix(r.URL.Path, prefix)
	status := 200
	var value any
	if scenario == "401" && calls == 1 {
		status = 401
		value = map[string]string{"error": "synthetic rejection"}
	} else if scenario == "403" && calls == 1 {
		status = 403
		value = map[string]string{"error": "synthetic forbidden"}
	} else if scenario == "404" && calls == 1 {
		status = 404
		value = map[string]string{"error": "synthetic missing"}
	} else if scenario == "429" && calls == 1 {
		status = 429
		value = map[string]string{"error": "synthetic limited"}
	} else if request.DefinitionKey == jiraServerDefinition {
		issue := jiraServerIssueFixture()
		fields := issue["fields"].(map[string]any)
		fields["project"] = map[string]string{"id": "1", "key": "DEMO"}
		fields["creator"] = map[string]string{"key": "synthetic-user"}
		fields["issuetype"] = map[string]string{"name": "Task"}
		fields["labels"] = []string{"kodex-effect-" + request.EffectKey}
		if scenario == "foreign author" {
			fields["creator"] = map[string]string{"key": "other-user"}
		}
		if scenario == "foreign project" {
			fields["project"] = map[string]string{"id": "2", "key": "OTHER"}
		}
		if scenario == "wrong type" {
			fields["issuetype"] = map[string]string{"name": "Other"}
		}
		if scenario == "wrong body" {
			fields["description"] = "other"
		}
		if scenario == "missing author" {
			delete(fields, "creator")
		}
		marker := "[kodex-effect:" + request.EffectKey + "]"
		comment := map[string]any{"id": "801", "body": "Comment\n\n" + marker, "author": map[string]string{"key": "synthetic-user"}, "updateAuthor": map[string]string{"key": "synthetic-user"}}
		if scenario == "foreign author" {
			comment["author"] = map[string]string{"key": "other-user"}
		}
		if scenario == "wrong body" {
			comment["body"] = "other\n\n" + marker
		}
		if scenario == "missing author" {
			delete(comment, "author")
		}
		if scenario == "wrong updater" {
			comment["updateAuthor"] = map[string]string{"key": "other-user"}
		}
		switch path {
		case "/rest/api/2/myself":
			value = map[string]any{"key": "synthetic-user", "name": "synthetic-user", "displayName": "Synthetic User", "active": true}
			if scenario == "inactive" {
				value.(map[string]any)["active"] = false
			}
			if scenario == "missing activity" {
				delete(value.(map[string]any), "active")
			}
		case "/rest/api/2/project/DEMO":
			value = map[string]string{"id": "1", "key": "DEMO", "name": "Synthetic project"}
		case "/rest/api/2/search":
			if !strings.Contains(r.URL.Query().Get("jql"), `project = "DEMO"`) || !strings.Contains(r.URL.Query().Get("jql"), "kodex-effect-"+request.EffectKey) || !strings.Contains(r.URL.Query().Get("fields"), "creator") {
				t.Fatal("create recovery omitted project/marker/creator")
			}
			offset, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
			rows := []any{issue}
			total, maxResults := 1, 50
			if scenario == "empty" {
				rows = []any{}
				total = 0
			}
			if scenario == "duplicate" {
				rows = append(rows, issue)
				total = 2
			}
			if scenario == "split duplicate" {
				total, maxResults = 2, 1
				if offset == 1 {
					issue["id"], issue["key"] = "2", "DEMO-2"
				}
			}
			value = map[string]any{"startAt": offset, "maxResults": maxResults, "total": total, "issues": rows}
			if scenario == "missing total" {
				delete(value.(map[string]any), "total")
			}
		case "/rest/api/2/issue/DEMO-1":
			value = issue
			if scenario == "detail changed" && request.Operation == "jira.issue.create" {
				fields["summary"] = "other"
			}
		case "/rest/api/2/issue/DEMO-1/comment":
			offset, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
			rows := []any{comment}
			total, maxResults := 1, 50
			if scenario == "empty" {
				rows = []any{}
				total = 0
			}
			if scenario == "duplicate" {
				other := map[string]any{}
				for k, v := range comment {
					other[k] = v
				}
				other["id"] = "802"
				rows = append(rows, other)
				total = 2
			}
			if scenario == "second page" || scenario == "page exhausted" || scenario == "repeated row" || scenario == "total drift" {
				total = 51
				if offset == 0 || scenario == "page exhausted" {
					count := 50
					if scenario == "page exhausted" {
						count = 1
						maxResults = 1
						total = 250
					}
					rows = make([]any, count)
					for i := range rows {
						rows[i] = map[string]any{"id": strconv.Itoa(10000 + offset + i), "body": "Unrelated", "author": map[string]string{"key": "other-user"}, "updateAuthor": map[string]string{"key": "other-user"}}
					}
					if scenario == "repeated row" {
						rows[1] = rows[0]
					}
				}
				if scenario == "total drift" && offset > 0 {
					total = 52
				}
			}
			value = map[string]any{"startAt": offset, "maxResults": maxResults, "total": total, "comments": rows}
			if scenario == "missing total" {
				delete(value.(map[string]any), "total")
			}
		case "/rest/api/2/issue/DEMO-1/comment/801":
			value = comment
			if scenario == "detail changed" {
				comment["body"] = "changed"
			}
		default:
			t.Fatalf("unexpected Jira recovery route: %s", path)
		}
	} else {
		var input confluenceRecoveryInput
		encoded, _ := json.Marshal(request.Input)
		_ = json.Unmarshal(encoded, &input)
		marker := confluenceServerMarker(request)
		title, body := input.Title, input.Body
		if title == "" {
			title = "Fixture"
		}
		if body == "" {
			body = "<p>old</p>"
		}
		id, version := "102", 1
		if request.Operation == "confluence.page.update" {
			id, version = input.PageID, int(input.ExpectedVersion+1)
		}
		page := confluenceServerPageFixture(id, title, version)
		page["version"] = map[string]any{"number": version, "by": map[string]string{"type": "known", "userKey": "synthetic-user"}}
		page["history"] = map[string]any{"createdBy": map[string]string{"type": "known", "userKey": "synthetic-user"}}
		page["body"].(map[string]any)["storage"].(map[string]string)["value"] = body + "\n" + marker
		if input.ParentID != "" {
			page["ancestors"] = []any{map[string]string{"id": input.ParentID}}
		}
		if scenario == "foreign author" {
			page["version"].(map[string]any)["by"] = map[string]string{"type": "known", "userKey": "other-user"}
			page["history"] = map[string]any{"createdBy": map[string]string{"type": "known", "userKey": "other-user"}}
		}
		if scenario == "missing author" {
			page["version"] = map[string]int{"number": version}
			delete(page, "history")
		}
		if scenario == "foreign project" {
			page["space"] = map[string]string{"key": "OTHER"}
		}
		if scenario == "wrong body" {
			page["body"].(map[string]any)["storage"].(map[string]string)["value"] = "other\n" + marker
		}
		if scenario == "wrong title" {
			page["title"] = "other"
		}
		if scenario == "later version" {
			page["version"].(map[string]any)["number"] = version + 1
		}
		if scenario == "duplicate marker" {
			page["body"].(map[string]any)["storage"].(map[string]string)["value"] += marker
		}
		switch path {
		case "/rest/api/user/current":
			value = map[string]string{"type": "known", "userKey": "synthetic-user", "username": "synthetic-user", "displayName": "Synthetic User"}
		case "/rest/api/space/DEMO":
			value = map[string]any{"id": 1, "key": "DEMO", "name": "Synthetic space"}
		case "/rest/api/content":
			if r.URL.Query().Get("spaceKey") != "DEMO" || r.URL.Query().Get("title") != input.Title || !strings.Contains(r.URL.Query().Get("expand"), "history") {
				t.Fatal("page recovery lost scope or creator expansion")
			}
			rows := []any{page}
			if scenario == "empty" {
				rows = []any{}
			}
			if scenario == "duplicate" {
				other := map[string]any{}
				for k, v := range page {
					other[k] = v
				}
				other["id"] = "103"
				rows = append(rows, other)
			}
			value = map[string]any{"start": 0, "limit": 20, "size": len(rows), "results": rows, "_links": map[string]any{}}
			if scenario == "missing links" {
				delete(value.(map[string]any), "_links")
			}
			if scenario == "foreign next" {
				value.(map[string]any)["_links"] = map[string]string{"next": "https://attacker.example.invalid/rest/api/content?start=1"}
			}
		case "/rest/api/content/200":
			value = confluenceServerPageFixture("200", "Parent", 2)
		case "/rest/api/content/101", "/rest/api/content/102":
			if scenario == "detail changed" {
				page["body"].(map[string]any)["storage"].(map[string]string)["value"] = "changed detail"
			}
			if r.URL.Query().Get("status") == "historical" {
				wanted, _ := strconv.Atoi(r.URL.Query().Get("version"))
				if wanted != 2 {
					t.Fatal("historical recovery changed pinned predecessor")
				}
				page = confluenceServerPageFixture(input.PageID, "Fixture", wanted)
				page["status"] = "historical"
				if scenario == "wrong history" {
					page["version"] = map[string]int{"number": 99}
				}
				if scenario == "wrong preserved body" {
					page["body"].(map[string]any)["storage"].(map[string]string)["value"] = "changed predecessor"
				}
			} else if r.URL.Query().Get("status") != "current" {
				t.Fatal("recovery current read omitted fixed status")
			}
			if !strings.Contains(r.URL.Query().Get("expand"), "body.storage") {
				if scenario == "metadata changed" || scenario == "history metadata changed" && r.URL.Query().Get("status") == "historical" {
					page["title"] = "inconsistent metadata"
				}
				delete(page, "body")
			}
			value = page
		default:
			t.Fatalf("unexpected Confluence recovery route: %s", path)
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if scenario == "duplicate JSON" && calls == 1 {
		raw = []byte(`{"key":"synthetic-user","Key":"other-user","active":true}`)
	}
	response := &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}
	if scenario == "allow" || scenario == "second page" || scenario == "title only" || scenario == "body only" || scenario == "parent" {
		contract := validateAtlassianWireRequest(t, request, r)
		validateAtlassianWireResponse(t, contract, response)
	}
	return response
}

// TestAtlassianActualRecoverDispatch qualifies four native Recover operations, owner profiles,
// fixed context/auth, full synthetic OpenAPI wire and negative attribution/pagination cases.
// These synthetic HTTP results do not claim installed owner authority or live write permission.
// TestAtlassianActualRecoverDispatch проверяет четыре native Recover операции, owner profiles,
// fixed context/auth, полный synthetic OpenAPI wire и negative attribution/pagination cases.
// Synthetic HTTP results не подтверждают installed owner authority либо live write permission.
func TestAtlassianActualRecoverDispatch(t *testing.T) {
	for _, operation := range []string{"jira.issue.create", "jira.issue.comment.write", "confluence.page.create", "confluence.page.update"} {
		key := jiraServerDefinition
		if strings.HasPrefix(operation, "confluence.") {
			key = confluenceServerDefinition
		}
		scenarios := []string{"allow", "empty", "foreign author", "missing author", "foreign project", "wrong body", "detail changed", "duplicate JSON", "401", "403", "404", "429", "timeout"}
		if strings.HasPrefix(operation, "jira.") {
			scenarios = append(scenarios, "inactive", "missing activity")
		}
		if operation == "jira.issue.create" {
			scenarios = append(scenarios, "duplicate", "split duplicate", "wrong type", "missing total")
		}
		if operation == "jira.issue.comment.write" {
			scenarios = append(scenarios, "duplicate", "missing total", "second page", "page exhausted", "repeated row", "total drift", "wrong updater")
		}
		if operation == "confluence.page.create" {
			scenarios = append(scenarios, "duplicate", "foreign next", "missing links", "metadata changed", "later version", "duplicate marker", "wrong title", "parent")
		}
		if operation == "confluence.page.update" {
			scenarios = []string{"allow", "title only", "body only", "foreign author", "missing author", "foreign project", "wrong body", "duplicate JSON", "401", "403", "404", "429", "timeout", "later version", "duplicate marker", "wrong title", "wrong history", "wrong preserved body", "metadata changed", "history metadata changed"}
		}
		for _, personal := range []bool{false, true} {
			for _, scenario := range scenarios {
				t.Run(operation+"/"+strconv.FormatBool(personal)+"/"+scenario, func(t *testing.T) {
					adapter := testAdapter(t)
					credential := testCredential(t, adapter, "synthetic-server-token")
					input := atlassianServerInput(operation)
					if scenario == "title only" || scenario == "wrong preserved body" {
						delete(input, "body")
					}
					if scenario == "body only" {
						delete(input, "title")
					}
					if scenario == "parent" {
						input["parent_id"] = "200"
					}
					request := atlassianServerInvocation(t, adapter, key, operation, input, credential)
					if personal {
						request.PersonalCredentialAdmission = true
						request.ResourcePermissionProfile = integrationpackage.CredentialResourcePermissionProfile(adapter.definitions[key], mustAtlassianCapability(t, adapter.definitions[key], operation))
					}
					calls := 0
					adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
						calls++
						if scenario == "timeout" && calls == 1 {
							return nil, context.DeadlineExceeded
						}
						return atlassianRecoveryFixture(t, request, r, scenario, calls), nil
					})}
					result, err := adapter.Recover(t.Context(), request)
					success := scenario == "allow" || scenario == "second page" || scenario == "title only" || scenario == "body only" || scenario == "parent"
					if success {
						if err != nil || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest || result.Receipt.ProviderEffectRef == "" {
							t.Fatal("native recovery lost exact effect provenance", err)
						}
					} else if err == nil || result.Receipt.ProviderEffectRef != "" {
						t.Fatal("unproven Server recovery fabricated a receipt")
					}
					raw, _ := json.Marshal(result)
					if strings.Contains(string(raw), "synthetic-server-token") || strings.Contains(string(raw), "synthetic-user") {
						t.Fatal("recovery receipt exposed credential/identity metadata")
					}
				})
			}
		}
	}
}

// mustAtlassianCapability resolves a synthetic published operation descriptor, not a grant.
// mustAtlassianCapability разрешает synthetic published operation descriptor, а не grant.
func mustAtlassianCapability(t *testing.T, definition integrationpackage.Package, operation string) integrationpackage.Capability {
	t.Helper()
	capability, ok := definition.Capability(operation)
	if !ok {
		t.Fatal("missing Server recovery capability")
	}
	return capability
}

// TestAtlassianRecoveryBasicAndOwnerProfile preserves explicit legacy Basic selection and
// rejects mismatched personal consumer profiles before any external identity or page is read.
// TestAtlassianRecoveryBasicAndOwnerProfile сохраняет явный legacy Basic selection и
// отклоняет mismatched personal consumer profiles до external identity либо page read.
func TestAtlassianRecoveryBasicAndOwnerProfile(t *testing.T) {
	for _, operation := range []string{"jira.issue.create", "jira.issue.comment.write", "confluence.page.create", "confluence.page.update"} {
		key := jiraServerDefinition
		if strings.HasPrefix(operation, "confluence.") {
			key = confluenceServerDefinition
		}
		for _, wrong := range []bool{false, true} {
			t.Run(operation+"/"+strconv.FormatBool(wrong), func(t *testing.T) {
				adapter := testAdapter(t)
				credential := testCredential(t, adapter, "synthetic-server-token")
				request := atlassianServerInvocation(t, adapter, key, operation, atlassianServerInput(operation), credential)
				request.Configuration["auth_scheme"], request.Configuration["username"] = "BASIC", "synthetic-user"
				if wrong {
					request.PersonalCredentialAdmission, request.ResourcePermissionProfile = true, "UNKNOWN"
				}
				calls := 0
				adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if wrong {
						t.Fatal("unknown personal profile reached provider")
					}
					return atlassianRecoveryFixture(t, request, r, "allow", calls), nil
				})}
				result, err := adapter.Recover(t.Context(), request)
				if wrong {
					if err == nil || calls != 0 || result.Receipt.ProviderEffectRef != "" {
						t.Fatal("wrong profile obtained recovery")
					}
					return
				}
				if err != nil || result.Receipt.ProviderEffectRef == "" {
					t.Fatal("explicit Basic recovery failed", err)
				}
			})
		}
	}
}
