package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func modelCapabilityFixture() *cp.ModelCapability {
	return &cp.ModelCapability{Id: "fixture-model", ProviderDefinitionKey: "openai-codex", ReasoningEfforts: []string{"low", "medium"}, DefaultReasoningEffort: "medium", Available: true, EligibleProviderAccountRefs: []string{"pacc_fixture01"}}
}

func TestModelCapabilitiesExactTypedRoute(t *testing.T) {
	client := &catalogRPCRecorder{response: &cp.ListModelCapabilitiesResponse{Models: []*cp.ModelCapability{modelCapabilityFixture()}, Total: 7, Page: &cp.PageInfo{NextPageToken: "opaque-next"}}}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/model-capabilities?providerDefinitionKey=openai-codex&providerAccountRef=pacc_fixture01&query=model&pageSize=2&pageToken=opaque-first", nil))
	if w.Code != 200 || client.method != cp.PlatformQueryService_ListModelCapabilities_FullMethodName {
		t.Fatalf("unexpected route status %d", w.Code)
	}
	r := client.request.(*cp.ListModelCapabilitiesRequest)
	if r.ProviderAccountRef != "pacc_fixture01" || r.ProviderDefinitionKey != "openai-codex" || r.Query != "model" || r.Page.PageSize != 2 || r.Page.PageToken != "opaque-first" {
		t.Fatal("model filters or cursor changed")
	}
	var result generated.ModelCapabilityPage
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Total != 7 || result.NextPageToken != "opaque-next" || len(result.Items) != 1 || result.Items[0].DefaultReasoningEffort != "medium" || result.Items[0].ReasoningEfforts[0] != "low" || !result.Items[0].Available || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("model capability readback changed")
	}
}

func TestModelCapabilitiesRejectMalformedInput(t *testing.T) {
	for _, query := range []string{"pageSize=0", "pageSize=101", "providerDefinitionKey=BAD", "providerAccountRef=prj_foreign01", "providerAccountRef=", "query=" + strings.Repeat("a", 201), "pageToken=" + strings.Repeat("x", 513)} {
		t.Run(query[:min(len(query), 32)], func(t *testing.T) {
			client := &catalogRPCRecorder{}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/model-capabilities?"+query, nil))
			if w.Code != 400 || client.method != "" {
				t.Fatal("invalid model filter reached CP")
			}
		})
	}
}

func TestModelCapabilitiesRejectCorruptOwnerResponse(t *testing.T) {
	for name, mutate := range map[string]func(*cp.ListModelCapabilitiesResponse){
		"default": func(r *cp.ListModelCapabilitiesResponse) { r.Models[0].DefaultReasoningEffort = "unknown" },
		"account": func(r *cp.ListModelCapabilitiesResponse) {
			r.Models[0].EligibleProviderAccountRefs = []string{"pacc_foreign01"}
		},
		"provider": func(r *cp.ListModelCapabilitiesResponse) { r.Models[0].ProviderDefinitionKey = "other" },
		"nil":      func(r *cp.ListModelCapabilitiesResponse) { r.Models[0] = nil },
		"total":    func(r *cp.ListModelCapabilitiesResponse) { r.Total = maximumSafeJSONInteger + 1 },
		"cursor": func(r *cp.ListModelCapabilitiesResponse) {
			r.Page = &cp.PageInfo{NextPageToken: strings.Repeat("x", 513)}
		},
		"duplicate": func(r *cp.ListModelCapabilitiesResponse) { r.Models = append(r.Models, r.Models[0]); r.Total = 2 },
		"readiness": func(r *cp.ListModelCapabilitiesResponse) { r.Models[0].ReadinessBlockers = []string{"BLOCKED"} },
		"efforts":   func(r *cp.ListModelCapabilitiesResponse) { r.Models[0].ReasoningEfforts = []string{"medium", "medium"} },
	} {
		t.Run(name, func(t *testing.T) {
			r := &cp.ListModelCapabilitiesResponse{Models: []*cp.ModelCapability{modelCapabilityFixture()}, Total: 1}
			mutate(r)
			client := &catalogRPCRecorder{response: r}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/model-capabilities?providerDefinitionKey=openai-codex&providerAccountRef=pacc_fixture01", nil))
			if w.Code != 502 {
				t.Fatalf("corrupt model accepted: %d", w.Code)
			}
		})
	}
}

func TestModelCapabilitiesDeniedAndEmpty(t *testing.T) {
	for code, want := range map[codes.Code]int{codes.Unauthenticated: 401, codes.PermissionDenied: 403, codes.NotFound: 404, codes.Unavailable: 503} {
		client := &catalogRPCRecorder{failure: status.Error(code, "private upstream detail")}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/model-capabilities", nil))
		if w.Code != want || strings.Contains(w.Body.String(), "private upstream detail") {
			t.Fatal("unsafe model error mapping")
		}
	}
	client := &catalogRPCRecorder{response: &cp.ListModelCapabilitiesResponse{}}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/model-capabilities", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal("empty model page lost shape")
	}
}

func TestProviderDefinitionsModelZeroValues(t *testing.T) {
	model := modelCapabilityFixture()
	model.Available, model.EligibleProviderAccountRefs = false, nil
	model.ReadinessBlockers = []string{"ELIGIBLE_PROVIDER_ACCOUNT_MISSING"}
	client := &catalogRPCRecorder{response: &cp.ListProviderDefinitionsResponse{Definitions: []*cp.ProviderDefinition{{Key: "openai-codex", Models: []*cp.ModelCapability{model}}}}}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/provider-definitions", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) || !strings.Contains(w.Body.String(), `"eligibleProviderAccountRefs":[]`) || !strings.Contains(w.Body.String(), `"models":[`) {
		t.Fatal("provider model readiness lost")
	}
}
