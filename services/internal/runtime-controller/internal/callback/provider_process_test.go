package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func providerProcessCurrentInput() runtimecontract.RunnerInput {
	input, _, _ := assistantOwnCurrentFixture(runtimecontract.AssistantScopeSystem)
	input.InputDigest, input.ExecutionBindingDigest = strings.Repeat("c", 64), strings.Repeat("d", 64)
	input.LeaseRef, input.LeaseGeneration, input.LeaseFence = "lease_current123", 1, "PRIVATE_FENCE_SENTINEL"
	return input
}

func TestProviderProcessCoordinatorExactLifecycleAndNoMutableAlias(t *testing.T) {
	input := providerProcessCurrentInput()
	value := runtimecontract.BindProviderProcessObservation(input, "0.160.0")
	coordinator := NewCoordinator()
	if coordinator.recordProviderProcess(input, value) == nil {
		t.Fatal("unregistered execution accepted")
	}
	coordinator.Register(input)
	if coordinator.providerProcessSnapshot(input)["status"] != "UNKNOWN" {
		t.Fatal("registration invented version")
	}
	for range 2 {
		if coordinator.recordProviderProcess(input, value) != nil {
			t.Fatal("same observation replay rejected")
		}
	}
	coordinator.Register(input)
	public := coordinator.providerProcessSnapshot(input)
	if public["status"] != "OBSERVED" || public["version"] != "0.160.0" || len(public) != 4 {
		t.Fatal("safe exact observation lost")
	}
	public["version"] = "PRIVATE_SENTINEL"
	if coordinator.providerProcessSnapshot(input)["version"] != "0.160.0" {
		t.Fatal("public map mutated observation")
	}
	conflicting := value
	conflicting.Version = "0.161.0"
	if coordinator.recordProviderProcess(input, conflicting) == nil {
		t.Fatal("different process version replaced first observation")
	}
	for name, mutate := range map[string]func(*runtimecontract.RunnerInput){
		"session": func(v *runtimecontract.RunnerInput) { v.SessionRef = "ses_other123" }, "turn": func(v *runtimecontract.RunnerInput) { v.TurnRef = "trn_other123" }, "attempt": func(v *runtimecontract.RunnerInput) { v.Attempt++ },
		"revision": func(v *runtimecontract.RunnerInput) { v.RuntimeRevisionVersion++ }, "image": func(v *runtimecontract.RunnerInput) {
			v.ImageReference = "pull.fixture.invalid/other@" + v.ImageManifestDigest
		},
		"generation": func(v *runtimecontract.RunnerInput) { v.LeaseGeneration++ }, "fence": func(v *runtimecontract.RunnerInput) { v.LeaseFence = "PRIVATE_OTHER_SENTINEL" }, "lease": func(v *runtimecontract.RunnerInput) { v.LeaseRef = "lease_other123" },
	} {
		t.Run(name, func(t *testing.T) {
			other := input
			mutate(&other)
			if coordinator.providerProcessSnapshot(other)["status"] != "UNKNOWN" || coordinator.recordProviderProcess(other, runtimecontract.BindProviderProcessObservation(other, "0.160.0")) == nil {
				t.Fatal("late/foreign observation crossed registered owner input")
			}
		})
	}
	other := input
	other.LeaseGeneration++
	other.ExecutionBindingDigest = strings.Repeat("e", 64)
	coordinator.Register(other)
	coordinator.Register(input)
	if coordinator.providerProcessSnapshot(other)["status"] != "UNKNOWN" || coordinator.recordProviderProcess(input, value) == nil {
		t.Fatal("new generation inherited/reaccepted old version")
	}
	if coordinator.recordProviderProcess(other, runtimecontract.BindProviderProcessObservation(other, "0.161.0")) != nil {
		t.Fatal("new exact generation rejected")
	}
	coordinator.Complete(other.LeaseRef)
	if coordinator.providerProcessSnapshot(other)["status"] != "UNKNOWN" || coordinator.recordProviderProcess(other, runtimecontract.BindProviderProcessObservation(other, "0.161.0")) == nil {
		t.Fatal("terminal execution retained/reaccepted observation")
	}
	restarted := NewCoordinator()
	restarted.Register(input)
	if restarted.providerProcessSnapshot(input)["status"] != "UNKNOWN" {
		t.Fatal("restart invented rejoin process proof")
	}
}

type providerProcessProgressClient struct {
	cp.RuntimeWorkServiceClient
	err      error
	requests []*cp.ReportExecutionProgressRequest
	before   func()
}

func (client *providerProcessProgressClient) ReportExecutionProgress(_ context.Context, request *cp.ReportExecutionProgressRequest, _ ...grpc.CallOption) (*cp.ReportExecutionProgressResponse, error) {
	client.before()
	client.requests = append(client.requests, request)
	return &cp.ReportExecutionProgressResponse{}, client.err
}

func TestProviderProcessProgressCheckedRouteRequiresOwnerAcceptance(t *testing.T) {
	for _, scenario := range []string{"exact", "owner unavailable", "owner terminal", "unauthorized", "stale header", "attempt", "image", "revision", "unknown field", "raw UA", "duplicate version", "conflicting version", "unregistered"} {
		t.Run(scenario, func(t *testing.T) {
			manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t)
			coordinator := NewCoordinator()
			if scenario != "unregistered" {
				coordinator.Register(input)
			}
			var logs bytes.Buffer
			client := &providerProcessProgressClient{before: func() {
				if coordinator.providerProcessSnapshot(input)["status"] != "UNKNOWN" && scenario != "conflicting version" {
					t.Error("observation published before owner acceptance")
				}
			}}
			payload := runtimecontract.RunnerProgressRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Progress: runtimecontract.ProviderProcessInitializedProgress}
			value := runtimecontract.BindProviderProcessObservation(input, "0.160.0")
			payload.ProviderProcess = &value
			wantStatus, ownerCalls := http.StatusNoContent, 1
			switch scenario {
			case "owner unavailable":
				client.err = status.Error(codes.Unavailable, "PRIVATE_OWNER_SENTINEL")
				wantStatus = http.StatusServiceUnavailable
			case "owner terminal":
				client.err = status.Error(codes.FailedPrecondition, "PRIVATE_OWNER_SENTINEL")
				wantStatus = http.StatusConflict
			case "unauthorized":
				ticket = strings.Repeat("0", 64)
				wantStatus, ownerCalls = http.StatusNotFound, 0
			case "stale header":
				wantStatus, ownerCalls = http.StatusNotFound, 0
			case "attempt":
				value.Attempt++
				wantStatus, ownerCalls = http.StatusBadRequest, 0
			case "image":
				value.ImageReference = "pull.fixture.invalid/other@" + value.ImageManifestDigest
				wantStatus, ownerCalls = http.StatusBadRequest, 0
			case "revision":
				value.RuntimeRevisionVersion++
				wantStatus, ownerCalls = http.StatusBadRequest, 0
			case "unknown field", "raw UA", "duplicate version":
				wantStatus, ownerCalls = http.StatusBadRequest, 0
			case "conflicting version":
				if coordinator.recordProviderProcess(input, value) != nil {
					t.Fatal("fixture rejected")
				}
				value.Version = "0.161.0"
				wantStatus = http.StatusConflict
			case "unregistered":
				wantStatus = http.StatusConflict
			}
			server := &Server{manager: manager, coordinator: coordinator, config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			raw, _ := json.Marshal(payload)
			if scenario == "unknown field" || scenario == "raw UA" {
				raw = bytes.Replace(raw, []byte(`"version":"0.160.0"`), []byte(`"version":"0.160.0","raw_user_agent":"PRIVATE_RAW_UA_SENTINEL"`), 1)
			}
			if scenario == "duplicate version" {
				raw = bytes.Replace(raw, []byte(`"version":"0.160.0"`), []byte(`"version":"0.160.0","version":"0.161.0"`), 1)
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/progress", bytes.NewReader(raw))
			request.Header.Set("Authorization", "Bearer "+ticket)
			bindTestExecutionHeaders(request, input, "progress")
			if scenario == "stale header" {
				request.Header.Set("X-Kodex-Turn-Ref", "trn_other123")
			}
			response := httptest.NewRecorder()
			server.route(response, request)
			if response.Code != wantStatus || len(client.requests) != ownerCalls {
				t.Fatalf("status=%d owner=%d", response.Code, len(client.requests))
			}
			if scenario == "exact" {
				if coordinator.providerProcessSnapshot(input)["version"] != "0.160.0" {
					t.Fatal("checked owner progress lost observation")
				}
				req := client.requests[0]
				if req.GetLeaseRef() != input.LeaseRef || req.GetFence() != input.LeaseFence || req.GetGeneration() != input.LeaseGeneration || req.GetProgress() != "i18n:MODEL_REQUEST_RUNNING" {
					t.Fatal("observation changed leased carrier")
				}
			} else if scenario != "conflicting version" && coordinator.providerProcessSnapshot(input)["status"] != "UNKNOWN" {
				t.Fatal("rejected callback supplied observed version")
			}
			for _, private := range []string{"PRIVATE_OWNER_SENTINEL", "PRIVATE_RAW_UA_SENTINEL", input.LeaseFence, ticket} {
				if private != "" && (strings.Contains(logs.String(), private) || strings.Contains(response.Body.String(), private)) {
					t.Fatal("private observation metadata leaked")
				}
			}
		})
	}
}

func TestProviderProcessOwnCurrentReadSeparatesActualVersionFromInventoryAndPrivateBinding(t *testing.T) {
	for _, observed := range []bool{false, true} {
		input, arguments, response := assistantOwnCurrentFixture(runtimecontract.AssistantScopeSystem)
		input.InputDigest, input.ExecutionBindingDigest = strings.Repeat("c", 64), strings.Repeat("d", 64)
		coordinator := NewCoordinator()
		coordinator.Register(input)
		if observed && coordinator.recordProviderProcess(input, runtimecontract.BindProviderProcessObservation(input, "0.160.0")) != nil {
			t.Fatal("fixture observation rejected")
		}
		client := &assistantFreshCatalogMCPClient{assistantDefinitionCatalogClient: &assistantDefinitionCatalogClient{response: response}}
		server := &Server{coordinator: coordinator, config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))}
		params, _ := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": arguments})
		recorder := httptest.NewRecorder()
		server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"process-current"`), Params: params}, input)
		var wire struct {
			Result struct {
				IsError           bool           `json:"isError"`
				StructuredContent map[string]any `json:"structuredContent"`
			} `json:"result"`
		}
		if json.Unmarshal(recorder.Body.Bytes(), &wire) != nil || wire.Result.IsError || client.request == nil {
			t.Fatal("fresh own configuration path failed")
		}
		catalog := wire.Result.StructuredContent["assistant_configuration_catalog"].(map[string]any)
		process := catalog["execution_snapshot"].(map[string]any)["provider_process"].(map[string]any)
		if observed {
			if process["status"] != "OBSERVED" || process["version"] != "0.160.0" || process["observation_source"] != "INITIALIZE_USER_AGENT" {
				t.Fatal("actual version omitted")
			}
		} else if process["status"] != "UNKNOWN" || process["version"] != nil {
			t.Fatal("inventory/current image invented process proof")
		}
		for _, private := range []string{input.LeaseFence, input.InputDigest, input.ExecutionBindingDigest, "userAgent", "raw_user_agent", "cliVersion", "clientInfo", "PRIVATE_CREDENTIAL_SENTINEL"} {
			if private != "" && strings.Contains(recorder.Body.String(), private) {
				t.Fatal("public current read leaked private process binding")
			}
		}
		if strings.Contains(client.projection.GetSafeResult(), "0.160.0") {
			t.Fatal("runtime observation persisted in tool result history")
		}
		// Существующая fresh owner eligibility не заменяется локальным observation.
		client.response = nil
		recorder = httptest.NewRecorder()
		server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"rejected-current"`), Params: params}, input)
		if strings.Contains(recorder.Body.String(), `"version":"0.160.0"`) {
			t.Fatal("owner failure leaked stale local observation")
		}
	}
}
