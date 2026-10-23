package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// workflowCatalogInputClient wraps a catalog fake with injectable owner/audit errors
// and activity recording to distinguish local-input recovery from remote failures.
// workflowCatalogInputClient дополняет catalog fake injectable owner/audit errors
// и activity recording для различения local-input recovery и remote failures.
type workflowCatalogInputClient struct {
	*workflowCatalogClient
	activities []*cp.RecordRunToolCallRequest
	ownerError error
	failPhase  int64
}

// GetExecutionWorkflowCatalog records an injected owner failure as one call, otherwise
// delegates unchanged context/request/options to the underlying catalog fake.
// GetExecutionWorkflowCatalog записывает injected owner failure как один call, иначе
// передаёт неизменные context/request/options исходному catalog fake.
func (c *workflowCatalogInputClient) GetExecutionWorkflowCatalog(ctx context.Context, request *cp.GetExecutionWorkflowCatalogRequest, options ...grpc.CallOption) (*cp.GetExecutionWorkflowCatalogResponse, error) {
	if c.ownerError != nil {
		c.calls++
		c.request = request
		return nil, c.ownerError
	}
	return c.workflowCatalogClient.GetExecutionWorkflowCatalog(ctx, request, options...)
}

// RecordRunToolCall records activity pointers and optionally fails the selected
// phase with a private audit canary; no actual persistent event is written.
// RecordRunToolCall записывает activity pointers и при настройке отклоняет выбранную
// phase с private audit canary; persistent event фактически не записывается.
func (c *workflowCatalogInputClient) RecordRunToolCall(_ context.Context, request *cp.RecordRunToolCallRequest, _ ...grpc.CallOption) (*cp.RecordRunToolCallResponse, error) {
	c.activities = append(c.activities, request)
	if request.Revision == c.failPhase {
		return nil, status.Error(codes.Unavailable, "PRIVATE_AUDIT_CANARY")
	}
	return &cp.RecordRunToolCallResponse{Event: &cp.RunEvent{Ref: "evt_fixture01"}}, nil
}

// workflowCatalogInputFixture wraps a fresh publication fixture in the recovery fake
// and returns exact publication pins, isolating mutable state for each scenario.
// workflowCatalogInputFixture оборачивает новую publication fixture recovery fake
// и возвращает exact publication pins с отдельным mutable state каждого сценария.
func workflowCatalogInputFixture(t *testing.T) (*Server, runtimecontract.RunnerInput, *workflowCatalogInputClient, map[string]any) {
	t.Helper()
	server, input, owner, _, _ := publicationReadFixture(t)
	client := &workflowCatalogInputClient{workflowCatalogClient: owner}
	server.control.Runtime = client
	return server, input, client, map[string]any{"publication_read": publicationPins()}
}

// workflowCatalogInputCall drives local MCP, captures logs, checks textual/structured
// error and retry fields agree and verifies private input/owner/audit/response canaries
// are absent before returning structured content; it installs a per-call test logger.
// workflowCatalogInputCall выполняет local MCP,собирает logs,проверяет согласие textual/
// structured error/retry fields и отсутствие private input/owner/audit/response canaries
// до возврата structured content; каждый call получает test logger.
func workflowCatalogInputCall(t *testing.T, server *Server, input runtimecontract.RunnerInput, args map[string]any) map[string]any {
	t.Helper()
	var logs bytes.Buffer
	server.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	params, err := json.Marshal(map[string]any{"name": "get_workflow_catalog", "arguments": args})
	if err != nil {
		t.Fatal("fixture encoding failed")
	}
	writer := httptest.NewRecorder()
	server.callTool(writer, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"fixture"`), Params: params}, input)
	var response struct {
		Result struct {
			Structured map[string]any `json:"structuredContent"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if json.Unmarshal(writer.Body.Bytes(), &response) != nil {
		t.Fatal("fixture response decoding failed")
	}
	if response.Result.Structured != nil {
		var text map[string]any
		if len(response.Result.Content) != 1 || json.Unmarshal([]byte(response.Result.Content[0].Text), &text) != nil || text["error_code"] != response.Result.Structured["error_code"] || text["retryable"] != response.Result.Structured["retryable"] {
			t.Fatal("native text and structured result disagree")
		}
		if response.Result.Structured["error_code"] != nil && !response.Result.IsError {
			t.Fatal("failure became successful native result")
		}
	}
	for _, canary := range []string{"PRIVATE_INPUT_CANARY", "PRIVATE_OWNER_CANARY", "PRIVATE_AUDIT_CANARY", "PRIVATE_RESPONSE_CANARY"} {
		if strings.Contains(writer.Body.String(), canary) || strings.Contains(logs.String(), canary) {
			t.Fatal("recovery exposed private data")
		}
	}
	return response.Result.Structured
}

// TestWorkflowCatalogInputRecoveryLocalShape checks thirteen malformed mode/pin/page
// selectors never call the owner, retain one-correction guidance and produce existing
// RUNNING/FAILED safe activity semantics without validated-mode metadata.
// TestWorkflowCatalogInputRecoveryLocalShape проверяет тринадцать malformed mode/pin/page
// selectors без owner call,с one-correction guidance и прежней RUNNING/FAILED safe
// activity semantics без validated-mode metadata.
func TestWorkflowCatalogInputRecoveryLocalShape(t *testing.T) {
	for name, change := range map[string]func(map[string]any){
		"unnested pins":                   func(a map[string]any) { delete(a, "publication_read"); a["workflow_ref"] = "wfl_workflow01" },
		"mixed selectors":                 func(a map[string]any) { a["active_runs_read"] = publicationPins() },
		"unknown top field":               func(a map[string]any) { a["PRIVATE_INPUT_CANARY"] = "PRIVATE_INPUT_CANARY" },
		"query with publication":          func(a map[string]any) { a["query"] = "PRIVATE_INPUT_CANARY" },
		"null selector":                   func(a map[string]any) { a["publication_read"] = nil },
		"null page size":                  func(a map[string]any) { a["publication_read"].(map[string]any)["maximum_bytes"] = nil },
		"publication number alias":        func(a map[string]any) { a["publication_read"].(map[string]any)["published_version"] = 14 },
		"fractional version":              func(a map[string]any) { a["publication_read"].(map[string]any)["workflow_version"] = 43.5 },
		"missing digest":                  func(a map[string]any) { delete(a["publication_read"].(map[string]any), "spec_digest") },
		"continuation without commitment": func(a map[string]any) { a["publication_read"].(map[string]any)["offset_bytes"] = 1 },
		"bad page size":                   func(a map[string]any) { a["publication_read"].(map[string]any)["maximum_bytes"] = 3 },
		"discovery query type":            func(a map[string]any) { delete(a, "publication_read"); a["query"] = nil },
		"active query": func(a map[string]any) {
			delete(a, "publication_read")
			a["active_runs_read"] = publicationPins()
			a["query"] = "PRIVATE_INPUT_CANARY"
		},
	} {
		t.Run(name, func(t *testing.T) {
			server, input, client, args := workflowCatalogInputFixture(t)
			change(args)
			result := workflowCatalogInputCall(t, server, input, args)
			if result["error_code"] != "WORKFLOW_CATALOG_INPUT_INVALID" || result["retryable"] != true || client.calls != 0 {
				t.Fatal("local shape failure lost bounded recovery or reached owner read")
			}
			guidance, _ := result["guidance"].(string)
			for _, required := range []string{"tools/list", "at most once", "publication_read", "workflow_version", "published_version", "offset_bytes", "configuration_sha256", "discovery", "Do not retry"} {
				if !strings.Contains(guidance, required) {
					t.Fatal("recovery guidance omitted required boundary")
				}
			}
			if len(client.activities) != 2 || client.activities[0].State != cp.RunToolCallState_RUN_TOOL_CALL_STATE_RUNNING || client.activities[1].State != cp.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED || client.activities[1].SafeResult != "TOOL_UNAVAILABLE" || len(client.activities[1].SafeParameters.AsMap()) != 0 {
				t.Fatal("local recovery changed existing owner activity semantics")
			}
		})
	}
}

// TestWorkflowCatalogInputRecoveryOwnerFailuresClosed checks ten owner status codes
// and corrupt/missing/mixed provenance responses cannot acquire corrective input guidance,
// including a remote typed local marker or matching/joined error text.
// TestWorkflowCatalogInputRecoveryOwnerFailuresClosed проверяет отсутствие corrective
// input guidance у десяти owner status codes и corrupt/missing/provenance responses,
// включая remote typed local marker и matching/joined error text.
func TestWorkflowCatalogInputRecoveryOwnerFailuresClosed(t *testing.T) {
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unauthenticated, codes.NotFound, codes.Aborted, codes.FailedPrecondition, codes.InvalidArgument, codes.Unavailable, codes.Unknown, codes.DataLoss, codes.DeadlineExceeded} {
		t.Run(code.String(), func(t *testing.T) {
			server, input, client, args := workflowCatalogInputFixture(t)
			client.ownerError = status.Error(code, "PRIVATE_OWNER_CANARY")
			result := workflowCatalogInputCall(t, server, input, args)
			if result["error_code"] != "TOOL_UNAVAILABLE" || result["retryable"] != false || result["guidance"] != nil || client.calls != 1 {
				t.Fatal("owner failure became recoverable or repeated")
			}
		})
	}
	for name, change := range map[string]func(*workflowCatalogInputClient, map[string]any){
		"missing response": func(c *workflowCatalogInputClient, _ map[string]any) { c.response = nil },
		"digest drift": func(c *workflowCatalogInputClient, _ map[string]any) {
			c.response.GetPublication().ConfigurationSha256 = strings.Repeat("b", 64)
		},
		"invalid response": func(c *workflowCatalogInputClient, _ map[string]any) {
			c.response.GetPublication().ConfigurationJson = []byte("PRIVATE_RESPONSE_CANARY")
		},
		"unknown proto": func(c *workflowCatalogInputClient, _ map[string]any) {
			c.response.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		},
		"pin mismatch": func(_ *workflowCatalogInputClient, a map[string]any) {
			a["publication_read"].(map[string]any)["workflow_version"] = 43
		},
		"page commitment drift": func(_ *workflowCatalogInputClient, a map[string]any) {
			a["publication_read"].(map[string]any)["configuration_sha256"] = strings.Repeat("b", 64)
		},
		"remote matching message": func(c *workflowCatalogInputClient, _ map[string]any) {
			c.ownerError = errors.Join(errors.New("workflow catalog input is invalid"), status.Error(codes.PermissionDenied, "PRIVATE_OWNER_CANARY"))
		},
		"owner typed marker": func(c *workflowCatalogInputClient, _ map[string]any) { c.ownerError = &workflowCatalogInputError{} },
	} {
		t.Run(name, func(t *testing.T) {
			server, input, client, args := workflowCatalogInputFixture(t)
			change(client, args)
			result := workflowCatalogInputCall(t, server, input, args)
			if result["error_code"] != "TOOL_UNAVAILABLE" || result["retryable"] != false || result["guidance"] != nil || client.calls != 1 {
				t.Fatal("projection or integrity failure acquired local input guidance")
			}
		})
	}
}

// TestWorkflowCatalogInputRecoveryRequiresAcceptedAudit checks failure of either
// audit phase suppresses local corrective guidance even when the input itself is invalid.
// TestWorkflowCatalogInputRecoveryRequiresAcceptedAudit проверяет запрет local corrective
// guidance при отказе любой audit phase, даже если input сам неверен.
func TestWorkflowCatalogInputRecoveryRequiresAcceptedAudit(t *testing.T) {
	for _, phase := range []int64{1, 2} {
		server, input, client, args := workflowCatalogInputFixture(t)
		client.failPhase = phase
		args["publication_read"] = nil
		result := workflowCatalogInputCall(t, server, input, args)
		if client.calls != 0 || result["retryable"] == true || result["error_code"] == "WORKFLOW_CATALOG_INPUT_INVALID" || result["guidance"] != nil {
			t.Fatal("audit failure became corrective input guidance")
		}
	}
}

// TestWorkflowCatalogInputRecoveryPreservesValidModes checks all three valid read
// modes keep one owner query, two successful-path activity phases and exact safe mode
// metadata through local dispatch; it does not establish a durable owner commit.
// TestWorkflowCatalogInputRecoveryPreservesValidModes проверяет один owner query,две
// successful-path activity phases и exact safe mode metadata трёх valid read modes
// через local dispatch, не доказывая durable owner commit.
func TestWorkflowCatalogInputRecoveryPreservesValidModes(t *testing.T) {
	for _, mode := range []string{workflowModeDiscovery, workflowModePublication, workflowModeActiveRuns} {
		t.Run(mode, func(t *testing.T) {
			server, input, client, args := workflowCatalogInputFixture(t)
			switch mode {
			case workflowModeDiscovery:
				client.response, args = workflowCatalogFixture(), map[string]any{"query": "Процесс"}
			case workflowModeActiveRuns:
				client.response, args = activeRunResponse(), map[string]any{"active_runs_read": publicationPins()}
			}
			result := workflowCatalogInputCall(t, server, input, args)
			if result["error_code"] != nil || client.calls != 1 || len(client.activities) != 2 || client.activities[1].State != cp.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED || client.activities[1].SafeParameters.AsMap()["mode"] != mode {
				t.Fatal("valid read changed owner query or receipt")
			}
		})
	}
}

// TestWorkflowCatalogInputRecoveryRejectsJoinedMarker checks joined local markers,
// matching plain text and specialized read errors yield no corrective input guidance.
// TestWorkflowCatalogInputRecoveryRejectsJoinedMarker проверяет отсутствие corrective
// input guidance у joined local markers,matching plain text и specialized read errors.
func TestWorkflowCatalogInputRecoveryRejectsJoinedMarker(t *testing.T) {
	for _, err := range []error{
		errors.Join(&workflowCatalogInputError{}, errors.New("PRIVATE_AUDIT_CANARY")),
		errors.New(workflowCatalogInputInvalidMessage), errWorkflowCatalogRead,
	} {
		if workflowCatalogInputRecoveryGuidance(err) != "" {
			t.Fatal("nonlocal or joined failure acquired corrective guidance")
		}
	}
}
