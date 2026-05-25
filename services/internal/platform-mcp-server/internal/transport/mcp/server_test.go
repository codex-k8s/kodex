package mcptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	ownerclients "github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/clients/owners"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestToolsListSnapshot(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools(): %v", err)
	}
	snapshot := make([]toolSnapshot, 0, len(result.Tools))
	for _, tool := range result.Tools {
		snapshot = append(snapshot, toolSnapshot{
			Name:            tool.Name,
			Description:     tool.Description,
			HasInputSchema:  tool.InputSchema != nil,
			HasOutputSchema: tool.OutputSchema != nil,
		})
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent(): %v", err)
	}
	const expected = `[
  {
    "name": "agent.run.record_state",
    "description": "Зафиксировать состояние агентного запуска через agent-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "agent.run.start",
    "description": "Запустить роль в рамках агентной сессии через agent-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "agent.session.record_snapshot",
    "description": "Записать ссылку на снимок состояния сессии через agent-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "agent.session.start",
    "description": "Начать или продолжить агентную сессию через agent-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "diagnostics.mcp_status.read",
    "description": "Ограниченная диагностика MCP-регистра и маршрутов без секретов и бизнес-данных.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "diagnostics.run_context.read",
    "description": "Прочитать безопасную сводку сессии и агентных запусков через agent-manager без бизнес-состояния в MCP.",
    "has_input_schema": true,
    "has_output_schema": true
  }
]`
	if string(data) != expected {
		t.Fatalf("tools/list snapshot mismatch:\n%s", data)
	}
}

func TestDiagnosticsStatusDoesNotExposeSecrets(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      ToolDiagnosticsMCPStatusRead,
		Arguments: map[string]any{"include_dependency_routes": true},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if strings.Contains(string(data), "secret-token") {
		t.Fatalf("structured content exposes secret: %s", data)
	}
	if !strings.Contains(string(data), "project-catalog:9090") {
		t.Fatalf("structured content does not include safe route target: %s", data)
	}
}

func TestAgentSessionStartRoutesToOwner(t *testing.T) {
	t.Parallel()

	agent := newFakeAgentManagerClient()
	server := newTestServerWithAgent(t, agent)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolAgentSessionStart,
		Arguments: map[string]any{
			"meta":                   validCommandMetaArgs(),
			"scope":                  map[string]any{"type": "project", "ref": "project-1"},
			"provider_work_item_ref": "issue-1",
			"created_by_actor_ref":   "user:1",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if agent.startSessionCalls != 1 {
		t.Fatalf("startSessionCalls = %d, want 1", agent.startSessionCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "session-1") {
		t.Fatalf("structured content does not contain owner response: %s", data)
	}
}

func TestRunContextReadRoutesToOwner(t *testing.T) {
	t.Parallel()

	agent := newFakeAgentManagerClient()
	server := newTestServerWithAgent(t, agent)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolDiagnosticsRunContextRead,
		Arguments: map[string]any{
			"meta":         validQueryMetaArgs(),
			"session_id":   "session-1",
			"include_runs": true,
			"status":       "waiting",
			"page":         map[string]any{"page_size": 10},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if agent.getSessionCalls != 1 || agent.listRunsCalls != 1 {
		t.Fatalf("owner calls get=%d list=%d, want 1/1", agent.getSessionCalls, agent.listRunsCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "waiting") {
		t.Fatalf("structured content does not include waiting state: %s", data)
	}
}

func TestAgentToolValidationErrorIsToolError(t *testing.T) {
	t.Parallel()

	agent := newFakeAgentManagerClient()
	server := newTestServerWithAgent(t, agent)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolAgentRunRecordState,
		Arguments: map[string]any{
			"meta":   validCommandMetaArgs(),
			"run_id": "run-1",
			"status": "not-a-status",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() IsError = false, want true")
	}
	if agent.recordStateCalls != 0 {
		t.Fatalf("recordStateCalls = %d, want 0", agent.recordStateCalls)
	}
}

func TestAgentToolOwnerErrorIsSafe(t *testing.T) {
	t.Parallel()

	agent := newFakeAgentManagerClient()
	agent.err = fakeOwnerError()
	server := newTestServerWithAgent(t, agent)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolAgentSessionStart,
		Arguments: map[string]any{
			"meta":                   validCommandMetaArgs(),
			"scope":                  map[string]any{"type": "project", "ref": "project-1"},
			"provider_work_item_ref": "issue-1",
			"created_by_actor_ref":   "user:1",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() IsError = false, want true")
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if strings.Contains(string(data), "secret-token") {
		t.Fatalf("tool error exposes owner detail: %s", data)
	}
	if !strings.Contains(string(data), "owner returned Internal") {
		t.Fatalf("tool error does not include safe owner code: %s", data)
	}
}

func TestHTTPHandlerRequiresBearerToken(t *testing.T) {
	t.Parallel()

	server := newTestServerWithAuth(t)

	for _, tt := range []struct {
		name   string
		header string
	}{
		{name: "missing token"},
		{name: "wrong token", header: "Bearer wrong-token"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rr := httptest.NewRecorder()
			server.HTTPHandler().ServeHTTP(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
			}
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-token")
	rr := httptest.NewRecorder()
	server.HTTPHandler().ServeHTTP(rr, req)
	if rr.Code == http.StatusUnauthorized || rr.Code == http.StatusForbidden {
		t.Fatalf("status = %d, want auth middleware to pass request to MCP handler", rr.Code)
	}
}

type toolSnapshot struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	HasInputSchema  bool   `json:"has_input_schema"`
	HasOutputSchema bool   `json:"has_output_schema"`
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	return newTestServerWithAgent(t, newFakeAgentManagerClient())
}

func newTestServerWithAgent(t *testing.T, agentManager AgentManagerClient) *Server {
	t.Helper()

	routes, err := ownerclients.NewCatalog([]ownerclients.RouteConfig{
		{
			Service:   ownerclients.ServiceAgentManager,
			GRPCAddr:  "agent-manager:9090",
			AuthToken: "secret-token",
			Timeout:   3 * time.Second,
			Enabled:   true,
		},
		{
			Service:   ownerclients.ServiceProjectCatalog,
			GRPCAddr:  "project-catalog:9090",
			AuthToken: "secret-token",
			Timeout:   3 * time.Second,
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("NewCatalog(): %v", err)
	}
	server, err := NewServer(Config{
		ServiceName:     "platform-mcp-server",
		RegistryVersion: "mcp-2",
		ToolsPageSize:   100,
		JSONResponse:    true,
		SessionTimeout:  time.Minute,
		OwnerRoutes:     routes,
		AgentManager:    agentManager,
		AuthRequired:    false,
	}, nil)
	if err != nil {
		t.Fatalf("NewServer(): %v", err)
	}
	return server
}

func validCommandMetaArgs() map[string]any {
	return map[string]any{
		"command_id": "command-1",
		"actor": map[string]any{
			"type": "user",
			"id":   "user-1",
		},
		"reason":     "test",
		"request_id": "request-1",
		"request_context": map[string]any{
			"source": "platform-mcp-server-test",
		},
	}
}

func validQueryMetaArgs() map[string]any {
	return map[string]any{
		"actor": map[string]any{
			"type": "user",
			"id":   "user-1",
		},
		"request_id": "request-1",
		"request_context": map[string]any{
			"source": "platform-mcp-server-test",
		},
	}
}

func newTestServerWithAuth(t *testing.T) *Server {
	t.Helper()

	routes, err := ownerclients.NewCatalog([]ownerclients.RouteConfig{
		{
			Service:  ownerclients.ServiceAgentManager,
			GRPCAddr: "agent-manager:9090",
			Timeout:  3 * time.Second,
			Enabled:  true,
		},
		{
			Service:  ownerclients.ServiceProjectCatalog,
			GRPCAddr: "project-catalog:9090",
			Timeout:  3 * time.Second,
			Enabled:  true,
		},
	})
	if err != nil {
		t.Fatalf("NewCatalog(): %v", err)
	}
	server, err := NewServer(Config{
		ServiceName:     "platform-mcp-server",
		RegistryVersion: "mcp-2",
		ToolsPageSize:   100,
		JSONResponse:    true,
		SessionTimeout:  time.Minute,
		OwnerRoutes:     routes,
		AgentManager:    newFakeAgentManagerClient(),
		AuthRequired:    true,
		AuthToken:       "test-token",
		AuthScope:       "kodex.mcp",
		AuthTokenTTL:    24 * time.Hour,
	}, nil)
	if err != nil {
		t.Fatalf("NewServer(): %v", err)
	}
	return server
}

func connectClient(t *testing.T, server *Server) (*mcpsdk.ClientSession, func()) {
	t.Helper()

	ctx := context.Background()
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.mcpServer.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect(): %v", err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect(): %v", err)
	}
	return clientSession, func() {
		_ = clientSession.Close()
		_ = serverSession.Wait()
	}
}

type fakeAgentManagerClient struct {
	startSessionCalls   int
	startRunCalls       int
	recordStateCalls    int
	recordSnapshotCalls int
	getSessionCalls     int
	listRunsCalls       int
	err                 error
}

func newFakeAgentManagerClient() *fakeAgentManagerClient {
	return &fakeAgentManagerClient{}
}

func (f *fakeAgentManagerClient) StartAgentSession(_ context.Context, request *agentsv1.StartAgentSessionRequest) (*agentsv1.AgentSessionResponse, error) {
	f.startSessionCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &agentsv1.AgentSessionResponse{Session: &agentsv1.AgentSession{
		Id:                  "session-1",
		Scope:               request.GetScope(),
		ProviderWorkItemRef: request.ProviderWorkItemRef,
		FlowVersionId:       request.FlowVersionId,
		CurrentStageId:      request.CurrentStageId,
		Status:              agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_OPEN,
		CreatedByActorRef:   request.GetCreatedByActorRef(),
		Version:             1,
		CreatedAt:           "2026-05-22T00:00:00Z",
		UpdatedAt:           "2026-05-22T00:00:00Z",
	}}, nil
}

func (f *fakeAgentManagerClient) StartAgentRun(_ context.Context, request *agentsv1.StartAgentRunRequest) (*agentsv1.AgentRunResponse, error) {
	f.startRunCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &agentsv1.AgentRunResponse{Run: &agentsv1.AgentRun{
		Id:                      "run-1",
		SessionId:               request.GetSessionId(),
		FlowVersionId:           request.FlowVersionId,
		StageId:                 request.StageId,
		RoleProfileId:           request.GetRoleProfileId(),
		RoleProfileVersion:      3,
		PromptTemplateVersionId: request.GetPromptTemplateVersionId(),
		RuntimeContext:          &agentsv1.RuntimeContextRef{},
		ProviderTarget:          request.GetProviderTarget(),
		Status:                  agentsv1.AgentRunStatus_AGENT_RUN_STATUS_REQUESTED,
		Version:                 1,
		CreatedAt:               "2026-05-22T00:00:00Z",
		UpdatedAt:               "2026-05-22T00:00:00Z",
	}}, nil
}

func (f *fakeAgentManagerClient) RecordRunState(_ context.Context, request *agentsv1.RecordRunStateRequest) (*agentsv1.AgentRunResponse, error) {
	f.recordStateCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &agentsv1.AgentRunResponse{Run: &agentsv1.AgentRun{
		Id:             request.GetRunId(),
		SessionId:      "session-1",
		RoleProfileId:  "role-1",
		RuntimeContext: request.GetRuntimeContext(),
		ProviderTarget: request.GetProviderTarget(),
		Status:         request.GetStatus(),
		ResultSummary:  request.ResultSummary,
		FailureCode:    request.FailureCode,
		Version:        2,
		StartedAt:      request.StartedAt,
		FinishedAt:     request.FinishedAt,
		CreatedAt:      "2026-05-22T00:00:00Z",
		UpdatedAt:      "2026-05-22T00:01:00Z",
	}}, nil
}

func (f *fakeAgentManagerClient) RecordSessionStateSnapshot(_ context.Context, request *agentsv1.RecordSessionStateSnapshotRequest) (*agentsv1.AgentSessionStateSnapshotResponse, error) {
	f.recordSnapshotCalls++
	if f.err != nil {
		return nil, f.err
	}
	snapshotID := "snapshot-1"
	return &agentsv1.AgentSessionStateSnapshotResponse{
		Snapshot: &agentsv1.AgentSessionStateSnapshot{
			Id:           snapshotID,
			SessionId:    request.GetSessionId(),
			RunId:        request.RunId,
			SnapshotKind: request.GetSnapshotKind(),
			TurnIndex:    request.TurnIndex,
			Object:       request.GetObject(),
			CapturedAt:   request.GetCapturedAt(),
			CreatedAt:    "2026-05-22T00:01:00Z",
		},
		Session: &agentsv1.AgentSession{
			Id:                    request.GetSessionId(),
			Scope:                 &agentsv1.ScopeRef{Type: agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT, Ref: "project-1"},
			LatestStateSnapshotId: &snapshotID,
			Status:                agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_OPEN,
			CreatedByActorRef:     "user:1",
			Version:               2,
			CreatedAt:             "2026-05-22T00:00:00Z",
			UpdatedAt:             "2026-05-22T00:01:00Z",
		},
	}, nil
}

func (f *fakeAgentManagerClient) GetAgentSession(_ context.Context, request *agentsv1.GetAgentSessionRequest) (*agentsv1.AgentSessionResponse, error) {
	f.getSessionCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &agentsv1.AgentSessionResponse{Session: &agentsv1.AgentSession{
		Id:                request.GetSessionId(),
		Scope:             &agentsv1.ScopeRef{Type: agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT, Ref: "project-1"},
		Status:            agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_WAITING,
		CreatedByActorRef: "user:1",
		Version:           4,
		CreatedAt:         "2026-05-22T00:00:00Z",
		UpdatedAt:         "2026-05-22T00:10:00Z",
	}}, nil
}

func (f *fakeAgentManagerClient) ListAgentRuns(_ context.Context, _ *agentsv1.ListAgentRunsRequest) (*agentsv1.ListAgentRunsResponse, error) {
	f.listRunsCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &agentsv1.ListAgentRunsResponse{
		Runs: []*agentsv1.AgentRun{{
			Id:             "run-1",
			SessionId:      "session-1",
			RoleProfileId:  "role-1",
			RuntimeContext: &agentsv1.RuntimeContextRef{},
			ProviderTarget: &agentsv1.ProviderTargetRef{},
			Status:         agentsv1.AgentRunStatus_AGENT_RUN_STATUS_WAITING,
			Version:        2,
			CreatedAt:      "2026-05-22T00:00:00Z",
			UpdatedAt:      "2026-05-22T00:10:00Z",
		}},
		Page: &agentsv1.PageResponse{},
	}, nil
}

func fakeOwnerError() error {
	return status.Error(codes.Internal, "secret-token leaked by owner")
}
