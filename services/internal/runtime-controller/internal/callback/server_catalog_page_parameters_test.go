package callback

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"math"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

func catalogPageParametersFixture(t *testing.T, kind string) (runtimecontract.RunnerInput, map[string]any, *controlplanev1.AssistantConfigurationCatalogResponse) {
	t.Helper()
	if kind == "WORKFLOW_CONFIGURATION" {
		return assistantWorkflowConfigurationFixture(t)
	}
	return assistantAgentConfigurationFixture(t)
}

func TestCatalogPageSafeParametersPublishValidatedRequestCoordinates(t *testing.T) {
	for _, kind := range []string{"WORKFLOW_CONFIGURATION", "AGENT_CONFIGURATION"} {
		for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeProject, runtimecontract.AssistantScopeSystem} {
			for _, page := range []struct {
				name      string
				offset    any
				maximum   any
				wantStart int64
				wantLimit int64
			}{
				{"defaults", nil, nil, 0, 0},
				{"explicit first", float64(0), float64(maximumAssistantConfigurationPageBytes), 0, maximumAssistantConfigurationPageBytes},
				{"minimum page", int(0), int64(4), 0, 4},
				{"continuation", float64(123), float64(17), 123, 17},
				{"source bound", int64(maximumAssistantCurrentConfigurationBytes), int(maximumAssistantConfigurationPageBytes), maximumAssistantCurrentConfigurationBytes, maximumAssistantConfigurationPageBytes},
			} {
				t.Run(kind+"/"+string(scope)+"/"+page.name, func(t *testing.T) {
					input, arguments, _ := catalogPageParametersFixture(t, kind)
					input.AssistantScope = scope
					selector := arguments["assistant_configuration_catalog"].(map[string]any)
					if page.offset != nil {
						selector["configuration_offset_bytes"] = page.offset
					}
					if page.maximum != nil {
						selector["maximum_bytes"] = page.maximum
					}
					if page.wantStart > 0 {
						selector["configuration_sha256"] = strings.Repeat("a", 64)
					}
					wantMaximum := page.wantLimit
					if page.maximum == nil {
						// Default принадлежит canonical parser, а не второй копии политики проекции.
						defaults, err := parseAssistantConfigurationPage(selector, kind)
						if err != nil {
							t.Fatal(err)
						}
						wantMaximum = defaults.maximum
					}
					parameters, capability, grant, ok := safeToolCallParameters(input, "get_configuration_catalog", arguments)
					want := map[string]any{"catalogKind": kind, "offset_bytes": page.wantStart, "maximum_bytes": wantMaximum}
					if !ok || capability != "platform.configuration.read" || grant != "" || !reflect.DeepEqual(parameters, want) {
						t.Fatal("validated request coordinates lost the closed projection")
					}
					encoded, err := json.Marshal(parameters)
					if err != nil || strings.Contains(string(encoded), input.AssistantContext.EntityRef) || strings.Contains(string(encoded), input.AgentRef) || strings.Contains(string(encoded), strings.Repeat("a", 64)) {
						t.Fatal("safe page parameters exposed locators or commitments")
					}
				})
			}
		}
	}
}

func TestCatalogPageSafeParametersInvalidInputKeepsKindOnly(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*runtimecontract.RunnerInput, map[string]any, map[string]any)
	}{
		{"offset null", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["configuration_offset_bytes"] = nil }},
		{"maximum null", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["maximum_bytes"] = nil }},
		{"negative offset", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["configuration_offset_bytes"] = -1 }},
		{"fractional offset", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["configuration_offset_bytes"] = 0.5 }},
		{"offset NaN", func(_ *runtimecontract.RunnerInput, _, s map[string]any) {
			s["configuration_offset_bytes"] = math.NaN()
		}},
		{"offset infinity", func(_ *runtimecontract.RunnerInput, _, s map[string]any) {
			s["configuration_offset_bytes"] = math.Inf(1)
		}},
		{"offset above source", func(_ *runtimecontract.RunnerInput, _, s map[string]any) {
			s["configuration_offset_bytes"] = maximumAssistantCurrentConfigurationBytes + 1
		}},
		{"offset string", func(_ *runtimecontract.RunnerInput, _, s map[string]any) {
			s["configuration_offset_bytes"] = "PRIVATE_SENTINEL"
		}},
		{"offset bool", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["configuration_offset_bytes"] = true }},
		{"maximum below bound", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["maximum_bytes"] = 3 }},
		{"maximum above bound", func(_ *runtimecontract.RunnerInput, _, s map[string]any) {
			s["maximum_bytes"] = maximumAssistantConfigurationPageBytes + 1
		}},
		{"maximum fraction", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["maximum_bytes"] = 4.5 }},
		{"maximum infinity", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["maximum_bytes"] = math.Inf(-1) }},
		{"continuation missing digest", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["configuration_offset_bytes"] = 1 }},
		{"digest invalid", func(_ *runtimecontract.RunnerInput, _, s map[string]any) {
			s["configuration_sha256"] = "PRIVATE_SENTINEL"
		}},
		{"digest null", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["configuration_sha256"] = nil }},
		{"unknown selector", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["credentials"] = "PRIVATE_SENTINEL" }},
		{"unknown argument", func(_ *runtimecontract.RunnerInput, a, _ map[string]any) {
			a["headers"] = map[string]any{"Authorization": "PRIVATE_SENTINEL"}
		}},
		{"mixed selector", func(_ *runtimecontract.RunnerInput, a, _ map[string]any) { a["definition_query"] = "PRIVATE_SENTINEL" }},
		{"query wrong type", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["query"] = true }},
		{"foreign helper", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["assistant_ref"] = "agt_foreign123" }},
		{"foreign entity", func(_ *runtimecontract.RunnerInput, _, s map[string]any) { s["entity_ref"] = "res_foreign123" }},
		{"context unavailable", func(i *runtimecontract.RunnerInput, _, _ map[string]any) { i.AssistantContext = nil }},
		{"fence missing", func(i *runtimecontract.RunnerInput, _, _ map[string]any) { i.LeaseFence = "" }},
	}
	for _, kind := range []string{"WORKFLOW_CONFIGURATION", "AGENT_CONFIGURATION"} {
		for _, mutation := range mutations {
			t.Run(kind+"/"+mutation.name, func(t *testing.T) {
				input, arguments, _ := catalogPageParametersFixture(t, kind)
				selector := arguments["assistant_configuration_catalog"].(map[string]any)
				mutation.mutate(&input, arguments, selector)
				parameters, capability, grant, ok := safeToolCallParameters(input, "get_configuration_catalog", arguments)
				if !ok || capability != "platform.configuration.read" || grant != "" || !reflect.DeepEqual(parameters, map[string]any{"catalogKind": kind}) {
					t.Fatal("invalid request gained numeric coordinates or changed the prior failed activity path")
				}
			})
		}
	}
}

func TestCatalogPageSafeParametersDoNotExpandOtherKinds(t *testing.T) {
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeSystem)
	for _, kind := range []string{"MODELS", "CURRENT_CONFIGURATION", "ROLE_ENVIRONMENTS", "UNKNOWN_PRIVATE_SENTINEL"} {
		arguments := map[string]any{"assistant_configuration_catalog": map[string]any{
			"kind": kind, "configuration_offset_bytes": 10, "maximum_bytes": 4,
			"configuration_sha256": strings.Repeat("a", 64), "content": "PRIVATE_SENTINEL",
		}}
		parameters, _, _, ok := safeToolCallParameters(input, "get_configuration_catalog", arguments)
		want := map[string]any{}
		if assistantConfigurationCatalogKindKnown(kind) {
			want["catalogKind"] = kind
		}
		if !ok || !reflect.DeepEqual(parameters, want) {
			t.Fatal("non-page or unknown catalog gained request coordinates")
		}
	}
}

type catalogPageActivityClient struct {
	*assistantDefinitionCatalogClient
	activities []*controlplanev1.RecordRunToolCallRequest
}

func (client *catalogPageActivityClient) RecordRunToolCall(_ context.Context, request *controlplanev1.RecordRunToolCallRequest, _ ...grpc.CallOption) (*controlplanev1.RecordRunToolCallResponse, error) {
	client.activities = append(client.activities, proto.Clone(request).(*controlplanev1.RecordRunToolCallRequest))
	return &controlplanev1.RecordRunToolCallResponse{Event: &controlplanev1.RunEvent{Ref: "event_synthetic123"}}, nil
}

func TestCatalogPageSafeParametersTraverseNativeActivityWithoutChangingFailure(t *testing.T) {
	for _, kind := range []string{"WORKFLOW_CONFIGURATION", "AGENT_CONFIGURATION"} {
		for _, invalid := range []bool{false, true} {
			t.Run(kind+"/"+map[bool]string{false: "valid", true: "invalid"}[invalid], func(t *testing.T) {
				input, arguments, response := catalogPageParametersFixture(t, kind)
				arguments = maps.Clone(arguments)
				selector := maps.Clone(arguments["assistant_configuration_catalog"].(map[string]any))
				arguments["assistant_configuration_catalog"] = selector
				selector["maximum_bytes"] = 17.0
				if invalid {
					selector["maximum_bytes"] = nil
				}
				client := &catalogPageActivityClient{assistantDefinitionCatalogClient: &assistantDefinitionCatalogClient{response: &controlplanev1.SearchAssistantResourcesResponse{AssistantConfigurationCatalog: response}}}
				server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
				params, err := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": arguments})
				if err != nil {
					t.Fatal(err)
				}
				recorder := httptest.NewRecorder()
				server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"safe-page-parameters"`), Params: params}, input)
				var wire struct {
					Result struct {
						IsError           bool           `json:"isError"`
						StructuredContent map[string]any `json:"structuredContent"`
					} `json:"result"`
				}
				if json.Unmarshal(recorder.Body.Bytes(), &wire) != nil || wire.Result.IsError != invalid || len(client.activities) != 2 {
					t.Fatal("native page activity changed success or invalid-input delivery")
				}
				want := map[string]any{"catalogKind": kind}
				terminal := controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED
				if !invalid {
					want["offset_bytes"], want["maximum_bytes"] = float64(0), float64(17)
					terminal = controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED
				} else if wire.Result.StructuredContent["error_code"] != assistantCatalogInputInvalidCode || wire.Result.StructuredContent["retryable"] != true || wire.Result.StructuredContent["guidance"] != assistantCatalogReadInputGuidance {
					t.Fatal("invalid page lost the prior closed corrective guidance")
				}
				for index, activity := range client.activities {
					state := controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_RUNNING
					if index == 1 {
						state = terminal
					}
					if activity.GetState() != state || !reflect.DeepEqual(activity.GetSafeParameters().AsMap(), want) || activity.GetLeaseRef() != input.LeaseRef || activity.GetFence() != input.LeaseFence || activity.GetGeneration() != input.LeaseGeneration || activity.GetRevision() != int64(index+1) {
						t.Fatal("native activity lost exact lease, state, revision or safe request parameters")
					}
				}
			})
		}
	}
}
