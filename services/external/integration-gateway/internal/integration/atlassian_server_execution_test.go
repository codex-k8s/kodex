package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// atlassianServerInput supplies only synthetic operation inputs for the complete declared Server/DC surface.
// atlassianServerInput задаёт только synthetic inputs для всей объявленной Server/DC поверхности.
func atlassianServerInput(operation string) map[string]any {
	switch operation {
	case "jira.issue.search":
		return map[string]any{"text": "fixture", "limit": 2}
	case "jira.issue.read", "jira.issue.transition.list":
		return map[string]any{"issue_key": "DEMO-1"}
	case "jira.issue.create":
		return map[string]any{"summary": "Fixture", "description": "body"}
	case "jira.issue.comment.write":
		return map[string]any{"issue_key": "DEMO-1", "body": "Comment"}
	case "jira.issue.transition.apply":
		return map[string]any{"issue_key": "DEMO-1", "transition_id": "11"}
	case "confluence.page.search":
		return map[string]any{"title": "Fixture", "limit": 2}
	case "confluence.page.read":
		return map[string]any{"page_id": "101"}
	case "confluence.page.create":
		return map[string]any{"title": "New fixture", "body": "<p>body</p>"}
	case "confluence.page.update":
		return map[string]any{"page_id": "101", "expected_version": 2, "title": "Updated fixture", "body": "<p>new</p>"}
	default:
		return map[string]any{}
	}
}

// atlassianServerInvocation reuses inert test lineage and derives new exact package/input/scope pins; it grants no live authority.
// atlassianServerInvocation переиспользует неактивную test lineage и выводит exact package/input/scope pins без live authority.
func atlassianServerInvocation(t *testing.T, adapter *Adapter, key, operation string, input map[string]any, credential *CredentialRevision) Request {
	t.Helper()
	definition := serverDefinitionFixture(t, key)
	definitions := make(map[string]integrationpackage.Package, len(adapter.definitions)+1)
	for name, value := range adapter.definitions {
		definitions[name] = value
	}
	definitions[key], adapter.definitions = definition, definitions
	request := invocationRequest(t, adapter.definitions["jira"], "jira.project.read", map[string]any{}, credential)
	capability, ok := definition.Capability(operation)
	if !ok {
		t.Fatal("missing synthetic Server capability")
	}
	configuration := map[string]string{"base_url": "https://jira.example.invalid", "auth_scheme": "BEARER", "context_path": "/tracker", "project_key": "DEMO", "issue_type": "Task"}
	if key == confluenceServerDefinition {
		configuration = map[string]string{"base_url": "https://confluence.example.invalid", "auth_scheme": "BEARER", "context_path": "/docs", "space_key": "DEMO"}
	}
	scope, err := capability.ResourceScopeValues(configuration)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(scope)
	scopeDigest := sha256.Sum256(raw)
	raw, _ = json.Marshal(input)
	canonical, err := capability.ValidateInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	inputDigest := sha256.Sum256(canonical)
	request.DefinitionPackage, _ = json.Marshal(definition)
	request.DefinitionKey, request.DefinitionVersion, request.DefinitionDigest = key, definition.Metadata.Version, definition.Digest
	request.CapabilityKey, request.Operation, request.Risk, request.ApprovalPolicy = capability.Key, capability.Operation, capability.Risk, capability.ApprovalPolicy
	request.ResourceKind, request.ResourceScope, request.ResourceScopeDigest = capability.ResourceScope.Kind, scope, hex.EncodeToString(scopeDigest[:])
	request.Input, request.InputDigest = input, hex.EncodeToString(inputDigest[:])
	request.Configuration = make(map[string]any)
	for name, value := range configuration {
		request.Configuration[name] = value
	}
	return request
}

// jiraServerIssueFixture is a synthetic v2 issue with explicit project eligibility and plaintext fields.
// jiraServerIssueFixture — synthetic v2 issue с явным проектом и plaintext fields.
func jiraServerIssueFixture() map[string]any {
	return map[string]any{"id": "1", "key": "DEMO-1", "fields": map[string]any{"project": map[string]string{"key": "DEMO"},
		"summary": "Fixture", "description": "body", "status": map[string]string{"name": "Open"}, "labels": []string{}}}
}

// confluenceServerPageFixture uses nested Server space/storage metadata without any Cloud spaceId.
// confluenceServerPageFixture содержит вложенные Server space/storage metadata без Cloud spaceId.
func confluenceServerPageFixture(id, title string, version int) map[string]any {
	return map[string]any{"id": id, "type": "page", "title": title, "status": "current", "space": map[string]string{"key": "DEMO"},
		"version": map[string]int{"number": version}, "ancestors": []any{},
		"body": map[string]any{"storage": map[string]string{"value": "<p>old</p>", "representation": "storage"}}}
}

// atlassianServerResponse validates actual wire routes and write payload shapes before returning synthetic versioned responses.
// atlassianServerResponse проверяет wire routes и write payloads перед synthetic versioned responses.
func atlassianServerResponse(t *testing.T, invocation Request, request *http.Request) *http.Response {
	t.Helper()
	if request.Header.Get("Authorization") != "Bearer synthetic-server-token" {
		t.Fatal("credential binding changed")
	}
	contractRequest := validateAtlassianWireRequest(t, invocation, request)
	var value any
	status := http.StatusOK
	if invocation.DefinitionKey == jiraServerDefinition {
		if request.URL.Host != "jira.example.invalid" || !strings.HasPrefix(request.URL.Path, "/tracker/rest/api/2/") {
			t.Fatalf("wrong Jira Server route: %s", request.URL)
		}
		path := strings.TrimPrefix(request.URL.Path, "/tracker/rest/api/2")
		switch {
		case request.Method == http.MethodGet && path == "/myself":
			value = map[string]string{"key": "synthetic-user", "name": "fixture", "displayName": "Synthetic User"}
		case request.Method == http.MethodGet && path == "/project/DEMO":
			value = map[string]string{"id": "1", "key": "DEMO", "name": "Fixture"}
		case request.Method == http.MethodGet && path == "/issue/DEMO-1":
			value = jiraServerIssueFixture()
		case request.Method == http.MethodGet && path == "/search":
			if request.URL.Query().Get("nextPageToken") != "" || request.URL.Query().Get("startAt") != "0" {
				t.Fatal("Cloud cursor reached Server request")
			}
			issues := []any{jiraServerIssueFixture()}
			if invocation.Operation == "jira.issue.create" {
				issues = []any{}
			}
			value = map[string]any{"startAt": 0, "maxResults": 2, "total": len(issues), "issues": issues}
		case request.Method == http.MethodPost && path == "/issue":
			var payload struct{ Fields map[string]any }
			if json.NewDecoder(request.Body).Decode(&payload) != nil || payload.Fields["description"] != "body" {
				t.Fatal("Server create did not use plaintext description")
			}
			value, status = map[string]string{"id": "2", "key": "DEMO-2"}, http.StatusCreated
		case request.Method == http.MethodPost && path == "/issue/DEMO-1/comment":
			var payload struct{ Body string }
			if json.NewDecoder(request.Body).Decode(&payload) != nil || !strings.HasPrefix(payload.Body, "Comment\n\n[kodex-effect:") {
				t.Fatal("Server comment body/effect changed")
			}
			value, status = map[string]string{"id": "3"}, http.StatusCreated
		case request.Method == http.MethodGet && path == "/issue/DEMO-1/transitions":
			value = map[string]any{"transitions": []any{map[string]any{"id": "11", "name": "Close", "fields": map[string]any{}}}}
		case request.Method == http.MethodPost && path == "/issue/DEMO-1/transitions":
			var payload struct{ Transition struct{ ID string } }
			if json.NewDecoder(request.Body).Decode(&payload) != nil || payload.Transition.ID != "11" {
				t.Fatal("transition ID changed")
			}
			status = http.StatusNoContent
		default:
			t.Fatalf("unsupported Jira Server call: %s %s", request.Method, path)
		}
	} else {
		if request.URL.Host != "confluence.example.invalid" || !strings.HasPrefix(request.URL.Path, "/docs/rest/api/") {
			t.Fatalf("wrong Confluence Server route: %s", request.URL)
		}
		path := strings.TrimPrefix(request.URL.Path, "/docs/rest/api")
		switch {
		case request.Method == http.MethodGet && path == "/user/current":
			value = map[string]string{"type": "known", "userKey": "synthetic-user", "username": "fixture", "displayName": "Synthetic User"}
		case request.Method == http.MethodGet && path == "/space/DEMO":
			value = map[string]any{"id": 42, "key": "DEMO", "name": "Fixture"}
		case request.Method == http.MethodGet && path == "/content/101":
			value = confluenceServerPageFixture("101", "Fixture", 2)
		case request.Method == http.MethodGet && path == "/content":
			if request.URL.Query().Get("spaceKey") != "DEMO" || request.URL.Query().Get("start") != "0" {
				t.Fatal("Server search lost space/offset")
			}
			pages := []any{confluenceServerPageFixture("101", "Fixture", 2)}
			if invocation.Operation == "confluence.page.create" {
				pages = []any{}
			}
			value = map[string]any{"start": 0, "limit": 2, "size": len(pages), "results": pages, "_links": map[string]any{}}
		case request.Method == http.MethodPost && path == "/content", request.Method == http.MethodPut && path == "/content/101":
			var payload confluenceServerPage
			if json.NewDecoder(request.Body).Decode(&payload) != nil || payload.Type != "page" || payload.Body.Storage.Representation != "storage" ||
				!strings.HasSuffix(payload.Body.Storage.Value, "\n"+confluenceServerMarker(invocation)) {
				t.Fatal("Server write did not use nested storage/effect marker")
			}
			if request.Method == http.MethodPost {
				if payload.Space.Key != "DEMO" {
					t.Fatal("create lost space")
				}
				value, status = confluenceServerPageFixture("102", "New fixture", 1), http.StatusCreated
			} else {
				if payload.Version.Number != 3 {
					t.Fatal("update lost next version")
				}
				value = confluenceServerPageFixture("101", "Updated fixture", 3)
			}
		default:
			t.Fatalf("unsupported Confluence Server call: %s %s", request.Method, path)
		}
	}
	raw, _ := json.Marshal(value)
	if status == http.StatusNoContent {
		raw = nil
	}
	response := &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: request}
	validateAtlassianWireResponse(t, contractRequest, response)
	return response
}

// testAtlassianServerCapabilities executes every advertised operation through normal package/scope/credential validation.
// testAtlassianServerCapabilities исполняет каждую объявленную операцию через штатные package/scope/credential проверки.
func testAtlassianServerCapabilities(t *testing.T, key string) {
	t.Helper()
	definition := serverDefinitionFixture(t, key)
	for _, capability := range definition.Spec.Capabilities {
		t.Run(capability.Key, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-server-token")
			request := atlassianServerInvocation(t, adapter, key, capability.Key, atlassianServerInput(capability.Key), credential)
			mutations := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					mutations++
				}
				return atlassianServerResponse(t, request, r), nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if err != nil || result.Receipt.ProviderEffectRef == "" {
				t.Fatalf("Server operation failed: %v", err)
			}
			if capability.Risk == "READ" && mutations != 0 || capability.Risk != "READ" && mutations != 1 {
				t.Fatalf("wrong effect count: %d", mutations)
			}
		})
	}
}

// TestAtlassianServerAllDeclaredOperations covers both new surfaces independently of generated-registry activation.
// TestAtlassianServerAllDeclaredOperations проверяет обе поверхности независимо от активации generated registry.
func TestAtlassianServerAllDeclaredOperations(t *testing.T) {
	for _, key := range []string{jiraServerDefinition, confluenceServerDefinition} {
		t.Run(key, func(t *testing.T) { testAtlassianServerCapabilities(t, key) })
	}
}
