package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestConfluencePersonalPageUpdatePermission checks bounded exact-page permission reads before
// one PUT, while a prior marked effect stays unknown until explicit author-checked Recover.
// TestConfluencePersonalPageUpdatePermission проверяет bounded права exact page перед одним PUT;
// прежний marked effect остаётся unknown до явного author-checked Recover.
func TestConfluencePersonalPageUpdatePermission(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		operations   any
		changeSecond func(map[string]any, Request)
		profile      string
		puts         int
		success      bool
	}{
		{"allowed", []any{map[string]string{"operation": "read", "targetType": "page"}, map[string]string{"operation": "update", "targetType": "page"}}, nil, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, 1, true},
		{"missing", nil, nil, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, 0, false},
		{"oversized", make([]any, 33), nil, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, 0, false},
		{"malformed", []any{map[string]any{"operation": true, "targetType": "page"}}, nil, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, 0, false},
		{"read only", []any{map[string]string{"operation": "read", "targetType": "page"}}, nil, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, 0, false},
		{"wrong target", []any{map[string]string{"operation": "update", "targetType": "space"}}, nil, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, 0, false},
		{"duplicate", []any{map[string]string{"operation": "update", "targetType": "page"}, map[string]string{"operation": "update", "targetType": "page"}}, nil, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, 0, false},
		{"wrong space", []any{map[string]string{"operation": "update", "targetType": "page"}}, func(p map[string]any, _ Request) { p["space"] = map[string]string{"key": "OTHER"} }, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, 0, false},
		{"version conflict", []any{map[string]string{"operation": "update", "targetType": "page"}}, func(p map[string]any, _ Request) { p["version"] = map[string]int{"number": 9} }, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, 0, false},
		{"second read loses permission", []any{map[string]string{"operation": "update", "targetType": "page"}}, func(p map[string]any, _ Request) { delete(p, "operations") }, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, 0, false},
		{"prior effect without update", nil, func(p map[string]any, r Request) {
			p["version"], p["title"] = map[string]int{"number": 3}, "Updated fixture"
			p["body"] = map[string]any{"storage": map[string]string{"representation": "storage", "value": "<p>new</p>\n" + confluenceServerMarker(r)}}
		}, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1, 0, false},
		{"missing profile", nil, nil, "", 0, false},
		{"unknown profile", nil, nil, "UNKNOWN", 0, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-server-token")
			request := atlassianServerInvocation(t, adapter, confluenceServerDefinition, "confluence.page.update", atlassianServerInput("confluence.page.update"), credential)
			request.PersonalCredentialAdmission, request.ResourcePermissionProfile = true, scenario.profile
			gets, puts := 0, 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodPut {
					puts++
					return atlassianServerResponse(t, request, r), nil
				}
				if r.Method != http.MethodGet || r.URL.Path != "/docs/rest/api/content/101" || !strings.Contains(r.URL.Query().Get("expand"), "operations") {
					t.Fatal("permission check changed its fixed route or omitted operations")
				}
				gets++
				page := confluenceServerPageFixture("101", "Fixture", 2)
				page["operations"] = scenario.operations
				if gets == 2 && scenario.changeSecond != nil {
					scenario.changeSecond(page, request)
				}
				body, err := json.Marshal(page)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if (err == nil) != scenario.success || puts != scenario.puts {
				t.Fatalf("wrong permission/effect outcome: puts=%d success=%v error=%v", puts, err == nil, err)
			}
			if scenario.name == "prior effect without update" {
				if !IsUnknownOutcome(err) || result.Receipt.ProviderEffectRef != "" {
					t.Fatal("marker-only update was adopted", err)
				}
			} else if !scenario.success && IsUnknownOutcome(err) {
				t.Fatal("pre-PUT object check invented an uncertain mutation", err)
			}
			if scenario.success && result.Receipt.ProviderEffectRef == "" {
				t.Fatal("successful observation lost its existing effect receipt")
			}
			if scenario.profile == "" || scenario.profile == "UNKNOWN" {
				if gets != 0 {
					t.Fatal("unsupported profile reached HTTP")
				}
			}
		})
	}
}

// TestConfluencePersonalStartedPutRetainsUnknown ensures a valid permission check does not
// erase uncertainty when the actual PUT response cannot be validated; no second PUT is sent.
// TestConfluencePersonalStartedPutRetainsUnknown проверяет, что успешная проверка прав не
// убирает неопределённость при невалидном ответе PUT; второй PUT не отправляется.
func TestConfluencePersonalStartedPutRetainsUnknown(t *testing.T) {
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "synthetic-server-token")
	request := atlassianServerInvocation(t, adapter, confluenceServerDefinition, "confluence.page.update", atlassianServerInput("confluence.page.update"), credential)
	request.PersonalCredentialAdmission, request.ResourcePermissionProfile = true, integrationpackage.CredentialResourcePermissionConfluencePageUpdateV1
	puts := 0
	adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPut {
			puts++
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":`)), Request: r}, nil
		}
		page := confluenceServerPageFixture("101", "Fixture", 2)
		page["operations"] = []any{map[string]string{"operation": "update", "targetType": "page"}}
		raw, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})}
	if _, err := adapter.Execute(t.Context(), request); !IsUnknownOutcome(err) || puts != 1 {
		t.Fatal("a started personal PUT lost uncertainty or was repeated", err)
	}
}
