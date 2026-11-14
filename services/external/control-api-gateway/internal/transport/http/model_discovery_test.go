package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// modelDiscoveryFixture provides safe synthetic provenance and explicit unknown execution evidence without asserting any upstream account result.
// modelDiscoveryFixture задаёт безопасные синтетические provenance и явное отсутствие execution evidence без утверждения результата upstream account.
func modelDiscoveryFixture() *cp.ListModelDiscoveryResponse {
	return &cp.ListModelDiscoveryResponse{Page: &cp.PageInfo{}, Total: 1, GroupBy: "PROVIDER", Source: &cp.ModelDiscoverySource{Schema: "kodex.model-provider-discovery/v1", Source: "models.dev", License: "MIT", SourceRevision: strings.Repeat("a", 40), UpstreamSchema: "models.dev/experimental-v2.0", SchemaSha256: strings.Repeat("b", 64), UpstreamSha256: strings.Repeat("c", 64), SnapshotSha256: strings.Repeat("d", 64), SourceUrl: "https://models.dev/_experimental_v2.0_api.json", ModelCount: 1, RetrievedAt: "2026-11-14T04:03:01Z", ProjectionSchema: "urn:kodex:contract:model-provider-discovery:v1", ProjectionSchemaSha256: strings.Repeat("e", 64), UpstreamSchemaValidation: "NOT_PERFORMED", OverlaySha256: strings.Repeat("e", 64), CatalogSha256: strings.Repeat("f", 64)}, Models: []*cp.ModelDiscoveryModel{{ProviderId: "synthetic", ProviderName: "Synthetic provider", ModelId: "same-model", Name: "Synthetic model", Type: "chat", Family: "UNKNOWN", Tools: "UNKNOWN", Reasoning: "UNKNOWN", Cache: "UNKNOWN", CacheImplicit: "UNKNOWN", CacheExplicit: "UNKNOWN", UpstreamStatus: "active", Known: "KNOWN", Adapter: "UNSUPPORTED", Account: "UNKNOWN", Allowed: "UNAVAILABLE", Tested: "NOT_RUN"}}}
}

// TestModelDiscoveryHTTPPreservesIndependentZeroValues proves exact RPC mapping, filter forwarding, required empty arrays/false values and decimal int64 representation.
// TestModelDiscoveryHTTPPreservesIndependentZeroValues доказывает точное RPC mapping, передачу фильтров, обязательные пустые arrays/false values и decimal int64 representation.
func TestModelDiscoveryHTTPPreservesIndependentZeroValues(t *testing.T) {
	client := &catalogRPCRecorder{response: modelDiscoveryFixture()}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/model-discovery?query=model&providerId=synthetic&pageSize=2", nil))
	if w.Code != 200 || client.method != cp.PlatformQueryService_ListModelDiscovery_FullMethodName {
		t.Fatalf("unexpected catalog route %d: %s", w.Code, w.Body.String())
	}
	request := client.request.(*cp.ListModelDiscoveryRequest)
	if request.Query != "model" || request.ProviderId != "synthetic" || request.Page.PageSize != 2 {
		t.Fatal("discovery filters lost")
	}
	var page map[string]any
	if json.Unmarshal(w.Body.Bytes(), &page) != nil {
		t.Fatal("invalid discovery JSON")
	}
	items := page["items"].([]any)
	model := items[0].(map[string]any)
	if page["nextPageToken"] != "" || page["total"] != "1" || model["contextTokens"] != "0" || model["routeConfigured"] != false || model["tools"] != "UNKNOWN" || len(model["reasoningEfforts"].([]any)) != 0 || model["allowed"] != "UNAVAILABLE" || model["tested"] != "NOT_RUN" {
		t.Fatal("discovery zero values or independent evidence lost")
	}
}

// TestModelDiscoveryHTTPRejectsInventedEvidence rejects fabricated PASS and invalid provenance before serialization, and bounds request filters before contacting the owner.
// TestModelDiscoveryHTTPRejectsInventedEvidence отклоняет выдуманный PASS и неверные provenance до сериализации и ограничивает request filters до обращения к владельцу.
func TestModelDiscoveryHTTPRejectsInventedEvidence(t *testing.T) {
	response := modelDiscoveryFixture()
	response.Models[0].Tested = "PASS"
	client := &catalogRPCRecorder{response: response}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/model-discovery", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatal("invented live evidence accepted")
	}
	client = &catalogRPCRecorder{response: modelDiscoveryFixture()}
	w = httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/model-discovery?providerId="+strings.Repeat("x", 129), nil))
	if w.Code != http.StatusBadRequest || client.method != "" {
		t.Fatal("oversize filter reached owner")
	}
}

// TestModelDiscoveryHTTPForwardsAllTypedFilters checks the complete public-to-owner selector mapping, including broad image MIME matching and family grouping.
// TestModelDiscoveryHTTPForwardsAllTypedFilters проверяет полное public-to-owner selector mapping, включая broad image MIME и family grouping.
func TestModelDiscoveryHTTPForwardsAllTypedFilters(t *testing.T) {
	response := modelDiscoveryFixture()
	response.GroupBy = "FAMILY"
	model := response.Models[0]
	model.Family = "glm"
	model.ContextTokens = 1000000
	model.ContextKnown = true
	model.Tools = "SUPPORTED"
	model.Reasoning = "SUPPORTED"
	model.Cache = "BOTH"
	model.CacheImplicit = "SUPPORTED"
	model.CacheExplicit = "SUPPORTED"
	model.InputModalities = []string{"image/png"}
	model.OutputModalities = []string{"text"}
	client := &catalogRPCRecorder{response: response}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/model-discovery?providerId=synthetic&family=glm&minimumContextTokens=1000000&inputModality=image&outputModality=text&tools=SUPPORTED&reasoning=SUPPORTED&cache=IMPLICIT&groupBy=FAMILY", nil))
	if w.Code != 200 {
		t.Fatalf("typed filters rejected: %d", w.Code)
	}
	request := client.request.(*cp.ListModelDiscoveryRequest)
	if request.Family != "glm" || request.MinimumContextTokens != 1000000 || request.InputModality != "image" || request.OutputModality != "text" || request.Tools != "SUPPORTED" || request.Reasoning != "SUPPORTED" || request.Cache != "IMPLICIT" || request.GroupBy != "FAMILY" {
		t.Fatal("typed selector lost at gateway")
	}
	for _, filter := range []string{"tools=", "reasoning=", "cache=", "groupBy=", "tools=false", "reasoning=PASS", "cache=OPTIMIZE", "groupBy=OTHER", "minimumContextTokens=-1", "minimumContextTokens=1000000000001"} {
		client = &catalogRPCRecorder{response: modelDiscoveryFixture()}
		w = httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/model-discovery?"+filter, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatal("invalid filter reached owner")
		}
	}
}
