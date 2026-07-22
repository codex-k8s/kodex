package http

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const mcpInitializePayload = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"security-test","version":"1"}}}`

func TestMCPDependencyRejectsUnsafeHTTPRequestsBeforeSessionEffects(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		origin      string
		localAddr   net.Addr
		host        string
		wantStatus  int
	}{
		{
			name:        "cross origin",
			contentType: "application/json",
			origin:      "https://attacker.invalid",
			wantStatus:  http.StatusForbidden,
		},
		{
			name:        "cors safelisted content type",
			contentType: "text/plain",
			wantStatus:  http.StatusUnsupportedMediaType,
		},
		{
			name:        "localhost dns rebinding",
			contentType: "application/json",
			localAddr:   &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080},
			host:        "attacker.invalid",
			wantStatus:  http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store, runner, publisher := newSessionBarrierService(0)
			request := httptest.NewRequest(http.MethodPost, "/mcp/sessions/session-admin", strings.NewReader(mcpInitializePayload))
			request.Header.Set("Accept", "application/json, text/event-stream")
			request.Header.Set("Authorization", "Bearer session-token")
			request.Header.Set("Content-Type", test.contentType)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.localAddr != nil {
				request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, test.localAddr))
			}
			if test.host != "" {
				request.Host = test.host
			}
			recorder := httptest.NewRecorder()
			handler := newMCPHandlerWithOptions(service, defaultMCPRequestBodyBytes, mcpHandlerOptions{
				MaximumTransportSessions: 2,
				SessionTimeout:           time.Second,
			})

			handler.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d, want=%d body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			guards := store.guardSnapshot()
			if store.sessionReads != 0 || guards.calls != 0 || runner.secretReads != 0 || publisher.posts != 0 {
				t.Fatalf("unsafe request effects: reads=%d guards=%d token_reads=%d posts=%d", store.sessionReads, guards.calls, runner.secretReads, publisher.posts)
			}
			if handler.transportAdmissionStateCount() != 0 || handler.sdkTransportSessionCount() != 0 {
				t.Fatalf("unsafe request state: admission=%d sdk=%d", handler.transportAdmissionStateCount(), handler.sdkTransportSessionCount())
			}
		})
	}
}

func TestMCPDependencyRejectsNonCanonicalJSONRPCKeys(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "case insensitive method",
			payload: strings.Replace(mcpInitializePayload, `"method"`, `"Method"`, 1),
		},
		{
			name:    "null suffixed duplicate method",
			payload: strings.Replace(mcpInitializePayload, `"method":"initialize"`, `"method":"unknown","method\u0000":"initialize"`, 1),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/mcp/sessions/session-admin", strings.NewReader(test.payload))
			request.Header.Set("Accept", "application/json, text/event-stream")
			request.Header.Set("Authorization", "Bearer session-token")
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			newMCPHandler(newSessionBarrierServiceOnly(), defaultMCPRequestBodyBytes).ServeHTTP(recorder, request)

			if recorder.Header().Get("Mcp-Session-Id") != "" || strings.Contains(recorder.Body.String(), `"serverInfo"`) {
				t.Fatalf("non-canonical JSON-RPC key initialized a session: status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
		})
	}
}
