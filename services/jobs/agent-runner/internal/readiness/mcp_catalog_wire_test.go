package readiness

import (
	"context"
	"encoding/json"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
)

func checkCatalogFixture(t *testing.T, catalog []byte, names []string) error {
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
	return checkMCP(t.Context(), server.Client(), endpoint, "synthetic-capability", names)
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
	if json.Unmarshal(raw, &fixtures) != nil || len(fixtures) != 5 {
		t.Fatal("catalog fixture invalid")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			if err := checkCatalogFixture(t, fixture.Catalog, runtimecontract.RuntimeMCPToolNames(fixture.Input)); err != nil {
				t.Fatalf("actual controller catalog rejected: %v stage=%s", err, FailureStage(err))
			}
		})
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
