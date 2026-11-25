package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// atlassianInstanceInvocation derives an instance scope digest from the published package and fixture configuration.
// atlassianInstanceInvocation рассчитывает дайджест инстанса из опубликованного пакета и тестовой конфигурации.
func atlassianInstanceInvocation(t *testing.T, adapter *Adapter, key, operation string, input map[string]any) Request {
	t.Helper()
	request := atlassianServerInvocation(t, adapter, key, operation, input, testCredential(t, adapter, "fixture-token"))
	configuration := map[string]string{
		"base_url": request.Configuration["base_url"].(string), "context_path": request.Configuration["context_path"].(string),
		"scope_kind": "INSTANCE", "auth_scheme": "BEARER",
	}
	request.Configuration = map[string]any{}
	for key, value := range configuration {
		request.Configuration[key] = value
	}
	capability, _ := adapter.definitions[key].Capability(operation)
	var err error
	request.ResourceScope, err = capability.ResourceScopeValues(configuration)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := json.Marshal(request.ResourceScope)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(scope)
	request.ResourceScopeDigest = hex.EncodeToString(digest[:])
	return request
}

// TestAtlassianInstanceReads keeps provider authorization and rejects selector drift beyond scoped package tests.
// TestAtlassianInstanceReads сохраняет авторизацию поставщика и отклоняет подмену селектора в режиме инстанса.
func TestAtlassianInstanceReads(t *testing.T) {
	t.Run("Jira across projects", func(t *testing.T) {
		adapter := testAdapter(t)
		request := atlassianInstanceInvocation(t, adapter, jiraServerDefinition, "jira.issue.search", map[string]any{"limit": 2})
		other := jiraServerIssueFixture()
		other["key"] = "OTHER-1"
		other["fields"].(map[string]any)["project"] = map[string]string{"key": "OTHER"}
		fixture := map[string]any{"startAt": 0, "maxResults": 2, "total": 2, "issues": []any{jiraServerIssueFixture(), other}}
		adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet || r.URL.Path != "/tracker/rest/api/2/search" || r.URL.Query().Get("jql") != "ORDER BY key ASC" ||
				r.Header.Get("Authorization") != "Bearer fixture-token" {
				t.Fatal("Jira instance read did not use the authorized native search")
			}
			body, _ := json.Marshal(fixture)
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
		})}
		result, err := adapter.Execute(t.Context(), request)
		if err != nil || !strings.Contains(result.Summary, "OTHER-1") {
			t.Fatalf("cross-project result did not contain both issues: %v, %v", result, err)
		}
		other["key"] = "DEMO-2"
		if _, err := adapter.Execute(t.Context(), request); err == nil {
			t.Fatal("Jira accepted mismatched issue and project keys")
		}
	})

	t.Run("Confluence across spaces", func(t *testing.T) {
		adapter := testAdapter(t)
		request := atlassianInstanceInvocation(t, adapter, confluenceServerDefinition, "confluence.page.search", map[string]any{"limit": 2})
		other := confluenceServerPageFixture("102", "Second", 1)
		other["space"] = map[string]string{"key": "OTHER"}
		fixture := map[string]any{"start": 0, "limit": 2, "size": 2, "results": []any{confluenceServerPageFixture("101", "First", 1), other}}
		adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet || r.URL.Path != "/docs/rest/api/content" || r.URL.Query().Has("spaceKey") ||
				r.Header.Get("Authorization") != "Bearer fixture-token" {
				t.Fatal("Confluence instance read did not use the authorized native search")
			}
			body, _ := json.Marshal(fixture)
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
		})}
		result, err := adapter.Execute(t.Context(), request)
		if err != nil || !strings.Contains(result.Summary, "OTHER") {
			t.Fatalf("cross-space result did not expose the actual space: %v, %v", result, err)
		}
	})
}

// TestJiraInstanceProjectCatalogProjectsBoundedWire allows native catalogs larger than one tool response before projecting a single project.
// TestJiraInstanceProjectCatalogProjectsBoundedWire допускает каталог больше одного ответа инструмента до проекции одного проекта.
func TestJiraInstanceProjectCatalogProjectsBoundedWire(t *testing.T) {
	adapter := testAdapter(t)
	request := atlassianInstanceInvocation(t, adapter, jiraServerDefinition, "jira.project.read", map[string]any{})
	raw, err := json.Marshal([]map[string]string{
		{"id": "101", "key": "DEMO", "name": "Visible project", "description": strings.Repeat("fixture ", 14000)},
		{"id": "102", "key": "OTHER", "name": "Second project"},
	})
	if err != nil || len(raw) <= maximumResponseBytes {
		t.Fatal("fixture did not exceed the standard wire budget")
	}
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/tracker/rest/api/2/project" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Fatal("unexpected project catalog request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}
	result, err := adapter.Execute(t.Context(), request)
	if err != nil || !strings.Contains(result.Summary, "DEMO") || strings.Contains(result.Summary, "OTHER") || len(result.Summary) > 256 {
		t.Fatalf("project catalog was not reduced to one bounded projection: %v", err)
	}
}
