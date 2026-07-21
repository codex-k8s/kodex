package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/integrations"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type integrationMCPStub struct {
	mu            sync.Mutex
	calls         int
	decisionCalls int
	decisionErr   error
	catalogDenied bool
	callDenied    bool
	lastKey       string
	lastToken     string
	lastDecision  integrations.ApprovalDecisionInput
}

func (stub *integrationMCPStub) DecideApproval(_ context.Context, input integrations.ApprovalDecisionInput) (integrations.ToolResult, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.decisionCalls++
	stub.lastDecision = input
	return integrations.ToolResult{Status: integrations.InvocationStatusPending, ApprovalID: input.ApprovalPublicID}, stub.decisionErr
}

func (stub *integrationMCPStub) Catalog(_ context.Context, sessionKey string, token string) ([]integrations.CatalogEntry, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if sessionKey == "allowed-session" && token == "allowed-token" && !stub.catalogDenied {
		return []integrations.CatalogEntry{{CapabilityKey: integrations.CapabilityRestartWorkload, Version: integrations.CapabilityVersion}}, nil
	}
	return nil, integrations.ErrUnauthorized
}

func (stub *integrationMCPStub) RestartWorkload(_ context.Context, sessionKey string, token string, input integrations.RestartWorkloadInput) (integrations.ToolResult, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if sessionKey != "allowed-session" || token != "allowed-token" || stub.callDenied {
		return integrations.ToolResult{}, integrations.ErrUnauthorized
	}
	stub.calls++
	stub.lastKey = input.IdempotencyKey
	stub.lastToken = token
	return integrations.ToolResult{
		Status: integrations.InvocationStatusPending, InvocationID: "inv_0123456789abcdef0123456789abcdef",
		ApprovalID: "apr_0123456789abcdef0123456789abcdef", ArgumentsHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, nil
}

func TestMCPIntegrationCatalogIsDynamicPerAuthorizedSession(t *testing.T) {
	stub := &integrationMCPStub{}
	server := httptest.NewServer(newMCPHandlerWithIntegrations(newSessionBarrierServiceOnly(), stub, defaultMCPRequestBodyBytes))
	defer server.Close()

	allowed := connectIntegrationMCP(t, server.URL, "allowed-session", "allowed-token")
	defer func() { _ = allowed.Close() }()
	tools, err := allowed.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("allowed ListTools: %v", err)
	}
	if !hasMCPTool(tools, integrations.CapabilityRestartWorkload) {
		t.Fatal("fresh grant capability отсутствует в каталоге разрешённой сессии")
	}
	result, err := allowed.CallTool(context.Background(), &mcp.CallToolParams{
		Name: integrations.CapabilityRestartWorkload,
		Arguments: map[string]any{
			"connection": "recording-main", "namespace": "mattermost", "workload_kind": "Deployment",
			"workload_name": "bot-service", "idempotency_key": "restart:test:mcp:0001",
		},
	})
	if err != nil || result.IsError {
		t.Fatalf("allowed CallTool err=%v result=%+v", err, result)
	}
	stub.mu.Lock()
	if stub.calls != 1 || stub.lastKey != "restart:test:mcp:0001" || stub.lastToken != "allowed-token" {
		t.Fatalf("fresh call binding calls=%d key=%q", stub.calls, stub.lastKey)
	}
	stub.mu.Unlock()

	stub.mu.Lock()
	stub.catalogDenied = true
	stub.callDenied = true
	stub.mu.Unlock()
	revoked := connectIntegrationMCP(t, server.URL, "allowed-session", "allowed-token")
	defer func() { _ = revoked.Close() }()
	tools, err = revoked.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("revoked ListTools: %v", err)
	}
	if hasMCPTool(tools, integrations.CapabilityRestartWorkload) {
		t.Fatal("revoked capability осталась в каталоге новой MCP-сессии")
	}
	stale, staleErr := allowed.CallTool(context.Background(), &mcp.CallToolParams{
		Name: integrations.CapabilityRestartWorkload,
		Arguments: map[string]any{
			"connection": "recording-main", "namespace": "mattermost", "workload_kind": "Deployment",
			"workload_name": "bot-service", "idempotency_key": "restart:test:mcp:stale",
		},
	})
	if staleErr == nil && stale != nil && !stale.IsError {
		t.Fatal("stale MCP tool call был принят после revoke")
	}

	denied := connectIntegrationMCP(t, server.URL, "denied-session", "denied-token")
	defer func() { _ = denied.Close() }()
	tools, err = denied.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("denied ListTools: %v", err)
	}
	if hasMCPTool(tools, integrations.CapabilityRestartWorkload) {
		t.Fatal("опасная capability попала в каталог чужой сессии")
	}
	guessed, callErr := denied.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      integrations.CapabilityRestartWorkload,
		Arguments: map[string]any{"connection": "recording-main"},
	})
	if callErr == nil && guessed != nil && !guessed.IsError {
		t.Fatal("угаданный tool call чужой сессии был принят")
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.calls != 1 {
		t.Fatalf("denied tool reached service: calls=%d", stub.calls)
	}
}

func connectIntegrationMCP(t *testing.T, baseURL string, sessionKey string, token string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "integration-catalog-test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   baseURL + "/mcp/sessions/" + sessionKey,
		HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: token}},
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("MCP connect: %v", err)
	}
	return session
}

func hasMCPTool(result *mcp.ListToolsResult, name string) bool {
	if result == nil {
		return false
	}
	for _, tool := range result.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
