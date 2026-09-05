package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func effectiveCapabilitiesFixture() *cp.GetAgentEffectiveCapabilitiesResponse {
	return &cp.GetAgentEffectiveCapabilitiesResponse{AgentRef: "agt_fixture01", AgentVersion: 3, RuntimeConfigurationRef: "arc_fixture01", RuntimeConfigurationVersion: 2, EnvironmentVersionRef: "envver_fixture01", Digest: strings.Repeat("a", 64), EvaluatedAt: timestamppb.New(time.Now().UTC()), RuntimeReady: true, Total: 9, Page: &cp.PageInfo{NextPageToken: strings.Repeat("a", 700)},
		Capabilities: []*cp.EffectiveCapability{{Key: "platform.run.launch", Name: "Запуск", Source: "PLATFORM", Reason: "AVAILABLE", Requested: true, Effective: true, Grantable: true}, {Key: "synthetic.journal.write", Name: "Журнал", Source: "INTEGRATION", Reason: "ACTOR_PERMISSION_REQUIRED", Requested: true, ConnectionRef: "int_fixture01", GrantRef: "grant_fixture01", ConnectionVersion: 4, GrantVersion: 5, DefinitionDigest: strings.Repeat("b", 64)}}}
}

func TestEffectiveCapabilitiesHTTPPreservesOwnerProjection(t *testing.T) {
	response := effectiveCapabilitiesFixture()
	client := &catalogRPCRecorder{response: response}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/agents/agt_fixture01/effective-capabilities?query=run&pageSize=20&pageToken="+strings.Repeat("c", 700), nil))
	if w.Code != 200 || client.method != cp.PlatformQueryService_GetAgentEffectiveCapabilities_FullMethodName {
		t.Fatalf("capability route: %d", w.Code)
	}
	request := client.request.(*cp.GetAgentEffectiveCapabilitiesRequest)
	if request.AgentRef != response.AgentRef || request.Query != "run" || request.Page.PageSize != 20 || len(request.Page.PageToken) != 700 {
		t.Fatal("server query/pagination scope changed")
	}
	var result generated.AgentEffectiveCapabilityPage
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 2 || result.Total != 9 || result.Items[1].Effective || result.Items[1].Grantable || result.Items[1].Reason != "ACTOR_PERMISSION_REQUIRED" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("owner eligibility or zero values lost")
	}
	response.Capabilities, response.Total, response.Page, response.RuntimeReady = nil, 0, nil, false
	w = httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/agents/agt_fixture01/effective-capabilities", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":0`) || !strings.Contains(w.Body.String(), `"items":[]`) || !strings.Contains(w.Body.String(), `"runtimeReady":false`) {
		t.Fatal("empty authoritative projection lost required fields")
	}
}

func TestEffectiveCapabilitiesHTTPRejectsInvalidScopeAndUpstream(t *testing.T) {
	for _, suffix := range []string{"?workflowRef=wfl_fixture01", "?stepKey=step", "?workflowRef=bad!&stepKey=step", "?workflowRef=wfl_fixture01&stepKey=%00", "?pageSize=0", "?pageToken=" + strings.Repeat("a", 2049)} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/agents/agt_fixture01/effective-capabilities"+suffix, nil))
		if w.Code != 400 || client.method != "" {
			t.Fatalf("invalid scope reached owner: %d", w.Code)
		}
	}
	for name, mutate := range map[string]func(*cp.GetAgentEffectiveCapabilitiesResponse){
		"wrong agent":                     func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.AgentRef = "agt_foreign01" },
		"wrong workflow":                  func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.WorkflowRef = "wfl_foreign01" },
		"bad digest":                      func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.Digest = "private" },
		"bad timestamp":                   func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.EvaluatedAt = nil },
		"bad version":                     func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.AgentVersion = maximumSafeJSONInteger + 1 },
		"unknown source":                  func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.Capabilities[0].Source = "UNKNOWN" },
		"unknown reason":                  func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.Capabilities[0].Reason = "UNKNOWN" },
		"effective without request":       func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.Capabilities[0].Requested = false },
		"effective while runtime unready": func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.RuntimeReady = false },
		"missing exact grant":             func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.Capabilities[1].GrantRef = "" },
		"duplicate":                       func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.Capabilities[1] = r.Capabilities[0] },
		"total":                           func(r *cp.GetAgentEffectiveCapabilitiesResponse) { r.Total = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			response := effectiveCapabilitiesFixture()
			mutate(response)
			client := &catalogRPCRecorder{response: response}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/agents/agt_fixture01/effective-capabilities", nil))
			if w.Code != 502 || strings.Contains(w.Body.String(), "private") {
				t.Fatalf("corrupt capability response accepted: %d", w.Code)
			}
		})
	}
}

func TestEffectiveCapabilitiesHTTPPreservesOwnerErrors(t *testing.T) {
	for code, want := range map[codes.Code]int{codes.NotFound: 404, codes.PermissionDenied: 403, codes.InvalidArgument: 400, codes.Unavailable: 503} {
		client := &catalogRPCRecorder{failure: status.Error(code, "private owner detail")}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/agents/agt_fixture01/effective-capabilities", nil))
		if w.Code != want || strings.Contains(w.Body.String(), "private") {
			t.Fatalf("owner error mapping: %d", w.Code)
		}
	}
}

func TestEffectiveCapabilitiesHTTPPreservesWorkflowAndReadOnlyGrant(t *testing.T) {
	response := effectiveCapabilitiesFixture()
	response.WorkflowRef, response.WorkflowVersionRef, response.StepKey = "wfl_fixture01", "wver_fixture01", "review"
	response.Capabilities[0].Required = true
	response.Capabilities[1].Key, response.Capabilities[1].Required = "synthetic.journal.read", true
	response.Capabilities[1].Reason, response.Capabilities[1].Effective = "AVAILABLE", true
	client := &catalogRPCRecorder{response: response}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/agents/agt_fixture01/effective-capabilities?workflowRef=wfl_fixture01&stepKey=review", nil))
	if w.Code != 200 {
		t.Fatalf("valid workflow projection: %d", w.Code)
	}
	request := client.request.(*cp.GetAgentEffectiveCapabilitiesRequest)
	if request.WorkflowRef != response.WorkflowRef || request.StepKey != response.StepKey {
		t.Fatal("workflow stage scope lost")
	}
	var result generated.AgentEffectiveCapabilityPage
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.WorkflowVersionRef == nil || *result.WorkflowVersionRef != response.WorkflowVersionRef || !result.Items[1].Effective || result.Items[1].Grantable {
		t.Fatal("read permission was replaced with grant-management permission")
	}
}
