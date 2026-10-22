package readiness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync/atomic"
	"testing"
)

// TestCheckMCPRejectsRevokedGrantBeforeToolDiscovery checks a synthetic HTTP 403 on initialize stops readiness after exactly one request, without later catalog discovery.
// TestCheckMCPRejectsRevokedGrantBeforeToolDiscovery проверяет остановку readiness после ровно одного запроса при синтетическом HTTP 403 на initialize без последующего поиска каталога.
func TestCheckMCPRejectsRevokedGrantBeforeToolDiscovery(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		http.Error(writer, "revoked", http.StatusForbidden)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	if err := checkMCP(t.Context(), server.Client(), endpoint, "revoked-fixture", []string{"required"}); err == nil {
		t.Fatal("revoked grant passed the provider startup barrier")
	}
	if calls.Load() != 1 {
		t.Fatal("MCP continued after the authoritative grant rejection")
	}
}

// TestCheckMCPVerifiesTheCompleteInitializationLifecycle checks local bearer delivery and exact initialize/initialized/tools-list order for an accepted synthetic catalog.
// TestCheckMCPVerifiesTheCompleteInitializationLifecycle проверяет передачу локального bearer и точный порядок initialize, initialized, tools-list для принятого синтетического каталога.
func TestCheckMCPVerifiesTheCompleteInitializationLifecycle(t *testing.T) {
	t.Parallel()
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer capability" {
			http.Error(writer, "not found", http.StatusNotFound)
			return
		}
		var message struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if json.NewDecoder(request.Body).Decode(&message) != nil {
			http.Error(writer, "invalid", http.StatusBadRequest)
			return
		}
		methods = append(methods, message.Method)
		switch message.Method {
		case "initialize":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":"agent-runner-readiness","result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"test","version":"1"}}}`))
		case "notifications/initialized":
			writer.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":"agent-runner-tools","result":{"tools":[{"name":"required","description":"Required tool","inputSchema":{"type":"object"}}]}}`))
		default:
			http.Error(writer, "invalid", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)

	if err := checkMCP(t.Context(), server.Client(), endpoint, "capability", []string{"required"}); err != nil {
		t.Fatalf("checkMCP() error = %v", err)
	}
	if !reflect.DeepEqual(methods, []string{"initialize", "notifications/initialized", "tools/list"}) {
		t.Fatalf("unexpected MCP lifecycle: %#v", methods)
	}
}

// TestCheckMCPRejectsAProtocolThatDoesNotAcceptInitialized checks a server refusing the initialized notification fails readiness rather than accepting an incomplete lifecycle.
// TestCheckMCPRejectsAProtocolThatDoesNotAcceptInitialized проверяет отказ readiness при сервере, отклоняющем уведомление initialized, вместо принятия неполного жизненного цикла.
func TestCheckMCPRejectsAProtocolThatDoesNotAcceptInitialized(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var message struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(request.Body).Decode(&message)
		if message.Method == "initialize" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":"agent-runner-readiness","result":{"protocolVersion":"2025-06-18"}}`))
			return
		}
		http.Error(writer, "unsupported", http.StatusBadRequest)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)

	if err := checkMCP(t.Context(), server.Client(), endpoint, "capability", []string{"required"}); err == nil {
		t.Fatal("incomplete MCP lifecycle was accepted")
	}
}

// TestCheckMCPRejectsUnknownMissingAndDuplicateToolsBeforeProviderStart checks unknown, missing and duplicate tool names fail the exact required-name catalog comparison through local HTTP fixtures.
// TestCheckMCPRejectsUnknownMissingAndDuplicateToolsBeforeProviderStart проверяет отказ точного сопоставления имён каталога для неизвестных, отсутствующих и повторных инструментов через локальные HTTP-образцы.
func TestCheckMCPRejectsUnknownMissingAndDuplicateToolsBeforeProviderStart(t *testing.T) {
	t.Parallel()
	for name, tools := range map[string]string{
		"unknown":   `[{"name":"required","description":"Required tool","inputSchema":{}},{"name":"foreign","description":"Foreign tool","inputSchema":{}}]`,
		"missing":   `[{"name":"different","description":"Different tool","inputSchema":{}}]`,
		"duplicate": `[{"name":"required","description":"Required tool","inputSchema":{}},{"name":"required","description":"Required tool","inputSchema":{}}]`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var message struct {
					Method string `json:"method"`
				}
				_ = json.NewDecoder(request.Body).Decode(&message)
				switch message.Method {
				case "initialize":
					writer.Header().Set("Content-Type", "application/json")
					_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":"agent-runner-readiness","result":{}}`))
				case "notifications/initialized":
					writer.WriteHeader(http.StatusAccepted)
				case "tools/list":
					writer.Header().Set("Content-Type", "application/json")
					_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":"agent-runner-tools","result":{"tools":` + tools + `}}`))
				}
			}))
			defer server.Close()
			endpoint, _ := url.Parse(server.URL)
			if err := checkMCP(t.Context(), server.Client(), endpoint, "capability", []string{"required"}); err == nil {
				t.Fatal("MCP catalog outside RuntimeRevision was accepted")
			}
		})
	}
}
