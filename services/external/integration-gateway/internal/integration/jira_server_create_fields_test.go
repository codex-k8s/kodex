package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// jiraCreateFieldsFixture is synthetic operator configuration; these IDs have no live provenance.
// jiraCreateFieldsFixture — синтетическая конфигурация; IDs не происходят из живой среды.
const jiraCreateFieldsFixture = `{"platform":{"id":"customfield_11001","type":"OPTION","options":{"test":"21001"}},"hours":{"id":"customfield_11002","type":"NUMBER"},"epic":{"id":"customfield_11003","type":"ISSUE_KEY"},"note":{"id":"customfield_11004","type":"STRING"},"flag":{"id":"customfield_11005","type":"BOOLEAN"}}`

// jiraCreateMetadataFixture describes a complete synthetic screen, including a non-editable type
// locator. Explicit zero/false and a project-local Epic Link must survive the actual HTTP request.
// jiraCreateMetadataFixture описывает полную синтетическую форму, включая неизменяемый тип.
// Явные zero/false и project-local Epic Link должны сохраниться в настоящем HTTP-запросе.
func jiraCreateMetadataFixture() []any {
	fields := []any{}
	for _, item := range []struct {
		id, kind string
		required bool
	}{{"project", "project", true}, {"issuetype", "issuetype", true}, {"summary", "string", true}, {"description", "string", false}, {"labels", "array", false}, {"reporter", "user", true}} {
		operations := []string{"set"}
		if item.id == "issuetype" {
			operations = []string{}
		}
		schema := map[string]string{"type": item.kind, "system": item.id}
		if item.id == "labels" {
			schema["items"] = "string"
		}
		fields = append(fields, map[string]any{"fieldId": item.id, "required": item.required, "schema": schema, "operations": operations})
	}
	for _, item := range []struct{ id, kind, custom string }{
		{"customfield_11001", "option", "com.atlassian.jira.plugin.system.customfieldtypes:select"},
		{"customfield_11002", "number", "com.atlassian.jira.plugin.system.customfieldtypes:float"},
		{"customfield_11003", "any", "com.pyxis.greenhopper.jira:gh-epic-link"},
		{"customfield_11004", "string", "synthetic:text"}, {"customfield_11005", "boolean", "synthetic:boolean"},
	} {
		field := map[string]any{"fieldId": item.id, "required": true, "schema": map[string]string{"type": item.kind, "custom": item.custom}, "operations": []string{"set"}}
		if item.kind == "option" {
			field["allowedValues"] = []any{map[string]string{"id": "21001"}}
		}
		fields = append(fields, field)
	}
	return fields
}

// jiraCreateFieldsInvocation uses real adapter admission and package/input pins with inert test
// credentials. Its configuration changes only a new connection-scoped non-secret allowlist.
// jiraCreateFieldsInvocation использует настоящий admission и package/input pins с test credential.
// Меняется только новый несекретный allowlist подключения.
func jiraCreateFieldsInvocation(t *testing.T, adapter *Adapter) Request {
	t.Helper()
	credential := testCredential(t, adapter, "synthetic-server-token")
	request := atlassianServerInvocation(t, adapter, jiraServerDefinition, "jira.issue.create", map[string]any{
		"summary": "Fixture", "description": "body", "fields_json": `{"platform":"test","hours":0,"epic":"DEMO-42","note":"synthetic","flag":false}`, "reporter_self": true,
	}, credential)
	request.Configuration["issue_create_fields"] = jiraCreateFieldsFixture
	return request
}

// TestJiraServerCreateFieldsHTTPBoundary executes the entire typed adapter against its OpenAPI
// subset, including two-page metadata, self identity and scope recheck; refusals never reach POST.
// TestJiraServerCreateFieldsHTTPBoundary исполняет typed adapter по OpenAPI, включая две страницы,
// self identity и scope recheck; после отказа POST недостижим.
func TestJiraServerCreateFieldsHTTPBoundary(t *testing.T) {
	for _, scenario := range []string{"offset", "start", "start no total", "renamed type", "localized type", "missing type", "ambiguous type", "subtask only", "missing required", "option denied", "wrong schema", "metadata duplicate", "total drift", "foreign epic", "inactive self", "permission denied", "401", "403", "404", "429", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			request := jiraCreateFieldsInvocation(t, adapter)
			if scenario == "localized type" {
				request.Configuration["issue_type"] = "Синтетический тип"
			}
			posts, reads := 0, 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				reads++
				if reads > 12 {
					t.Fatal("unbounded prerequisite reads")
				}
				path := strings.TrimPrefix(r.URL.Path, "/tracker/rest/api/2")
				var value any
				switch path {
				case "/search":
					value = map[string]any{"startAt": 0, "maxResults": 2, "total": 0, "issues": []any{}}
				case "/mypermissions":
					value = map[string]any{"permissions": map[string]any{"CREATE_ISSUES": map[string]any{"key": "CREATE_ISSUES", "type": "PROJECT", "havePermission": scenario != "permission denied"}}}
				case "/project/DEMO":
					types := []any{map[string]any{"id": "3", "name": request.Configuration["issue_type"], "subtask": scenario == "subtask only"}}
					if scenario == "missing type" {
						types[0].(map[string]any)["name"] = "Another type"
					}
					if scenario == "ambiguous type" {
						types = append(types, map[string]any{"id": "4", "name": request.Configuration["issue_type"], "subtask": false})
					}
					value = map[string]any{"id": "1", "key": "DEMO", "name": "Synthetic", "issueTypes": types}
				case "/issue/createmeta/DEMO/issuetypes/3":
					if scenario == "timeout" {
						return nil, context.DeadlineExceeded
					}
					if status, err := strconv.Atoi(scenario); err == nil {
						return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
					}
					fields := jiraCreateMetadataFixture()
					if scenario == "missing required" {
						fields = append(fields, map[string]any{"fieldId": "customfield_11999", "required": true, "schema": map[string]string{"type": "string"}, "operations": []string{"set"}})
					}
					if scenario == "option denied" {
						fields[6].(map[string]any)["allowedValues"] = []any{map[string]string{"id": "21999"}}
					}
					if scenario == "wrong schema" {
						fields[7].(map[string]any)["schema"] = map[string]string{"type": "array", "custom": "synthetic:array"}
					}
					offset, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
					total := len(fields)
					if offset == 0 {
						fields = fields[:6]
					} else {
						fields = fields[6:]
					}
					if scenario == "metadata duplicate" && offset > 0 {
						fields[0] = jiraCreateMetadataFixture()[0]
					}
					if scenario == "total drift" && offset > 0 {
						total++
					}
					value = map[string]any{"startAt": offset, "maxResults": 50, "total": total, "isLast": offset > 0, "values": fields}
					if scenario == "start" || scenario == "start no total" {
						value = map[string]any{"start": offset, "size": 50, "last": offset > 0, "total": total, "values": fields}
						if scenario == "start no total" {
							delete(value.(map[string]any), "total")
						}
					}
				case "/myself":
					value = map[string]any{"key": "synthetic-user", "name": "synthetic-login", "displayName": "Synthetic", "active": scenario != "inactive self"}
				case "/issue/DEMO-42":
					value = jiraServerIssueFixture()
					value.(map[string]any)["key"] = "DEMO-42"
					if scenario == "foreign epic" {
						value.(map[string]any)["fields"].(map[string]any)["project"] = map[string]string{"key": "OTHER"}
					}
				case "/issue":
					if r.Method != "POST" {
						t.Fatal("wrong create method")
					}
					posts++
					var body struct {
						Fields map[string]any `json:"fields"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil {
						t.Fatal("invalid create body")
					}
					f := body.Fields
					issueType := f["issuetype"].(map[string]any)
					if scenario == "renamed type" && issueType["name"] == "Task" {
						// После metadata сервер уже переименовал тип; его ID остался прежним.
						return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"errors":{"issuetype":"stale name"}}`)), Request: r}, nil
					}
					if len(issueType) != 1 || issueType["id"] != "3" {
						t.Fatal("POST lost the issue type ID validated by create metadata")
					}
					if f["customfield_11002"] != float64(0) || f["customfield_11005"] != false || f["customfield_11003"] != "DEMO-42" || f["customfield_11004"] != "synthetic" || f["customfield_11001"].(map[string]any)["id"] != "21001" || f["reporter"].(map[string]any)["name"] != "synthetic-login" {
						t.Fatal("approved explicit field values changed")
					}
					raw, _ := json.Marshal(body)
					r.Body = io.NopCloser(strings.NewReader(string(raw)))
					wire := validateAtlassianWireRequest(t, request, r)
					response := &http.Response{StatusCode: 201, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"1","key":"DEMO-1"}`)), Request: r}
					validateAtlassianWireResponse(t, wire, response)
					return response, nil
				default:
					t.Fatal("unexpected prerequisite route", path)
				}
				return serverFixtureJSONResponse(t, request, r, value), nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if scenario == "offset" || scenario == "start" || scenario == "start no total" || scenario == "renamed type" || scenario == "localized type" {
				if err != nil || posts != 1 || result.Receipt.ProviderEffectRef != "jira-issue:DEMO-1" {
					t.Fatal("opt-in create failed", err, posts)
				}
			} else if err == nil || posts != 0 {
				t.Fatal("refused prerequisite reached create", scenario, err, posts)
			}
		})
	}
}

// TestJiraServerCreateFieldsRecoveryRequiresExactIntent verifies search AND addressed evidence
// after an unknown effect. It never reads mutable create metadata, checks new write permissions,
// follows provider links or repeats POST; zero and zero-point-zero remain the same numeric intent.
// TestJiraServerCreateFieldsRecoveryRequiresExactIntent сверяет поиск И адресное evidence после
// unknown effect. Новые metadata/права и POST не запрашиваются; zero и zero-point-zero равнозначны.
func TestJiraServerCreateFieldsRecoveryRequiresExactIntent(t *testing.T) {
	for _, scenario := range []string{"exact", "changed detail", "missing detail", "changed option", "wrong reporter", "foreign author", "foreign project", "duplicate", "wrong type"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := testAdapter(t)
			request := jiraCreateFieldsInvocation(t, adapter)
			calls := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				response := atlassianRecoveryFixture(t, request, r, scenario, calls)
				path := strings.TrimPrefix(r.URL.Path, "/tracker/rest/api/2")
				if path != "/search" && path != "/issue/DEMO-1" {
					return response, nil
				}
				for _, field := range []string{"customfield_11001", "customfield_11002", "customfield_11003", "reporter"} {
					if !strings.Contains(r.URL.Query().Get("fields"), field) {
						t.Fatal("recovery omitted explicit intent evidence")
					}
				}
				var body map[string]any
				if json.NewDecoder(response.Body).Decode(&body) != nil {
					t.Fatal("invalid recovery fixture")
				}
				issues := []any{body}
				if path == "/search" {
					issues = body["issues"].([]any)
				}
				for _, entry := range issues {
					fields := entry.(map[string]any)["fields"].(map[string]any)
					fields["customfield_11001"] = map[string]string{"id": "21001", "value": "Synthetic"}
					fields["customfield_11002"] = json.Number("0.0")
					fields["customfield_11003"] = "DEMO-42"
					fields["customfield_11004"] = "synthetic"
					fields["customfield_11005"] = false
					fields["reporter"] = map[string]string{"key": "synthetic-user", "name": "synthetic-login"}
					if scenario == "changed detail" && path != "/search" {
						fields["customfield_11002"] = 1
					}
					if scenario == "missing detail" && path != "/search" {
						delete(fields, "customfield_11005")
					}
					if scenario == "changed option" {
						fields["customfield_11001"] = map[string]string{"id": "21999"}
					}
					if scenario == "wrong reporter" {
						fields["reporter"] = map[string]string{"key": "other-user"}
					}
				}
				raw, _ := json.Marshal(body)
				response.Body = io.NopCloser(strings.NewReader(string(raw)))
				validateAtlassianWireResponse(t, validateAtlassianWireRequest(t, request, r), response)
				return response, nil
			})}
			result, err := adapter.Recover(t.Context(), request)
			if scenario == "exact" {
				if err != nil || result.Receipt.ProviderEffectRef != "jira-issue:DEMO-1" {
					t.Fatal("exact recovery rejected", err)
				}
			} else if !IsUnknownOutcome(err) || result.Receipt.ProviderEffectRef != "" {
				t.Fatal("changed effect adopted", err)
			}
		})
	}
}
