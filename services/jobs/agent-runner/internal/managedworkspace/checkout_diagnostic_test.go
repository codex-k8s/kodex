package managedworkspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"go.opentelemetry.io/otel/trace"
)

// TestCheckoutBoundaryDiagnostics proves real remote, staging, install and post-owner failures
// reach one redacted JSON log and MCP error without exposing synthetic private request/error bytes.
// TestCheckoutBoundaryDiagnostics доказывает передачу remote, staging, install и post-owner отказов
// в один безопасный JSON-лог и MCP error без синтетических приватных bytes запроса либо ошибки.
func TestCheckoutBoundaryDiagnostics(t *testing.T) {
	for _, stage := range []string{"REMOTE", "STAGING", "INSTALL", "OWNER_AFTER"} {
		t.Run(stage, func(t *testing.T) {
			h, request := snapshotFixture(t)
			activity, calls := handlerFixture(t)
			h.gate, h.record = activity.gate, activity.record
			owner := h.owner
			after := 0
			h.owner = func(ctx context.Context, id, name string, args map[string]any) (map[string]any, error) {
				if name == runtimecontract.WorkspacePolicyTool {
					after++
					if stage == "OWNER_AFTER" && after == 2 {
						return nil, errors.New("PRIVATE_SENTINEL token=private")
					}
					return map[string]any{"profile": "managed-workspace-v1", "input_digest": h.store.input.InputDigest, "tools": runtimecontract.RuntimeWorkspaceToolNames(h.store.input)}, nil
				}
				if stage == "REMOTE" {
					return nil, errors.New("PRIVATE_SENTINEL token=private")
				}
				if stage == "STAGING" {
					h.store.input.WorkspacePolicy.MaximumFileCount = 0
				}
				return owner(ctx, id, name, args)
			}
			if stage == "INSTALL" {
				if err := os.Mkdir(filepath.Join(h.store.root, request.Destination), 0700); err != nil {
					t.Fatal(err)
				}
			}
			var logs bytes.Buffer
			h.logger = (&sharedobservability.Runtime{}).Logger(&logs)
			span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
			ctx := trace.ContextWithSpanContext(t.Context(), span)
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": runtimecontract.WorkspaceToolCheckout, "arguments": request}})
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest("POST", "http://workspace/workspace/mcp", bytes.NewReader(body)).WithContext(ctx))
			if response.Code != 200 || !strings.Contains(response.Body.String(), `"isError":true`) || !strings.Contains(response.Body.String(), "stage="+stage) {
				t.Fatal("missing closed MCP stage", response.Body.String())
			}
			if strings.Count(logs.String(), "\n") != 1 {
				t.Fatal("checkout error logged more than once")
			}
			var log map[string]any
			if json.Unmarshal(logs.Bytes(), &log) != nil || log[checkoutStageKey] != stage || log["trace_id"] != span.TraceID().String() || log["span_id"] != span.SpanID().String() || len(log) != 7 {
				t.Fatal("missing closed traced diagnostic", logs.String())
			}
			for _, forbidden := range []string{"PRIVATE_SENTINEL", "token=private", request.ProjectPath, request.Destination, request.SHA, h.store.root} {
				if strings.Contains(logs.String()+response.Body.String(), forbidden) {
					t.Fatal("private diagnostic bytes leaked")
				}
			}
			if len(*calls) != 2 || (*calls)[1].State != runtimecontract.NativeToolStateFailed {
				t.Fatal("checkout lifecycle changed")
			}
		})
	}
}

// TestCheckoutFailureUnknownNeverParsesErrorText retains the checkout sentinel and closed fallback
// even for forged codes or arbitrary dependency bytes; no string-based classification is allowed.
// TestCheckoutFailureUnknownNeverParsesErrorText сохраняет checkout sentinel и закрытый fallback
// при неизвестных кодах либо dependency bytes; классификация по тексту запрещена.
func TestCheckoutFailureUnknownNeverParsesErrorText(t *testing.T) {
	for code := checkoutInputInvalid; code <= checkoutOwnerAfterDenied; code++ {
		failure := checkoutDiagnostic(checkoutRejected(code))
		if !errors.Is(failure, errCheckout) || failure.stage() == "UNKNOWN" || failure.label() == "UNKNOWN" {
			t.Fatal("known failure lost")
		}
	}
	for _, err := range []error{errors.New("PRIVATE_SENTINEL STAGING_WRITE_DENIED"), checkoutRejected(255), nil} {
		failure := checkoutDiagnostic(err)
		if failure.stage() != "UNKNOWN" || failure.label() != "UNKNOWN" || strings.Contains(failure.Error(), "PRIVATE_SENTINEL") {
			t.Fatal("unsafe fallback")
		}
	}
}
