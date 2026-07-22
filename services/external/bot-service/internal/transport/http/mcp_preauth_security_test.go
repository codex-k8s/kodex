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

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPPreauthorizationRejectsInvalidCredentialsBeforeSDKState(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		token     string
		configure func(*sessionBarrierStore)
	}{
		{name: "missing session key", path: "/mcp/sessions/", token: "session-token"},
		{name: "missing bearer", path: "/mcp/sessions/session-admin"},
		{name: "wrong bearer", path: "/mcp/sessions/session-admin", token: "wrong-token"},
		{name: "foreign session key", path: "/mcp/sessions/session-foreign", token: "session-token"},
		{
			name: "expired session", path: "/mcp/sessions/session-admin", token: "session-token",
			configure: func(store *sessionBarrierStore) {
				store.session.ExpiresAt = time.Now().UTC().Add(-time.Minute)
			},
		},
		{
			name: "revoked session", path: "/mcp/sessions/session-admin", token: "session-token",
			configure: func(store *sessionBarrierStore) {
				store.session.Status = "closed"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, store, _, publisher := newSessionBarrierService(0)
			if test.configure != nil {
				test.configure(store)
			}
			handler := newMCPHandlerWithOptions(service, defaultMCPRequestBodyBytes, mcpHandlerOptions{
				MaximumTransportSessions: 2,
				SessionTimeout:           time.Second,
			})
			request := newMCPInitializeRequest(test.path, test.token, mcpInitializePayload)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			assertRejectedMCPBootstrap(t, recorder, http.StatusUnauthorized)
			if handler.transportAdmissionStateCount() != 0 || handler.sdkTransportSessionCount() != 0 || publisher.posts != 0 {
				t.Fatalf("state admission=%d sdk=%d posts=%d", handler.transportAdmissionStateCount(), handler.sdkTransportSessionCount(), publisher.posts)
			}
		})
	}
}

func TestMCPClusterRequestWithoutOriginStillRequiresCredentials(t *testing.T) {
	handler := newMCPHandlerWithOptions(newSessionBarrierServiceOnly(), defaultMCPRequestBodyBytes, mcpHandlerOptions{
		MaximumTransportSessions: 2,
		SessionTimeout:           time.Second,
	})
	request := newMCPInitializeRequest("/mcp/sessions/session-admin", "", mcpInitializePayload)
	request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, &net.TCPAddr{
		IP: net.ParseIP("10.0.0.20"), Port: 8080,
	}))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assertRejectedMCPBootstrap(t, recorder, http.StatusUnauthorized)
	if handler.transportAdmissionStateCount() != 0 || handler.sdkTransportSessionCount() != 0 {
		t.Fatalf("cluster request created state admission=%d sdk=%d", handler.transportAdmissionStateCount(), handler.sdkTransportSessionCount())
	}
}

func TestMCPInvalidInitializeSeriesDoesNotGrowState(t *testing.T) {
	service, _, _, publisher := newSessionBarrierService(0)
	handler := newMCPHandlerWithOptions(service, defaultMCPRequestBodyBytes, mcpHandlerOptions{
		MaximumTransportSessions: 2,
		SessionTimeout:           time.Second,
	})
	payload := strings.Replace(mcpInitializePayload, `"method"`, `"Method"`, 1)
	for attempt := 0; attempt < 32; attempt++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, newMCPInitializeRequest("/mcp/sessions/session-admin", "session-token", payload))
		if recorder.Header().Get("Mcp-Session-Id") != "" || strings.Contains(recorder.Body.String(), `"serverInfo"`) {
			t.Fatalf("attempt %d initialized transport: status=%d body=%s", attempt+1, recorder.Code, recorder.Body.String())
		}
		if handler.transportAdmissionStateCount() != 0 || handler.sdkTransportSessionCount() != 0 {
			t.Fatalf("attempt %d state admission=%d sdk=%d", attempt+1, handler.transportAdmissionStateCount(), handler.sdkTransportSessionCount())
		}
	}
	if publisher.posts != 0 {
		t.Fatalf("invalid initialize publications=%d", publisher.posts)
	}
}

func TestMCPTransportAdmissionBurstAndSlotRelease(t *testing.T) {
	handler := newMCPHandlerWithOptions(newConcurrentMCPAuthorizationService(), defaultMCPRequestBodyBytes, mcpHandlerOptions{
		MaximumTransportSessions: 2,
		SessionTimeout:           time.Second,
	})
	const attempts = 12
	results := make(chan *httptest.ResponseRecorder, attempts)
	var wait sync.WaitGroup
	for attempt := 0; attempt < attempts; attempt++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, newMCPInitializeRequest("/mcp/sessions/session-admin", "session-token", mcpInitializePayload))
			results <- recorder
		}()
	}
	wait.Wait()
	close(results)

	sessionIDs := make([]string, 0, 2)
	unavailable := 0
	for recorder := range results {
		if recorder.Code == http.StatusOK {
			sessionID := recorder.Header().Get("Mcp-Session-Id")
			if sessionID == "" {
				t.Fatalf("successful initialize has no session: body=%s", recorder.Body.String())
			}
			sessionIDs = append(sessionIDs, sessionID)
			continue
		}
		if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Mcp-Session-Id") != "" {
			t.Fatalf("burst status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
		}
		unavailable++
	}
	if len(sessionIDs) != 2 || unavailable != attempts-2 {
		t.Fatalf("burst sessions=%d unavailable=%d", len(sessionIDs), unavailable)
	}
	if handler.activeTransportSessionCount() != 2 || handler.sdkTransportSessionCount() != 2 {
		t.Fatalf("full admission=%d sdk=%d", handler.activeTransportSessionCount(), handler.sdkTransportSessionCount())
	}

	deleteRecorder := serveMCPTransportRequest(handler, http.MethodDelete, "/mcp/sessions/session-admin", "session-token", sessionIDs[0], "")
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if handler.activeTransportSessionCount() != 1 || handler.sdkTransportSessionCount() != 1 {
		t.Fatalf("released admission=%d sdk=%d", handler.activeTransportSessionCount(), handler.sdkTransportSessionCount())
	}

	replacement := httptest.NewRecorder()
	handler.ServeHTTP(replacement, newMCPInitializeRequest("/mcp/sessions/session-admin", "session-token", mcpInitializePayload))
	if replacement.Code != http.StatusOK || replacement.Header().Get("Mcp-Session-Id") == "" {
		t.Fatalf("replacement status=%d headers=%v body=%s", replacement.Code, replacement.Header(), replacement.Body.String())
	}
}

func TestMCPTransportRejectsForeignBindingAndReplay(t *testing.T) {
	service, store, _, _ := newSessionBarrierService(0)
	handler := newMCPHandlerWithOptions(service, defaultMCPRequestBodyBytes, mcpHandlerOptions{
		MaximumTransportSessions: 2,
		SessionTimeout:           time.Second,
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "binding-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   server.URL + "/mcp/sessions/session-admin",
		HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "session-token"}},
	}, nil)
	if err != nil {
		t.Fatalf("MCP connect: %v", err)
	}
	sessionID := session.ID()

	wrongToken := serveMCPTransportRequest(handler, http.MethodPost, "/mcp/sessions/session-admin", "wrong-token", sessionID, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	assertRejectedMCPBootstrap(t, wrongToken, http.StatusUnauthorized)
	if handler.activeTransportSessionCount() != 1 || handler.sdkTransportSessionCount() != 1 {
		t.Fatalf("mismatched token changed state admission=%d sdk=%d", handler.activeTransportSessionCount(), handler.sdkTransportSessionCount())
	}

	store.session.ID = 2
	store.session.SessionKey = "session-foreign"
	foreign := serveMCPTransportRequest(handler, http.MethodPost, "/mcp/sessions/session-foreign", "session-token", sessionID, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	assertRejectedMCPBootstrap(t, foreign, http.StatusForbidden)
	if handler.activeTransportSessionCount() != 1 || handler.sdkTransportSessionCount() != 1 {
		t.Fatalf("foreign request changed state admission=%d sdk=%d", handler.activeTransportSessionCount(), handler.sdkTransportSessionCount())
	}

	store.session.ID = 1
	store.session.SessionKey = "session-admin"
	if err := session.Close(); err != nil {
		t.Fatalf("close MCP session: %v", err)
	}
	waitMCPTransportCounts(t, handler, 0, 0)
	replay := serveMCPTransportRequest(handler, http.MethodPost, "/mcp/sessions/session-admin", "session-token", sessionID, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	assertRejectedMCPBootstrap(t, replay, http.StatusForbidden)
}

func TestMCPTransportIdleCleanupReleasesAdmissionAndSDKState(t *testing.T) {
	const timeout = 40 * time.Millisecond
	handler := newMCPHandlerWithOptions(newSessionBarrierServiceOnly(), defaultMCPRequestBodyBytes, mcpHandlerOptions{
		MaximumTransportSessions: 1,
		SessionTimeout:           timeout,
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "idle-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   server.URL + "/mcp/sessions/session-admin",
		HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "session-token"}},
	}, nil)
	if err != nil {
		t.Fatalf("MCP connect: %v", err)
	}
	sessionID := session.ID()
	if handler.activeTransportSessionCount() != 1 || handler.sdkTransportSessionCount() != 1 {
		t.Fatalf("connected admission=%d sdk=%d", handler.activeTransportSessionCount(), handler.sdkTransportSessionCount())
	}

	waitMCPTransportCounts(t, handler, 0, 0)
	replay := serveMCPTransportRequest(handler, http.MethodPost, "/mcp/sessions/session-admin", "session-token", sessionID, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	assertRejectedMCPBootstrap(t, replay, http.StatusForbidden)
	_ = session.Close()
}

func TestMCPValidClientConnectListToolsAndToolCall(t *testing.T) {
	service, store, _, _ := newSessionBarrierService(0)
	store.requireGuard = false
	store.role.KubernetesAccess = "read-only"
	handler := newMCPHandlerWithOptions(service, defaultMCPRequestBodyBytes, mcpHandlerOptions{
		MaximumTransportSessions: 2,
		SessionTimeout:           time.Second,
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "full-flow-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   server.URL + "/mcp/sessions/session-admin",
		HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: "session-token"}},
	}, nil)
	if err != nil {
		t.Fatalf("MCP connect: %v", err)
	}
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) == 0 {
		t.Fatalf("ListTools() tools=%d error=%v", len(tools.Tools), err)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "mattermost_get_thread", Arguments: map[string]any{"limit": 5},
	})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("CallTool() result=%#v error=%v", result, err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close MCP session: %v", err)
	}
	waitMCPTransportCounts(t, handler, 0, 0)
}

func newMCPInitializeRequest(path string, token string, payload string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request
}

func serveMCPTransportRequest(handler http.Handler, method string, path string, token string, sessionID string, payload string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(payload))
	request.Header.Set("Accept", "application/json, text/event-stream")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Mcp-Session-Id", sessionID)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func assertRejectedMCPBootstrap(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, wantStatus, recorder.Body.String())
	}
	if recorder.Header().Get("Mcp-Session-Id") != "" || strings.Contains(recorder.Body.String(), `"serverInfo"`) {
		t.Fatalf("rejected request leaked MCP bootstrap: headers=%v body=%s", recorder.Header(), recorder.Body.String())
	}
}

func waitMCPTransportCounts(t *testing.T, handler *mcpHTTPHandler, wantAdmission int, wantSDK int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if handler.transportAdmissionStateCount() == wantAdmission && handler.sdkTransportSessionCount() == wantSDK {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("counts admission=%d sdk=%d want=%d/%d", handler.transportAdmissionStateCount(), handler.sdkTransportSessionCount(), wantAdmission, wantSDK)
}

type concurrentMCPAuthorizationStore struct {
	adminrepo.Repository
	session entity.AgentSession
	role    entity.AgentRole
}

func (store *concurrentMCPAuthorizationStore) GetAgentSession(_ context.Context, sessionKey string) (entity.AgentSession, error) {
	if sessionKey != store.session.SessionKey {
		return entity.AgentSession{}, adminrepo.ErrNotFound
	}
	return store.session, nil
}

func (store *concurrentMCPAuthorizationStore) GetAgentRole(context.Context, int64) (entity.AgentRole, error) {
	return store.role, nil
}

type concurrentMCPAuthorizationRunner struct {
	runtimerepo.Runner
}

func (*concurrentMCPAuthorizationRunner) GetMattermostBotTokenSecret(context.Context, string) (runtimerepo.MattermostBotTokenSecret, error) {
	return runtimerepo.MattermostBotTokenSecret{Token: "session-token"}, nil
}

func newConcurrentMCPAuthorizationService() *statusservice.AgentSessionService {
	store := &concurrentMCPAuthorizationStore{
		session: entity.AgentSession{
			ID: 1, SessionKey: "session-admin", ProjectID: 1, ChatID: 1, RoleID: 1,
			MattermostChannelID: "channel-admin", Status: "running", TokenSecretRef: "session-secret",
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		},
		role: entity.AgentRole{ID: 1, ProjectID: 1, KubernetesAccess: "read-only", Enabled: true},
	}
	return statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{
		Store: store, RuntimeRunner: &concurrentMCPAuthorizationRunner{}, StorageReady: true, RuntimeReady: true,
	})
}
