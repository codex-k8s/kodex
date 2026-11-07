package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// personalCatalogHTTPFixture contains one own empty binding and one visible shared-mode
// Connection without a binding. It supplies no owner identities or Secret descriptors.
// personalCatalogHTTPFixture содержит одну собственную пустую привязку и один видимый Connection
// общего режима без привязки. Owner identities и Secret descriptors не передаются.
func personalCatalogHTTPFixture() *cp.ListPersonalIntegrationConnectionsResponse {
	return &cp.ListPersonalIntegrationConnectionsResponse{Page: &cp.PageInfo{NextPageToken: "page-fixture"},
		Connections: []*cp.PersonalIntegrationConnection{
			{ConnectionRef: "int_fixture01", ConnectionVersion: 7, Name: "First fixture", DefinitionKey: "gitlab",
				Policy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED, Credential: personalCredentialHTTPFixture()},
			{ConnectionRef: "int_second01", ConnectionVersion: 8, Name: "Second fixture", DefinitionKey: "jira",
				Policy: cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_PROJECT_SHARED, Enabled: true, Available: true},
		}}
}

// TestPersonalCatalogHTTPPreservesSelfProjectionAndPagination checks generated route/client
// dispatch, cursor/search pins, explicit false enablement and absent-binding semantics.
// TestPersonalCatalogHTTPPreservesSelfProjectionAndPagination проверяет generated route/client
// dispatch, cursor/search pins, явный false enablement и сохранение отсутствующей привязки.
func TestPersonalCatalogHTTPPreservesSelfProjectionAndPagination(t *testing.T) {
	t.Parallel()
	recorder := &catalogRPCRecorder{response: personalCatalogHTTPFixture()}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me/integration-connections?query=fixture&pageSize=2&pageToken=opaque", nil)
	writer := httptest.NewRecorder()
	personalCredentialHTTPHandler(recorder).ServeHTTP(writer, request)
	input, ok := recorder.request.(*cp.ListPersonalIntegrationConnectionsRequest)
	if writer.Code != http.StatusOK || !ok || recorder.method != cp.PersonalIntegrationCredentialService_ListPersonalIntegrationConnections_FullMethodName ||
		input.GetQuery() != "fixture" || input.GetPage().GetPageSize() != 2 || input.GetPage().GetPageToken() != "opaque" {
		t.Fatalf("personal catalog dispatch failed: %d", writer.Code)
	}
	var body struct {
		Items         []map[string]any `json:"items"`
		NextPageToken string           `json:"nextPageToken"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &body); err != nil || len(body.Items) != 2 || body.NextPageToken != "page-fixture" ||
		body.Items[0]["enabled"] != false || body.Items[1]["enabled"] != true || body.Items[0]["available"] != false || body.Items[1]["available"] != true || body.Items[0]["credential"] == nil {
		t.Fatal("personal catalog lost required safe fields or cursor", err)
	}
	if _, exists := body.Items[1]["credential"]; exists {
		t.Fatal("catalog fabricated an empty binding for an absent personal key")
	}
	for _, field := range []string{"subjectId", "organizationId", "secretRef", "secretUid", "contentSha256", `"value"`} {
		if strings.Contains(writer.Body.String(), field) {
			t.Fatal("personal catalog exposed internal credential coordinates")
		}
	}
	if writer.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("personal catalog permits caching self metadata")
	}
}

// TestPersonalCatalogHTTPRejectsAuthoritySelectorsAndBadMetadata proves malformed inputs fail
// before RPC and partial/mismatched owner replies fail closed rather than appearing as empty success.
// TestPersonalCatalogHTTPRejectsAuthoritySelectorsAndBadMetadata проверяет отказ неверным inputs
// до RPC и закрытый отказ частичным/несогласованным owner replies вместо успешного пустого ответа.
func TestPersonalCatalogHTTPRejectsAuthoritySelectorsAndBadMetadata(t *testing.T) {
	t.Parallel()
	const path = "/api/v1/me/integration-connections"
	for _, query := range []string{"?userRef=usr_other01", "?secretRef=private", "?query=one&query=two", "?pageSize=0", "?pageSize=101", "?pageToken=" + strings.Repeat("x", 513)} {
		recorder := &catalogRPCRecorder{}
		writer := httptest.NewRecorder()
		personalCredentialHTTPHandler(recorder).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, path+query, nil))
		if writer.Code != http.StatusBadRequest || recorder.request != nil {
			t.Fatal("catalog accepted an authority selector or unbounded input")
		}
	}
	for _, mutate := range []func(*cp.ListPersonalIntegrationConnectionsResponse){
		func(r *cp.ListPersonalIntegrationConnectionsResponse) { r.Page = nil },
		func(r *cp.ListPersonalIntegrationConnectionsResponse) {
			r.Connections[0].Policy = cp.IntegrationCredentialPolicy(999)
		},
		func(r *cp.ListPersonalIntegrationConnectionsResponse) {
			r.Connections[0].Credential.ConnectionVersion++
		},
		func(r *cp.ListPersonalIntegrationConnectionsResponse) {
			r.Connections[1].ConnectionRef = r.Connections[0].ConnectionRef
		},
		func(r *cp.ListPersonalIntegrationConnectionsResponse) {
			r.Page.NextPageToken = strings.Repeat("x", 513)
		},
	} {
		response := personalCatalogHTTPFixture()
		mutate(response)
		recorder := &catalogRPCRecorder{response: response}
		writer := httptest.NewRecorder()
		personalCredentialHTTPHandler(recorder).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, path, nil))
		if writer.Code != http.StatusBadGateway {
			t.Fatalf("catalog accepted malformed owner metadata: %d", writer.Code)
		}
	}
	response := &cp.ListPersonalIntegrationConnectionsResponse{Page: &cp.PageInfo{}}
	recorder := &catalogRPCRecorder{response: response}
	writer := httptest.NewRecorder()
	personalCredentialHTTPHandler(recorder).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, path, nil))
	var empty struct {
		Items []any `json:"items"`
	}
	if writer.Code != http.StatusOK || json.Unmarshal(writer.Body.Bytes(), &empty) != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatal("authoritative empty catalog did not preserve an explicit empty array")
	}
}
