package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
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

type executionSnapshotClient struct {
	cp.RuntimeWorkServiceClient
	input          runtimecontract.RunnerInput
	requests       []*cp.RecordRunToolCallRequest
	rejectRevision int64
}

func (client *executionSnapshotClient) RecordRunToolCall(_ context.Context, request *cp.RecordRunToolCallRequest, _ ...grpc.CallOption) (*cp.RecordRunToolCallResponse, error) {
	client.requests = append(client.requests, request)
	if request.LeaseRef != client.input.LeaseRef || request.Fence != client.input.LeaseFence || request.Generation != client.input.LeaseGeneration ||
		request.Tool != runtimecontract.ExecutionSnapshotTool || request.CapabilityRef != "" || request.GrantRef != "" || len(request.SafeParameters.GetFields()) != 0 {
		return nil, status.Error(codes.PermissionDenied, "PRIVATE_OWNER_SENTINEL")
	}
	if request.Revision == client.rejectRevision {
		return nil, status.Error(codes.FailedPrecondition, "PRIVATE_OWNER_SENTINEL")
	}
	return &cp.RecordRunToolCallResponse{Event: &cp.RunEvent{Ref: "event_fixture123"}}, nil
}

func TestExecutionSnapshotCheckedOwnReadAndClosedFailure(t *testing.T) {
	for _, scenario := range []string{"observed", "unknown", "foreign observation", "completed observation", "owner cancelled", "owner terminal race",
		"caller selector", "unknown alias", "stale turn", "wrong ticket"} {
		t.Run(scenario, func(t *testing.T) {
			manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t, func(value *runtimecontract.RunnerInput) {
				value.Mode = runtimecontract.RunnerModeTurn
				value.Instructions = "PRIVATE_INSTRUCTIONS_SENTINEL"
			})
			coordinator := NewCoordinator()
			coordinator.Register(input)
			if scenario != "unknown" {
				observed := input
				if scenario == "foreign observation" {
					observed.LeaseRef, observed.SessionRef = "lease_foreign123", "ses_foreign123"
					coordinator.Register(observed)
				}
				if coordinator.recordProviderProcess(observed, runtimecontract.BindProviderProcessObservation(observed, "0.160.0")) != nil {
					t.Fatal("observation fixture rejected")
				}
			}
			if scenario == "completed observation" {
				coordinator.Complete(input.LeaseRef)
			}
			client := &executionSnapshotClient{input: input}
			if scenario == "owner cancelled" {
				client.rejectRevision = 1
			}
			if scenario == "owner terminal race" {
				client.rejectRevision = 2
			}
			arguments := map[string]any{}
			if scenario == "caller selector" {
				arguments["session_ref"] = "ses_foreign123"
			}
			name := runtimecontract.ExecutionSnapshotTool
			if scenario == "unknown alias" {
				name = "get_run_metadata"
			}
			var logs bytes.Buffer
			server := &Server{manager: manager, coordinator: coordinator, config: Config{RequestTimeout: time.Second},
				control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "execution-own", "method": "tools/call",
				"params": map[string]any{"name": name, "arguments": arguments}})
			request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/mcp", bytes.NewReader(raw))
			request.Header.Set("Authorization", "Bearer "+ticket)
			bindTestExecutionHeaders(request, input, "mcp")
			if scenario == "stale turn" {
				request.Header.Set("X-Kodex-Turn-Ref", "trn_foreign123")
			}
			if scenario == "wrong ticket" {
				request.Header.Set("Authorization", "Bearer foreign")
			}
			recorder := httptest.NewRecorder()
			server.route(recorder, request)
			var wire struct {
				Result struct {
					IsError           bool           `json:"isError"`
					StructuredContent map[string]any `json:"structuredContent"`
				} `json:"result"`
				Error json.RawMessage `json:"error"`
			}
			success := slices.Contains([]string{"observed", "unknown", "foreign observation", "completed observation"}, scenario)
			_ = json.Unmarshal(recorder.Body.Bytes(), &wire)
			if success {
				value := wire.Result.StructuredContent
				if wire.Result.IsError || len(wire.Error) != 0 || len(value) != 13 || len(client.requests) != 2 ||
					value["session_ref"] != input.SessionRef || value["turn_ref"] != input.TurnRef || value["attempt"] != float64(input.Attempt) ||
					value["runtime_revision_ref"] != input.RuntimeRevisionRef || value["runtime_revision_digest"] != input.RuntimeRevisionDigest ||
					value["image_reference"] != input.ImageReference || value["image_manifest_digest"] != input.ImageManifestDigest || value["model"] != input.Model {
					t.Fatal("own exact snapshot or fresh two-phase owner gate lost")
				}
				process := value["provider_process"].(map[string]any)
				if scenario == "observed" {
					if process["status"] != "OBSERVED" || process["version"] != "0.160.0" || process["observation_source"] != "INITIALIZE_USER_AGENT" {
						t.Fatal("serving process proof absent")
					}
				} else if process["status"] != "UNKNOWN" || process["version"] != nil {
					t.Fatal("foreign/completed/missing observation became proof")
				}
				if client.requests[0].State != cp.RunToolCallState_RUN_TOOL_CALL_STATE_RUNNING ||
					client.requests[1].State != cp.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED ||
					client.requests[1].SafeResult != runtimecontract.ExecutionSnapshotTool+":completed" {
					t.Fatal("read audit lifecycle/result privacy changed")
				}
			} else {
				if strings.Contains(recorder.Body.String(), "0.160.0") || strings.Contains(recorder.Body.String(), input.ImageReference) {
					t.Fatal("rejected/stale/terminal request received execution evidence")
				}
				if scenario == "owner terminal race" && (len(client.requests) != 2 || !wire.Result.IsError) {
					t.Fatal("terminal owner rejection did not close already-computed snapshot")
				}
				if slices.Contains([]string{"caller selector", "unknown alias", "stale turn", "wrong ticket"}, scenario) && len(client.requests) != 0 {
					t.Fatal("invalid scope reached owner projection")
				}
			}
			for _, private := range []string{input.LeaseFence, input.InputDigest, input.ExecutionBindingDigest, input.MCPBindingDigest, ticket,
				"PRIVATE_INSTRUCTIONS_SENTINEL", "PRIVATE_OWNER_SENTINEL", "userAgent", "cliVersion", "clientInfo", "environment_values", "configuration"} {
				if private != "" && (strings.Contains(recorder.Body.String(), private) || strings.Contains(logs.String(), private)) {
					t.Fatal("own diagnostic read leaked private configuration/binding")
				}
			}
		})
	}
}

func TestExecutionSnapshotProducerSchemaMatchesConsumerRegistry(t *testing.T) {
	input := providerProcessCurrentInput()
	input.Mode, input.AssistantScope, input.AssistantProfileRef = runtimecontract.RunnerModeTurn, runtimecontract.AssistantScopeNone, ""
	names := []string{}
	for _, tool := range tools(input) {
		names = append(names, tool["name"].(string))
	}
	if !reflect.DeepEqual(names, runtimecontract.RuntimeMCPToolNames(input)) || !slices.Contains(names, runtimecontract.ExecutionSnapshotTool) ||
		slices.Contains(names, "get_configuration_catalog") {
		t.Fatal("ordinary own read producer/consumer mismatch or privileged catalog expansion")
	}
	schema := executionSnapshotTool()
	args := schema["inputSchema"].(map[string]any)
	result := schema["outputSchema"].(map[string]any)
	if args["additionalProperties"] != false || len(args["properties"].(map[string]any)) != 0 ||
		result["additionalProperties"] != false || len(result["required"].([]string)) != 13 || len(result["properties"].(map[string]any)) != 13 {
		t.Fatal("own read schema is not closed")
	}
}
