package readiness

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func checkCatalogFixture(t *testing.T, catalog []byte, names []string, inputs ...runtimecontract.RunnerInput) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Method string }
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid fixture request")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"agent-runner-readiness","result":{"protocolVersion":"2025-06-18"}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_, _ = w.Write(catalog)
		default:
			t.Error("unexpected MCP effect")
			w.WriteHeader(400)
		}
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	return checkMCP(t.Context(), server.Client(), endpoint, "synthetic-capability", names, inputs...)
}

func TestRuntimeMCPCatalogWireConsumer(t *testing.T) {
	path := os.Getenv("KODEX_RUNTIME_MCP_CATALOG_FIXTURE")
	if path == "" {
		t.Skip("KODEX_RUNTIME_MCP_CATALOG_FIXTURE is not configured")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name    string
		Input   runtimecontract.RunnerInput
		Catalog json.RawMessage
	}
	if json.Unmarshal(raw, &fixtures) != nil || len(fixtures) != 16 {
		t.Fatal("catalog fixture invalid")
	}
	seen := make(map[string]bool, len(fixtures))
	for _, fixture := range fixtures {
		if seen[fixture.Name] {
			t.Fatal("duplicate catalog fixture")
		}
		seen[fixture.Name] = true
		t.Run(fixture.Name, func(t *testing.T) {
			if err := checkCatalogFixture(t, fixture.Catalog, runtimecontract.RuntimeMCPToolNames(fixture.Input), fixture.Input); err != nil {
				t.Fatalf("actual controller catalog rejected: %v stage=%s", err, FailureStage(err))
			}
		})
	}
	for _, launch := range []bool{false, true} {
		for _, vfs := range []bool{false, true} {
			for _, delegation := range []bool{false, true} {
				if !seen[fmt.Sprintf("ordinary-launch-%t-vfs-%t-delegation-%t", launch, vfs, delegation)] {
					t.Fatal("ordinary workflow catalog combination missing")
				}
			}
		}
	}
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		if !seen["assistant-launch-"+string(scope)] {
			t.Fatal("assistant workflow exclusion missing")
		}
	}
}

func TestMCPOutputSchemaCompatibilityAndClosedFailures(t *testing.T) {
	for name, extra := range map[string]string{
		"legacy": "", "object": `,"outputSchema":{"type":"object","properties":{"ok":{"type":"boolean"}}}`,
		"null": `,"outputSchema":null`, "array": `,"outputSchema":[]`, "scalar": `,"outputSchema":"object"`, "wrong-type": `,"outputSchema":{"type":"string"}`, "missing-type": `,"outputSchema":{}`, "unknown-field": `,"outputSchema":{"type":"object"},"authority":"forged"`,
	} {
		t.Run(name, func(t *testing.T) {
			raw := []byte(`{"jsonrpc":"2.0","id":"agent-runner-tools","result":{"tools":[{"name":"required","description":"Fixture","inputSchema":{"type":"object"}` + extra + `}]}}`)
			err := checkCatalogFixture(t, raw, []string{"required"})
			if name == "legacy" || name == "object" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || FailureStage(err) != "CATALOG_SCHEMA" {
				t.Fatal("invalid descriptor did not fail closed at schema stage")
			}
		})
	}
}

func TestMCPWorkflowLaunchCatalogStillRequiresExactBinding(t *testing.T) {
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone, ProjectRef: "prj_fixture"}
	for _, launch := range []bool{false, true} {
		t.Run(fmt.Sprintf("capability-%t", launch), func(t *testing.T) {
			copy := input
			if launch {
				copy.Capabilities = []string{"platform.run.launch"}
			}
			tools := []map[string]any{{"name": "propose_run_metadata", "description": "Fixture metadata", "inputSchema": map[string]any{"type": "object"}}}
			// Подменённый wire-каталог: лишний launch без права либо отсутствующий
			// launch при назначенном праве. Readiness не принимает ни один вариант.
			if !launch {
				tools = append(tools, map[string]any{"name": "launch_workflow", "description": "Fixture workflow", "inputSchema": map[string]any{"type": "object"}})
			}
			raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "agent-runner-tools", "result": map[string]any{"tools": tools}})
			if err != nil {
				t.Fatal(err)
			}
			if err := checkCatalogFixture(t, raw, runtimecontract.RuntimeMCPToolNames(copy), copy); err == nil || FailureStage(err) != "CATALOG_BINDING" {
				t.Fatal("workflow launch catalog mismatch did not fail closed")
			}
		})
	}
}

func TestMCPStartupFailsClosedBeforeDiscoveryOnTLSOrCancellation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	// Обычный trust store не доверяет self-signed fixture; никакого skipTLSVerify.
	client := &http.Client{}
	err := checkMCP(t.Context(), client, endpoint, "synthetic", []string{"required"})
	if err == nil || FailureStage(err) != "INITIALIZE" || calls.Load() != 0 {
		t.Fatal("untrusted TLS reached MCP discovery")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = checkMCP(ctx, server.Client(), endpoint, "synthetic", []string{"required"})
	if err == nil || FailureStage(err) != "INITIALIZE" || calls.Load() != 0 {
		t.Fatal("cancelled startup reached MCP discovery")
	}
}
