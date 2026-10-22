package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

// workflowReceiptClient adds recording of local activity writes to the catalog fake,
// allowing tests to observe attempts without claiming durable persistence.
// workflowReceiptClient добавляет запись local activity write attempts к catalog fake
// для проверки вызовов без утверждения durable persistence.
type workflowReceiptClient struct {
	*workflowCatalogClient
	activities []*cp.RecordRunToolCallRequest
}

// RecordRunToolCall appends the original activity pointer and returns a synthetic
// event ref; it enforces no real lease claim or transactional audit boundary.
// RecordRunToolCall добавляет исходный activity pointer и возвращает synthetic event
// ref, не обеспечивая реальный lease claim и transactional audit boundary.
func (client *workflowReceiptClient) RecordRunToolCall(_ context.Context, request *cp.RecordRunToolCallRequest, _ ...grpc.CallOption) (*cp.RecordRunToolCallResponse, error) {
	client.activities = append(client.activities, request)
	return &cp.RecordRunToolCallResponse{Event: &cp.RunEvent{Ref: "evt_receipt001"}}, nil
}

// actualWorkflowReceipt obtains a genuine local handler-produced typed receipt
// for one of three synthetic owner response modes, with complete execution pins and
// a discard logger. It does not fabricate evidence by unmarshaling public JSON.
// actualWorkflowReceipt получает настоящий local handler-produced typed receipt
// одного из трёх synthetic owner response modes с execution pins/discard logger,
// не создавая evidence через unmarshal public JSON.
func actualWorkflowReceipt(t *testing.T, mode string) (*Server, runtimecontract.RunnerInput, *workflowReceiptClient, map[string]any, workflowCatalogToolResult) {
	t.Helper()
	server, input, owner, _, _ := publicationReadFixture(t)
	input.RunRef, input.SessionRef, input.TurnRef, input.NodeRef, input.Attempt = "run_fixture001", "ses_fixture001", "trn_fixture001", "nod_fixture001", 1
	input.RuntimeRevisionRef, input.RuntimeRevisionVersion, input.RuntimeRevisionDigest = "rrev_fixture001", 1, strings.Repeat("e", 64)
	args := map[string]any{"publication_read": publicationPins()}
	switch mode {
	case workflowModeDiscovery:
		owner.response = workflowCatalogFixture()
		args = map[string]any{"query": "PRIVATE_QUERY_CANARY", "page_token": "PRIVATE_CURSOR_CANARY"}
	case workflowModeActiveRuns:
		owner.response = activeRunResponse()
		args = map[string]any{"active_runs_read": publicationPins()}
	}
	client := &workflowReceiptClient{workflowCatalogClient: owner}
	server.control = &controlplaneclient.Client{Runtime: client}
	server.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	result, err := server.workflowCatalog(t.Context(), input, args)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := result.(workflowCatalogToolResult)
	if !ok || value.evidence == nil {
		t.Fatal("validated owner reply did not seal proof")
	}
	return server, input, client, args, value
}

// TestWorkflowCatalogReceiptPreservesWireAndClosedMetadata checks public JSON remains
// exactly the wire map while bounded versioned safe receipts exclude refs/text/tokens,
// preserve actual page digests and list/advisory metadata, and reject unbound fallback.
// TestWorkflowCatalogReceiptPreservesWireAndClosedMetadata проверяет неизменный public
// JSON wire map и bounded versioned safe receipts без refs/text/tokens,с actual page
// digests/list/advisory metadata и отказом unbound fallback.
func TestWorkflowCatalogReceiptPreservesWireAndClosedMetadata(t *testing.T) {
	for _, mode := range []string{workflowModeDiscovery, workflowModePublication, workflowModeActiveRuns} {
		t.Run(mode, func(t *testing.T) {
			_, input, _, args, value := actualWorkflowReceipt(t, mode)
			got, err := json.Marshal(value)
			want, _ := json.Marshal(value.wire)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatal("public MCP wire changed")
			}
			safe, err := safeWorkflowCatalogReceipt(input, args, value)
			if err != nil || len(safe) > maximumWorkflowReceiptBytes {
				t.Fatal("receipt unavailable")
			}
			var receipt workflowCatalogReceipt
			if json.Unmarshal([]byte(safe), &receipt) != nil || receipt.Version != 1 || receipt.Kind != workflowReceiptKind || receipt.Mode != mode {
				t.Fatal("closed version/mode lost")
			}
			for _, sentinel := range []string{"PRIVATE_", "wfl_", "wfv_", "prj_", "run_", "lse_", "text", "query", "token", "fixture-fence", "Работа"} {
				if strings.Contains(safe, sentinel) {
					t.Fatal("receipt exposed payload, locator or credential")
				}
			}
			if mode == workflowModePublication {
				page := value.wire["configuration_page"].(map[string]any)
				if receipt.Publication.NextOffsetBytes != page["next_offset_bytes"] || receipt.Publication.ConfigurationSHA256 != value.wire["configuration_sha256"] || receipt.Publication.PageSHA256 != workflowReceiptDigest([]byte(page["text"].(string))) {
					t.Fatal("page proof lost actual bytes")
				}
			} else if receipt.List.ItemsCount != 1 || receipt.List.Advisory != (mode == workflowModeActiveRuns) {
				t.Fatal("list cardinality or advisory lost")
			}
			if safeToolCallResult("get_workflow_catalog", value, nil) != "TOOL_UNAVAILABLE" {
				t.Fatal("unbound successful fallback remains")
			}
		})
	}
}

// TestWorkflowCatalogReceiptRejectsForgedResultBeforeTerminalWrite checks nil, maps,
// JSON reconstructions and evidence-free typed values cannot yield success history
// or trigger a terminal owner activity write.
// TestWorkflowCatalogReceiptRejectsForgedResultBeforeTerminalWrite проверяет запрет success
// history/terminal owner activity write для nil,maps,JSON reconstructions
// и typed values без evidence.
func TestWorkflowCatalogReceiptRejectsForgedResultBeforeTerminalWrite(t *testing.T) {
	server, input, client, args, value := actualWorkflowReceipt(t, workflowModePublication)
	wire, _ := json.Marshal(value)
	var decoded map[string]any
	var reconstructed workflowCatalogToolResult
	if json.Unmarshal(wire, &decoded) != nil || json.Unmarshal(wire, &reconstructed) != nil {
		t.Fatal("fixture decode")
	}
	for _, result := range []any{nil, value.wire, decoded, reconstructed, workflowCatalogToolResult{wire: value.wire}, string(wire)} {
		if err := server.recordToolCall(t.Context(), input, "get_workflow_catalog", args, result, nil, json.RawMessage(`"forged"`), time.Millisecond); err == nil {
			t.Fatal("forged JSON acquired proof")
		}
	}
	if len(client.activities) != 0 {
		t.Fatal("invalid proof wrote terminal activity")
	}
}

// TestWorkflowCatalogReceiptBindingAndMalformedClosedFailures mutates execution/request/
// wire/receipt commitments and eligibility, checking every mismatch fails before
// terminal activity. Changed discovery query and canceled reads cannot reuse evidence.
// TestWorkflowCatalogReceiptBindingAndMalformedClosedFailures меняет execution/request/
// wire/receipt commitments и eligibility,проверяя отказ до terminal activity.
// Изменённый discovery query и canceled reads не переиспользуют evidence.
func TestWorkflowCatalogReceiptBindingAndMalformedClosedFailures(t *testing.T) {
	mutations := map[string]func(*runtimecontract.RunnerInput, map[string]any, *workflowCatalogToolResult){
		"lease": func(i *runtimecontract.RunnerInput, _ map[string]any, _ *workflowCatalogToolResult) {
			i.LeaseRef += "foreign"
		},
		"fence": func(i *runtimecontract.RunnerInput, _ map[string]any, _ *workflowCatalogToolResult) {
			i.LeaseFence += "foreign"
		},
		"generation": func(i *runtimecontract.RunnerInput, _ map[string]any, _ *workflowCatalogToolResult) {
			i.LeaseGeneration++
		},
		"session": func(i *runtimecontract.RunnerInput, _ map[string]any, _ *workflowCatalogToolResult) {
			i.SessionRef += "foreign"
		},
		"turn": func(i *runtimecontract.RunnerInput, _ map[string]any, _ *workflowCatalogToolResult) {
			i.TurnRef += "foreign"
		},
		"attempt": func(i *runtimecontract.RunnerInput, _ map[string]any, _ *workflowCatalogToolResult) { i.Attempt++ },
		"revision": func(i *runtimecontract.RunnerInput, _ map[string]any, _ *workflowCatalogToolResult) {
			i.RuntimeRevisionDigest = strings.Repeat("f", 64)
		},
		"project": func(i *runtimecontract.RunnerInput, _ map[string]any, _ *workflowCatalogToolResult) {
			i.ProjectRef += "foreign"
		},
		"capability": func(i *runtimecontract.RunnerInput, _ map[string]any, _ *workflowCatalogToolResult) {
			i.Capabilities = nil
		},
		"request offset": func(_ *runtimecontract.RunnerInput, a map[string]any, _ *workflowCatalogToolResult) {
			a["publication_read"].(map[string]any)["offset_bytes"] = 1.5
		},
		"request maximum": func(_ *runtimecontract.RunnerInput, a map[string]any, _ *workflowCatalogToolResult) {
			a["publication_read"].(map[string]any)["maximum_bytes"] = 4
		},
		"request pins": func(_ *runtimecontract.RunnerInput, a map[string]any, _ *workflowCatalogToolResult) {
			a["publication_read"].(map[string]any)["workflow_ref"] = "wfl_foreign001"
		},
		"request unknown": func(_ *runtimecontract.RunnerInput, a map[string]any, _ *workflowCatalogToolResult) {
			a["headers"] = "PRIVATE_HEADER_CANARY"
		},
		"unknown version": func(_ *runtimecontract.RunnerInput, _ map[string]any, v *workflowCatalogToolResult) {
			v.evidence.receipt.Version++
		},
		"unknown mode": func(_ *runtimecontract.RunnerInput, _ map[string]any, v *workflowCatalogToolResult) {
			v.evidence.receipt.Mode = "PRIVATE_MODE_CANARY"
		},
		"invalid EOF": func(_ *runtimecontract.RunnerInput, _ map[string]any, v *workflowCatalogToolResult) {
			v.evidence.receipt.Publication.EOF = true
		},
		"bad digest": func(_ *runtimecontract.RunnerInput, _ map[string]any, v *workflowCatalogToolResult) {
			v.evidence.receipt.Publication.PageSHA256 = "PRIVATE_DIGEST_CANARY"
		},
		"metadata drift": func(_ *runtimecontract.RunnerInput, _ map[string]any, v *workflowCatalogToolResult) {
			v.evidence.receipt.Publication.SizeBytes++
		},
		"wire drift": func(_ *runtimecontract.RunnerInput, _ map[string]any, v *workflowCatalogToolResult) {
			v.wire["configuration_page"].(map[string]any)["text"] = "PRIVATE_TEXT_CANARY"
		},
		"wire unknown": func(_ *runtimecontract.RunnerInput, _ map[string]any, v *workflowCatalogToolResult) {
			v.wire["credential"] = "PRIVATE_SECRET_CANARY"
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			server, input, client, args, value := actualWorkflowReceipt(t, workflowModePublication)
			mutate(&input, args, &value)
			if err := server.recordToolCall(t.Context(), input, "get_workflow_catalog", args, value, nil, json.RawMessage(`"invalid"`), time.Millisecond); err == nil || len(client.activities) != 0 {
				t.Fatal("bad proof wrote success")
			}
		})
	}
	server, input, _, args, value := actualWorkflowReceipt(t, workflowModeDiscovery)
	args["query"] = "another"
	if _, err := safeWorkflowCatalogReceipt(input, args, value); err == nil {
		t.Fatal("changed discovery query reused proof")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := server.workflowCatalog(ctx, input, args); err == nil {
		t.Fatal("cancelled read produced proof")
	}
}

// TestWorkflowCatalogReceiptNativePhasesAndUTF8EOF checks local MCP RUNNING/SUCCEEDED
// activity revisions, lease binding and safe parameters for all modes, traversing
// publication pages with exact page commitments through true EOF.
// TestWorkflowCatalogReceiptNativePhasesAndUTF8EOF проверяет local MCP RUNNING/SUCCEEDED
// activity revisions,lease binding/safe parameters всех modes и publication pages
// с exact page commitments до истинного EOF.
func TestWorkflowCatalogReceiptNativePhasesAndUTF8EOF(t *testing.T) {
	for _, mode := range []string{workflowModeDiscovery, workflowModePublication, workflowModeActiveRuns} {
		server, input, client, args, _ := actualWorkflowReceipt(t, mode)
		var next int64
		for {
			client.activities = nil
			params, _ := json.Marshal(map[string]any{"name": "get_workflow_catalog", "arguments": args})
			recorder := httptest.NewRecorder()
			server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"page"`), Params: params}, input)
			var reply struct {
				Result struct {
					IsError           bool           `json:"isError"`
					StructuredContent map[string]any `json:"structuredContent"`
				} `json:"result"`
			}
			if json.Unmarshal(recorder.Body.Bytes(), &reply) != nil || reply.Result.IsError || len(client.activities) != 2 {
				t.Fatal("native result/activity unavailable")
			}
			for index, activity := range client.activities {
				want := cp.RunToolCallState_RUN_TOOL_CALL_STATE_RUNNING
				if index == 1 {
					want = cp.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED
				}
				expected := workflowCatalogSafeParameters(args)
				encoded, _ := json.Marshal(expected)
				var normalized map[string]any
				_ = json.Unmarshal(encoded, &normalized)
				if activity.State != want || activity.Revision != int64(index+1) || activity.LeaseRef != input.LeaseRef || activity.Fence != input.LeaseFence || activity.Generation != input.LeaseGeneration || !reflect.DeepEqual(activity.SafeParameters.AsMap(), normalized) {
					t.Fatal("phase lost exact binding/parameters")
				}
				if index == 0 && activity.SafeResult != "" {
					t.Fatal("RUNNING claimed a result")
				}
			}
			var receipt workflowCatalogReceipt
			if json.Unmarshal([]byte(client.activities[1].SafeResult), &receipt) != nil || receipt.Mode != mode {
				t.Fatal("terminal receipt absent")
			}
			if mode != workflowModePublication {
				break
			}
			p := receipt.Publication
			if p.OffsetBytes != next || p.PageSHA256 != workflowReceiptDigest([]byte(reply.Result.StructuredContent["configuration_page"].(map[string]any)["text"].(string))) {
				t.Fatal("actual page commitment changed")
			}
			next = p.NextOffsetBytes
			if p.EOF {
				if next != p.SizeBytes {
					t.Fatal("false EOF")
				}
				break
			}
			args["publication_read"].(map[string]any)["offset_bytes"] = next
			args["publication_read"].(map[string]any)["configuration_sha256"] = p.ConfigurationSHA256
		}
	}
}

// TestWorkflowCatalogReceiptInvalidInputAndHandlerFailureRemainFailed checks local
// input and owner-integrity failures retain RUNNING/FAILED history without success
// metadata or reflected error details; invalid modes expose no validated parameters.
// TestWorkflowCatalogReceiptInvalidInputAndHandlerFailureRemainFailed проверяет прежнюю
// RUNNING/FAILED history local input/owner-integrity failures без success metadata
// и reflected error details; invalid modes не дают validated parameters.
func TestWorkflowCatalogReceiptInvalidInputAndHandlerFailureRemainFailed(t *testing.T) {
	for _, invalidInput := range []bool{true, false} {
		server, input, client, args, value := actualWorkflowReceipt(t, workflowModePublication)
		if invalidInput {
			args["publication_read"].(map[string]any)["maximum_bytes"] = nil
		} else {
			client.response.GetPublication().ConfigurationSha256 = strings.Repeat("b", 64)
		}
		params, _ := json.Marshal(map[string]any{"name": "get_workflow_catalog", "arguments": args})
		recorder := httptest.NewRecorder()
		server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"failure"`), Params: params}, input)
		if len(client.activities) != 2 || client.activities[0].State != cp.RunToolCallState_RUN_TOOL_CALL_STATE_RUNNING || client.activities[1].State != cp.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED || client.activities[1].SafeResult != "TOOL_UNAVAILABLE" {
			t.Fatal("failed handler acquired success metadata")
		}
		if invalidInput && len(client.activities[1].SafeParameters.AsMap()) != 0 {
			t.Fatal("invalid input was advertised as validated mode")
		}
		if safeToolCallResult("get_workflow_catalog", value, errors.New("PRIVATE_ERROR_CANARY")) != "TOOL_UNAVAILABLE" {
			t.Fatal("handler error leaked metadata")
		}
	}
}

// TestWorkflowCatalogReceiptEmptyExactEOF checks final offset equals source size,
// EOF is true and the empty page digest is bound into the safe receipt.
// TestWorkflowCatalogReceiptEmptyExactEOF проверяет final offset равный source size,
// истинный EOF и empty page digest в safe receipt.
func TestWorkflowCatalogReceiptEmptyExactEOF(t *testing.T) {
	server, input, _, args, initial := actualWorkflowReceipt(t, workflowModePublication)
	page := initial.wire["configuration_page"].(map[string]any)
	selector := args["publication_read"].(map[string]any)
	selector["offset_bytes"] = page["size_bytes"]
	selector["configuration_sha256"] = initial.wire["configuration_sha256"]
	result, err := server.workflowCatalog(t.Context(), input, args)
	if err != nil {
		t.Fatal(err)
	}
	safe, err := safeWorkflowCatalogReceipt(input, args, result)
	var receipt workflowCatalogReceipt
	if err != nil || json.Unmarshal([]byte(safe), &receipt) != nil || !receipt.Publication.EOF ||
		receipt.Publication.OffsetBytes != receipt.Publication.SizeBytes || receipt.Publication.NextOffsetBytes != receipt.Publication.SizeBytes ||
		receipt.Publication.PageSHA256 != workflowReceiptDigest(nil) {
		t.Fatal("exact empty EOF proof unavailable")
	}
}

// TestWorkflowCatalogRequestParametersRejectUnvalidatedModes checks malformed mode/
// page/pin selectors produce empty safe parameters, while valid publication input
// exposes only normalized mode/offset/maximum, not private query/token/refs.
// TestWorkflowCatalogRequestParametersRejectUnvalidatedModes проверяет пустые safe
// parameters malformed mode/page/pin selectors и выдачу лишь normalized mode/offset/
// maximum valid publication input без private query/token/refs.
func TestWorkflowCatalogRequestParametersRejectUnvalidatedModes(t *testing.T) {
	invalid := []map[string]any{
		{"publication_read": nil},
		{"active_runs_read": publicationPins(), "query": "PRIVATE_QUERY_CANARY"},
		{"publication_read": publicationPins(), "active_runs_read": publicationPins()},
		{"query": nil}, {"page_token": strings.Repeat("x", 513)}, {"headers": "PRIVATE_HEADER_CANARY"},
	}
	for _, change := range []map[string]any{
		{"maximum_bytes": nil}, {"maximum_bytes": 3}, {"maximum_bytes": 16385},
		{"offset_bytes": -1}, {"offset_bytes": 0.5}, {"offset_bytes": 1},
		{"workflow_version": 1.5}, {"published_ref": ""}, {"configuration_sha256": "PRIVATE_SECRET_CANARY"},
	} {
		pins := publicationPins()
		for key, value := range change {
			pins[key] = value
		}
		invalid = append(invalid, map[string]any{"publication_read": pins})
	}
	for _, args := range invalid {
		if len(workflowCatalogSafeParameters(args)) != 0 {
			t.Fatal("invalid input acquired validated mode/coordinates")
		}
	}
	pins := publicationPins()
	pins["maximum_bytes"] = 17
	want := map[string]any{"mode": workflowModePublication, "offset_bytes": int64(0), "maximum_bytes": int64(17)}
	if !reflect.DeepEqual(workflowCatalogSafeParameters(map[string]any{"publication_read": pins}), want) {
		t.Fatal("bounded request coordinates missing")
	}
}
