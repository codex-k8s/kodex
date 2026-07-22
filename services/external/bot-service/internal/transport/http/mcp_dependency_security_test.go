package http

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpInitializePayload = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"security-test","version":"1"}}}`

func TestMCPDependencyAcceptsCanonicalParameterizedAndSameOriginRequests(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		origin      string
		localAddr   net.Addr
		host        string
	}{
		{name: "Codex CLI 0.144.1 wire header", contentType: "application/json"},
		{name: "charset", contentType: "application/json; charset=utf-8"},
		{name: "quoted boundary", contentType: `application/json; boundary="mcp; batch"`},
		{name: "casing and whitespace", contentType: ` Application/JSON ; Charset = UTF-8 ; boundary = "batch-1" `},
		{name: "same origin", contentType: "application/json", origin: "http://example.com"},
		{
			name:        "same origin localhost",
			contentType: "application/json; charset=utf-8",
			origin:      "http://localhost:8080",
			localAddr:   &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080},
			host:        "localhost:8080",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store, runner, publisher := newSessionBarrierService(0)
			body := newObservedMCPBodyReader(mcpInitializePayload)
			request := httptest.NewRequest(http.MethodPost, "/mcp/sessions/session-admin", body)
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

			if recorder.Code != http.StatusOK || recorder.Header().Get("Mcp-Session-Id") == "" || !strings.Contains(recorder.Body.String(), `"serverInfo"`) {
				t.Fatalf("request was not initialized: status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
			if body.readBytes == 0 {
				t.Fatal("accepted request body was not read")
			}
			if store.sessionReads == 0 || store.guardCalls == 0 || runner.secretReads == 0 {
				t.Fatalf("initialize skipped pre-authorization: reads=%d guards=%d token_reads=%d", store.sessionReads, store.guardCalls, runner.secretReads)
			}
			if publisher.posts != 0 {
				t.Fatalf("initialize published unexpected posts: %d", publisher.posts)
			}
		})
	}
}

func TestMCPDependencyRejectsUnsafeHTTPRequestsBeforeSessionEffects(t *testing.T) {
	tests := []struct {
		name              string
		contentTypeValues []string
		origin            string
		secFetchSite      string
		localAddr         net.Addr
		host              string
		wantStatus        int
	}{
		{name: "missing content type", wantStatus: http.StatusUnsupportedMediaType},
		{name: "plain text", contentTypeValues: []string{"text/plain"}, wantStatus: http.StatusUnsupportedMediaType},
		{name: "wrong JSON media type", contentTypeValues: []string{"application/problem+json"}, wantStatus: http.StatusUnsupportedMediaType},
		{name: "missing parameter value", contentTypeValues: []string{"application/json; charset"}, wantStatus: http.StatusUnsupportedMediaType},
		{name: "unterminated quoted parameter", contentTypeValues: []string{`application/json; boundary="batch`}, wantStatus: http.StatusUnsupportedMediaType},
		{name: "duplicate parameter", contentTypeValues: []string{"application/json; charset=utf-8; CHARSET=utf-8"}, wantStatus: http.StatusUnsupportedMediaType},
		{name: "duplicate identical headers", contentTypeValues: []string{"application/json", "application/json"}, wantStatus: http.StatusUnsupportedMediaType},
		{name: "duplicate conflicting headers", contentTypeValues: []string{"application/json", "text/plain"}, wantStatus: http.StatusUnsupportedMediaType},
		{name: "ambiguous combined value", contentTypeValues: []string{"application/json, text/plain"}, wantStatus: http.StatusUnsupportedMediaType},
		{
			name:              "cross origin",
			contentTypeValues: []string{"application/json"},
			origin:            "https://attacker.invalid",
			wantStatus:        http.StatusForbidden,
		},
		{
			name:              "cross site fetch metadata",
			contentTypeValues: []string{"application/json"},
			secFetchSite:      "cross-site",
			wantStatus:        http.StatusForbidden,
		},
		{
			name:              "localhost dns rebinding",
			contentTypeValues: []string{"application/json"},
			localAddr:         &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080},
			host:              "attacker.invalid",
			wantStatus:        http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store, runner, publisher := newSessionBarrierService(0)
			body := newObservedMCPBodyReader(mcpInitializePayload)
			request := httptest.NewRequest(http.MethodPost, "/mcp/sessions/session-admin", body)
			request.Header.Set("Accept", "application/json, text/event-stream")
			request.Header.Set("Authorization", "Bearer session-token")
			if test.contentTypeValues != nil {
				request.Header["Content-Type"] = append([]string(nil), test.contentTypeValues...)
			}
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.secFetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", test.secFetchSite)
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
			if body.readBytes != 0 {
				t.Fatalf("rejected request read %d body bytes", body.readBytes)
			}
			if recorder.Header().Get("Mcp-Session-Id") != "" || strings.Contains(recorder.Body.String(), `"serverInfo"`) {
				t.Fatalf("rejected request initialized a session: headers=%v body=%s", recorder.Header(), recorder.Body.String())
			}
			if store.sessionReads != 0 || store.guardCalls != 0 || runner.secretReads != 0 || publisher.posts != 0 {
				t.Fatalf("unsafe request effects: reads=%d guards=%d token_reads=%d posts=%d", store.sessionReads, store.guardCalls, runner.secretReads, publisher.posts)
			}
			if handler.transportAdmissionStateCount() != 0 || handler.sdkTransportSessionCount() != 0 {
				t.Fatalf("unsafe request state: admission=%d sdk=%d", handler.transportAdmissionStateCount(), handler.sdkTransportSessionCount())
			}
		})
	}
}

func TestMCPGoStreamableClientTransportUsesCanonicalContentType(t *testing.T) {
	handler := newMCPHandler(newSessionBarrierServiceOnly(), defaultMCPRequestBodyBytes)
	var mutex sync.Mutex
	var postContentTypes [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mutex.Lock()
			postContentTypes = append(postContentTypes, append([]string(nil), r.Header.Values("Content-Type")...))
			mutex.Unlock()
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "wire-contract-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             server.URL + "/mcp/sessions/session-admin",
		DisableStandaloneSSE: true,
		HTTPClient:           &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "session-token"}},
	}, nil)
	if err != nil {
		t.Fatalf("MCP connect: %v", err)
	}
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		_ = session.Close()
		t.Fatalf("MCP ListTools: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("MCP close: %v", err)
	}

	mutex.Lock()
	captured := append([][]string(nil), postContentTypes...)
	mutex.Unlock()
	if len(captured) == 0 {
		t.Fatal("official Go transport did not send a POST request")
	}
	for index, values := range captured {
		if len(values) != 1 || values[0] != mcpJSONMediaType {
			t.Fatalf("POST %d Content-Type=%v, want [%s]", index, values, mcpJSONMediaType)
		}
	}
}

type observedMCPBodyReader struct {
	reader    *strings.Reader
	readBytes int
}

func newObservedMCPBodyReader(body string) *observedMCPBodyReader {
	return &observedMCPBodyReader{reader: strings.NewReader(body)}
}

func (reader *observedMCPBodyReader) Read(buffer []byte) (int, error) {
	read, err := reader.reader.Read(buffer)
	reader.readBytes += read
	return read, err
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
