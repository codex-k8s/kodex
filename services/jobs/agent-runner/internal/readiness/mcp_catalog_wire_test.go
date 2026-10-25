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

// checkCatalogFixture serves a synthetic initialize/initialized/catalog sequence over a local temporary HTTP listener and invokes the real readiness validator.
// Unexpected methods fail the test; no owner service or model endpoint is contacted.
// checkCatalogFixture обслуживает синтетическую последовательность initialize, initialized и каталога на временном локальном HTTP-слушателе, вызывает настоящий валидатор готовности.
// Неожиданные методы завершают тест ошибкой; сервис владельца и endpoint модели не вызываются.
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

// TestRuntimeMCPCatalogWireConsumer consumes seventeen explicitly provided controller-produced wire fixtures and verifies scope/file/delegation/launch/bundle combinations through readiness.
// Missing fixture-path configuration skips this producer-dependent check rather than proving wire compatibility.
// TestRuntimeMCPCatalogWireConsumer читает семнадцать явно переданных wire-образцов producer контроллера и проверяет сочетания области, файлов, делегирования, запуска и пакета через readiness.
// Отсутствующий путь образца пропускает зависимую от producer проверку, а не доказывает wire-совместимость.
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
	if json.Unmarshal(raw, &fixtures) != nil || len(fixtures) != 17 {
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
			if fixture.Name == "repository-bundle-and-files" {
				unauthorized := fixture.Input
				unauthorized.IntegrationGrants = append([]runtimecontract.RunnerIntegrationGrant(nil), fixture.Input.IntegrationGrants...)
				unauthorized.IntegrationGrants[0].ApprovalPolicy = "NONE"
				if err := checkCatalogFixture(t, fixture.Catalog, runtimecontract.RuntimeMCPToolNames(unauthorized), unauthorized); err == nil || FailureStage(err) != "CATALOG_BINDING" {
					t.Fatal("extra repository bundle tool bypassed the immutable grant boundary")
				}
				var envelope struct {
					JSONRPC string `json:"jsonrpc"`
					ID      string `json:"id"`
					Result  struct {
						Tools []map[string]any `json:"tools"`
					} `json:"result"`
				}
				if json.Unmarshal(fixture.Catalog, &envelope) != nil {
					t.Fatal("catalog decode failed")
				}
				filtered := make([]map[string]any, 0, len(envelope.Result.Tools))
				for _, tool := range envelope.Result.Tools {
					if tool["name"] != runtimecontract.RepositoryBundlePrepareTool {
						filtered = append(filtered, tool)
					}
				}
				envelope.Result.Tools = filtered
				missing, err := json.Marshal(envelope)
				if err != nil {
					t.Fatal(err)
				}
				if err := checkCatalogFixture(t, missing, runtimecontract.RuntimeMCPToolNames(fixture.Input), fixture.Input); err == nil || FailureStage(err) != "CATALOG_BINDING" {
					t.Fatal("missing repository bundle tool passed readiness")
				}
			}
		})
	}
	if !seen["repository-bundle-and-files"] {
		t.Fatal("repository bundle catalog fixture missing")
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

// TestMCPOutputSchemaCompatibilityAndClosedFailures checks optional/object output schemas remain readable while null/non-object/malformed schemas or unknown descriptor fields fail at CATALOG_SCHEMA.
// TestMCPOutputSchemaCompatibilityAndClosedFailures проверяет читаемость отсутствующей и объектной выходных схем и отказ на CATALOG_SCHEMA для null, не-объектных и некорректных схем либо неизвестных полей описания.
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

// TestMCPWorkflowLaunchCatalogStillRequiresExactBinding checks both missing-authorized and extra-unauthorized launch tools fail at CATALOG_BINDING before any workflow launch.
// TestMCPWorkflowLaunchCatalogStillRequiresExactBinding проверяет отказ на CATALOG_BINDING при отсутствии разрешённого либо наличии неразрешённого инструмента запуска до любого запуска процесса.
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

// TestMCPStartupFailsClosedBeforeDiscoveryOnTLSOrCancellation checks an untrusted local TLS fixture and an already canceled context fail at INITIALIZE with zero server-handler calls.
// TestMCPStartupFailsClosedBeforeDiscoveryOnTLSOrCancellation проверяет отказ на INITIALIZE с нулём вызовов серверного обработчика для недоверенного локального TLS-образца и уже отменённого контекста.
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
