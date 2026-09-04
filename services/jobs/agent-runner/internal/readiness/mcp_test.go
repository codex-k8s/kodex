package readiness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

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
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":"agent-runner-tools","result":{"tools":[{"name":"required"}]}}`))
		default:
			http.Error(writer, "invalid", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)

	if err := checkMCP(t.Context(), server.Client(), endpoint, "capability"); err != nil {
		t.Fatalf("checkMCP() error = %v", err)
	}
	if !reflect.DeepEqual(methods, []string{"initialize", "notifications/initialized", "tools/list"}) {
		t.Fatalf("unexpected MCP lifecycle: %#v", methods)
	}
}

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

	if err := checkMCP(t.Context(), server.Client(), endpoint, "capability"); err == nil {
		t.Fatal("incomplete MCP lifecycle was accepted")
	}
}
