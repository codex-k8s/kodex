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
	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
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
    "name": "agent.human_gate.get",
    "description": "Прочитать ожидание или результат Human gate через agent-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "agent.human_gate.list",
    "description": "Получить список ожиданий Human gate через agent-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "agent.human_gate.request",
    "description": "Зафиксировать ожидание решения человека через agent-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
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
  },
  {
    "name": "governance.gate.cancel",
    "description": "Cancel an open governance gate request through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.gate.expire",
    "description": "Expire an open governance gate request through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.gate.get",
    "description": "Read a safe governance gate request summary through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.gate.list",
    "description": "List safe governance gate request summaries through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.gate.request",
    "description": "Request a governance gate through governance-manager without storing decision state in MCP.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.gate.submit_decision",
    "description": "Submit a governance gate decision through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.get_decision",
    "description": "Read a safe release decision summary through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.get_decision_package",
    "description": "Read a safe release decision package summary through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.get_safety_state",
    "description": "Read release safety-loop state through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.list_blocking_signals",
    "description": "List safe release blocking signal summaries through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.list_decision_packages",
    "description": "List safe release decision package summaries through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.list_decisions",
    "description": "List safe release decision summaries through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.prepare_decision_package",
    "description": "Prepare a release decision package through governance-manager from safe refs.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.record_blocking_signal",
    "description": "Record a release blocking signal through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.record_safety_state",
    "description": "Record release safety-loop state through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.request_decision",
    "description": "Request a release decision through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.resolve_blocking_signal",
    "description": "Resolve a release blocking signal through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.release.submit_decision",
    "description": "Submit a release decision through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.risk.evaluate",
    "description": "Evaluate risk through governance-manager from safe refs and summaries.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.risk.get",
    "description": "Read a safe risk assessment summary through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.risk.list",
    "description": "List safe risk assessment summaries through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.risk.reevaluate",
    "description": "Reevaluate an existing risk assessment through governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.signal.list_review",
    "description": "Прочитать безопасные сводки review signals через governance-manager.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.signal.record_review",
    "description": "Записать review signal через governance-manager без хранения состояния в MCP.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "governance.summary.get",
    "description": "Прочитать безопасную сводку governance через governance-manager без хранения состояния в MCP.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "interaction.owner_inbox.get",
    "description": "Прочитать входящую задачу владельца через interaction-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "interaction.owner_inbox.list",
    "description": "Получить список входящих задач владельца через interaction-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "interaction.owner_inbox.respond",
    "description": "Записать ответ владельца через interaction-hub без переноса решения в MCP.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.artifact_signal.register",
    "description": "Register a provider-native artifact signal through provider-hub without raw payload input.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.comment.create",
    "description": "Create a provider-native comment through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.comment.update",
    "description": "Update a platform-owned provider-native comment through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.comments.list",
    "description": "List safe comment, mention, and review-signal summaries through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.issue.create",
    "description": "Create a provider-native Issue through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.issue.update",
    "description": "Update allowed provider-native Issue fields through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.projection.find",
    "description": "Find a safe Issue or PR/MR projection by provider-native reference through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.projection.get",
    "description": "Read a safe Issue or PR/MR projection through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.projections.list",
    "description": "List safe Issue and PR/MR projections through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.pull_request.create",
    "description": "Create a provider-native PR/MR through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.pull_request.update",
    "description": "Update allowed provider-native PR/MR fields through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.relationship.update",
    "description": "Save or update a provider-native relationship through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.relationships.list",
    "description": "List provider-native work item relationships through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.repository.adoption_pull_request.create",
    "description": "Create or update an adoption branch and PR/MR through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.repository.bootstrap_pull_request.create",
    "description": "Create or update a bootstrap branch and PR/MR through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.repository.create",
    "description": "Create a provider-native repository through provider-hub.",
    "has_input_schema": true,
    "has_output_schema": true
  },
  {
    "name": "provider.review_signal.create",
    "description": "Create a review signal, approval, or changes-request through provider-hub.",
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

func TestAgentHumanGateRequestRoutesToOwner(t *testing.T) {
	t.Parallel()

	agent := newFakeAgentManagerClient()
	server := newTestServerWithAgent(t, agent)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolAgentHumanGateRequest,
		Arguments: map[string]any{
			"meta":       validCommandMetaArgs(),
			"session_id": "session-1",
			"run_id":     "run-1",
			"stage_id":   "stage-1",
			"provider_target": map[string]any{
				"pull_request_ref": "provider:pr:1",
			},
			"target_ref":                  "provider:pr:1",
			"request_kind":                "owner_review",
			"reason_code":                 "needs_owner_decision",
			"safe_summary":                "Нужно решение владельца",
			"interaction_request_ref":     "interaction-request-1",
			"governance_gate_request_ref": "gate-request-1",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if agent.requestGateCalls != 1 {
		t.Fatalf("requestGateCalls = %d, want 1", agent.requestGateCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "human-gate-1") || strings.Contains(string(data), "raw") {
		t.Fatalf("structured content is not safe human gate summary: %s", data)
	}
}

func TestAgentHumanGateListRoutesToOwner(t *testing.T) {
	t.Parallel()

	agent := newFakeAgentManagerClient()
	server := newTestServerWithAgent(t, agent)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolAgentHumanGateList,
		Arguments: map[string]any{
			"meta":       validQueryMetaArgs(),
			"session_id": "session-1",
			"status":     "waiting",
			"page":       map[string]any{"page_size": 10},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if agent.listGatesCalls != 1 {
		t.Fatalf("listGatesCalls = %d, want 1", agent.listGatesCalls)
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

func TestProviderProjectionFindRoutesToOwner(t *testing.T) {
	t.Parallel()

	provider := newFakeProviderHubClient()
	server := newTestServerWithProvider(t, provider)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolProviderProjectionFind,
		Arguments: map[string]any{
			"meta": validProviderQueryMetaArgs(),
			"target": map[string]any{
				"provider_slug":        "github",
				"repository_full_name": "codex-k8s/kodex",
				"work_item_kind":       "issue",
				"number":               780,
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if provider.findProjectionCalls != 1 {
		t.Fatalf("findProjectionCalls = %d, want 1", provider.findProjectionCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "projection-1") || strings.Contains(string(data), "raw provider payload") {
		t.Fatalf("structured content is not safe projection summary: %s", data)
	}
}

func TestProviderIssueCreateRoutesToOwnerWithIdempotency(t *testing.T) {
	t.Parallel()

	provider := newFakeProviderHubClient()
	server := newTestServerWithProvider(t, provider)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolProviderIssueCreate,
		Arguments: map[string]any{
			"meta":                validProviderCommandMetaArgs("create_issue"),
			"project_id":          "project-1",
			"repository_id":       "repository-1",
			"provider_slug":       "github",
			"title":               "Проверить MCP provider tools",
			"body":                "Безопасное тело issue",
			"labels":              []any{"mcp"},
			"external_account_id": "external-account-1",
			"repository_target": map[string]any{
				"provider_slug":        "github",
				"repository_full_name": "codex-k8s/kodex",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if provider.createIssueCalls != 1 {
		t.Fatalf("createIssueCalls = %d, want 1", provider.createIssueCalls)
	}
	if provider.lastCommandID != "command-1" {
		t.Fatalf("lastCommandID = %q, want command-1", provider.lastCommandID)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "operation-1") {
		t.Fatalf("structured content does not contain provider operation: %s", data)
	}
	if strings.Contains(string(data), "Безопасное тело issue") {
		t.Fatalf("structured content exposes submitted body: %s", data)
	}
}

func TestProviderArtifactSignalDoesNotExposeRawPayloadInput(t *testing.T) {
	t.Parallel()

	provider := newFakeProviderHubClient()
	server := newTestServerWithProvider(t, provider)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools(): %v", err)
	}
	foundTool := false
	for _, tool := range tools.Tools {
		if tool.Name != ToolProviderArtifactSignalRegister {
			continue
		}
		foundTool = true
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("Marshal(input schema): %v", err)
		}
		if strings.Contains(string(schema), "payload_json") {
			t.Fatalf("artifact signal input schema exposes raw payload_json: %s", schema)
		}
		break
	}
	if !foundTool {
		t.Fatalf("%s tool is not registered", ToolProviderArtifactSignalRegister)
	}

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolProviderArtifactSignalRegister,
		Arguments: map[string]any{
			"meta":      validProviderCommandMetaArgs("update_issue"),
			"signal_id": "signal-1",
			"target": map[string]any{
				"provider_slug":        "github",
				"repository_full_name": "codex-k8s/kodex",
				"work_item_kind":       "issue",
				"number":               780,
			},
			"source":              "agent_manager",
			"observed_at":         "2026-05-25T00:00:00Z",
			"payload_json":        `{"raw_provider_payload":"must not be forwarded"}`,
			"external_account_id": "external-account-1",
		},
	})
	if err == nil {
		t.Fatalf("CallTool() err = nil, want schema validation error; result = %+v", result)
	}
	if provider.registerSignalCalls != 0 {
		t.Fatalf("registerSignalCalls = %d, want 0 after invalid raw payload input", provider.registerSignalCalls)
	}

	result, err = session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolProviderArtifactSignalRegister,
		Arguments: map[string]any{
			"meta":      validProviderCommandMetaArgs("update_issue"),
			"signal_id": "signal-1",
			"target": map[string]any{
				"provider_slug":        "github",
				"repository_full_name": "codex-k8s/kodex",
				"work_item_kind":       "issue",
				"number":               780,
			},
			"source":              "agent_manager",
			"observed_at":         "2026-05-25T00:00:00Z",
			"external_account_id": "external-account-1",
		},
	})
	if err != nil {
		t.Fatalf("CallTool() without raw payload: %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if provider.registerSignalCalls != 1 {
		t.Fatalf("registerSignalCalls = %d, want 1", provider.registerSignalCalls)
	}
	if provider.lastArtifactPayload != "" {
		t.Fatalf("artifact signal forwarded raw payload = %q, want empty", provider.lastArtifactPayload)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if strings.Contains(string(data), "raw_provider_payload") {
		t.Fatalf("structured content exposes raw payload: %s", data)
	}
}

func TestProviderToolValidationErrorIsToolError(t *testing.T) {
	t.Parallel()

	provider := newFakeProviderHubClient()
	server := newTestServerWithProvider(t, provider)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolProviderIssueCreate,
		Arguments: map[string]any{
			"meta":                validProviderCommandMetaArgs("not-a-provider-operation"),
			"project_id":          "project-1",
			"repository_id":       "repository-1",
			"provider_slug":       "github",
			"title":               "invalid",
			"body":                "valid body",
			"external_account_id": "external-account-1",
			"repository_target": map[string]any{
				"provider_slug":        "github",
				"repository_full_name": "codex-k8s/kodex",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() IsError = false, want true")
	}
	if provider.createIssueCalls != 0 {
		t.Fatalf("createIssueCalls = %d, want 0", provider.createIssueCalls)
	}
}

func TestProviderToolOwnerErrorIsSafe(t *testing.T) {
	t.Parallel()

	provider := newFakeProviderHubClient()
	provider.err = fakeOwnerError()
	server := newTestServerWithProvider(t, provider)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolProviderProjectionFind,
		Arguments: map[string]any{
			"meta": validProviderQueryMetaArgs(),
			"target": map[string]any{
				"provider_slug":        "github",
				"repository_full_name": "codex-k8s/kodex",
				"work_item_kind":       "issue",
				"number":               780,
			},
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

func TestInteractionOwnerInboxListRoutesToOwner(t *testing.T) {
	t.Parallel()

	interaction := newFakeInteractionHubClient()
	server := newTestServerWithInteraction(t, interaction)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolInteractionOwnerInboxList,
		Arguments: map[string]any{
			"meta": validQueryMetaArgs(),
			"scope": map[string]any{
				"type": "project",
				"ref":  "project-1",
			},
			"request_kinds":       []any{"human_gate"},
			"statuses":            []any{"waiting"},
			"source_owner_kind":   "agent_manager",
			"source_owner_ref":    "human-gate-1",
			"include_diagnostics": true,
			"page":                map[string]any{"page_size": 10},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if interaction.listInboxCalls != 1 {
		t.Fatalf("listInboxCalls = %d, want 1", interaction.listInboxCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "interaction-request-1") || strings.Contains(string(data), "raw") {
		t.Fatalf("structured content is not safe owner inbox summary: %s", data)
	}
}

func TestInteractionOwnerInboxRespondRoutesToOwner(t *testing.T) {
	t.Parallel()

	interaction := newFakeInteractionHubClient()
	server := newTestServerWithInteraction(t, interaction)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolInteractionOwnerInboxRespond,
		Arguments: map[string]any{
			"meta":                   validCommandMetaArgs(),
			"request_id":             "interaction-request-1",
			"response_action":        "approve",
			"responded_by_actor_ref": "user:1",
			"response_summary":       "approved",
			"source_kind":            "mcp",
			"source_ref":             "mcp-call-1",
			"owner_decision_ref":     "human-gate-decision-1",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if interaction.respondCalls != 1 {
		t.Fatalf("respondCalls = %d, want 1", interaction.respondCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "interaction-response-1") || strings.Contains(string(data), "secret-token") {
		t.Fatalf("structured content is not safe owner response summary: %s", data)
	}
}

func TestGovernanceRiskEvaluateRoutesToOwner(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceRiskEvaluate,
		Arguments: map[string]any{
			"meta":   validGovernanceCommandMetaArgs("evaluate_risk", nil),
			"target": map[string]any{"type": "pull_request", "ref": "provider:pr:1"},
			"project_context": map[string]any{
				"project_ref":    "project:core",
				"repository_ref": "repository:kodex",
			},
			"provider_context": map[string]any{
				"pull_request_ref":          "provider:pr:1",
				"changed_files_summary_ref": "changed-files-summary-1",
			},
			"agent_context": map[string]any{
				"session_ref": "session-1",
				"run_ref":     "run-1",
			},
			"runtime_context": map[string]any{
				"slot_ref": "slot-1",
				"job_ref":  "job-1",
			},
			"evidence_refs": []any{
				map[string]any{
					"kind":    "provider_review",
					"ref":     "provider-review-1",
					"summary": "review requested by policy",
				},
			},
			"risk_profile_ref": "risk-profile-1",
			"evaluation_summary": map[string]any{
				"changed_files_summary_ref": "changed-files-summary-1",
				"summary":                   "bounded classifier summary",
				"factors": []any{
					map[string]any{
						"source_type": "changed_file",
						"ref":         "path:services/internal/platform-mcp-server",
						"summary":     "MCP surface changed",
						"tags":        []any{"mcp", "governance"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.evaluateRiskCalls != 1 {
		t.Fatalf("evaluateRiskCalls = %d, want 1", governance.evaluateRiskCalls)
	}
	if governance.getRiskAssessmentCalls != 1 {
		t.Fatalf("getRiskAssessmentCalls = %d, want 1 enrichment read", governance.getRiskAssessmentCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "risk-assessment-1") || !strings.Contains(string(data), "gate-policy-1") || !strings.Contains(string(data), "rule:path-sensitive") {
		t.Fatalf("structured content does not include safe risk summary: %s", data)
	}
	if strings.Contains(string(data), "raw_provider_payload") || strings.Contains(string(data), "secret-token") {
		t.Fatalf("structured content exposes unsafe data: %s", data)
	}
}

func TestGovernanceRiskReevaluateRoutesToOwnerWithExpectedVersion(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	expectedVersion := int64(5)
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceRiskReevaluate,
		Arguments: map[string]any{
			"meta":                validGovernanceCommandMetaArgs("reevaluate_risk", &expectedVersion),
			"risk_assessment_id":  "risk-assessment-1",
			"reevaluation_reason": "new bounded evidence",
			"new_evidence_refs": []any{
				map[string]any{
					"kind":    "runtime_summary",
					"ref":     "runtime-summary-1",
					"summary": "post-test runtime summary",
				},
			},
			"evaluation_summary": map[string]any{
				"summary": "updated bounded classifier summary",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.reevaluateRiskCalls != 1 {
		t.Fatalf("reevaluateRiskCalls = %d, want 1", governance.reevaluateRiskCalls)
	}
	if governance.getRiskAssessmentCalls != 1 {
		t.Fatalf("getRiskAssessmentCalls = %d, want 1 enrichment read", governance.getRiskAssessmentCalls)
	}
	if governance.lastExpectedVersion == nil || *governance.lastExpectedVersion != expectedVersion {
		t.Fatalf("lastExpectedVersion = %v, want %d", governance.lastExpectedVersion, expectedVersion)
	}
}

func TestGovernanceRiskGetRoutesToOwnerWithBoundedFactors(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceRiskGet,
		Arguments: map[string]any{
			"meta":                   validGovernanceQueryMetaArgs(),
			"risk_assessment_id":     "risk-assessment-1",
			"include_factors":        true,
			"include_review_signals": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.getRiskAssessmentCalls != 1 {
		t.Fatalf("getRiskAssessmentCalls = %d, want 1", governance.getRiskAssessmentCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "matched_rule_refs") || !strings.Contains(string(data), "rule:path-sensitive") {
		t.Fatalf("structured content does not include bounded factor refs: %s", data)
	}
	if strings.Contains(string(data), "transcript") || strings.Contains(string(data), "stdout") {
		t.Fatalf("structured content exposes disallowed large text marker: %s", data)
	}
}

func TestGovernanceRiskListRequiresTargetOrProjectScope(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceRiskList,
		Arguments: map[string]any{
			"meta":                 validGovernanceQueryMetaArgs(),
			"effective_risk_class": "r2",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() IsError = false, want true")
	}
	if governance.listRiskAssessmentsCalls != 0 {
		t.Fatalf("listRiskAssessmentsCalls = %d, want 0", governance.listRiskAssessmentsCalls)
	}
}

func TestGovernanceRiskListRoutesToOwnerWithProjectScope(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceRiskList,
		Arguments: map[string]any{
			"meta": validGovernanceQueryMetaArgs(),
			"project_context": map[string]any{
				"project_ref": "project:core",
			},
			"effective_risk_class": "r2",
			"status":               "active",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.listRiskAssessmentsCalls != 1 {
		t.Fatalf("listRiskAssessmentsCalls = %d, want 1", governance.listRiskAssessmentsCalls)
	}
	if governance.getRiskAssessmentCalls != 1 {
		t.Fatalf("getRiskAssessmentCalls = %d, want 1 enrichment read", governance.getRiskAssessmentCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "risk-assessment-1") || !strings.Contains(string(data), "rule:path-sensitive") {
		t.Fatalf("structured content does not include risk assessment summary: %s", data)
	}
}

func TestGovernanceRiskOwnerErrorIsSafe(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	governance.err = fakeOwnerError()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceRiskGet,
		Arguments: map[string]any{
			"meta":               validGovernanceQueryMetaArgs(),
			"risk_assessment_id": "risk-assessment-1",
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

func TestGovernanceReviewSignalRecordRoutesToOwner(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceSignalRecordReview,
		Arguments: map[string]any{
			"meta":               validGovernanceCommandMetaArgs("record_review_signal", nil),
			"risk_assessment_id": "risk-assessment-1",
			"target":             map[string]any{"type": "pull_request", "ref": "provider:pr:1"},
			"role_kind":          "reviewer",
			"author_ref":         "agent:reviewer-1",
			"outcome":            "pass",
			"severity":           "info",
			"confidence":         "high",
			"evidence_refs": []any{map[string]any{
				"kind":    "provider_review",
				"ref":     "provider-review-1",
				"summary": "review summary",
			}},
			"summary": "review passed",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.recordReviewSignalCalls != 1 {
		t.Fatalf("recordReviewSignalCalls = %d, want 1", governance.recordReviewSignalCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "review-signal-1") || strings.Contains(string(data), "provider payload") {
		t.Fatalf("structured content is not safe review signal summary: %s", data)
	}
}

func TestGovernanceReviewSignalListRoutesToOwner(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceSignalListReview,
		Arguments: map[string]any{
			"meta":      validGovernanceQueryMetaArgs(),
			"target":    map[string]any{"type": "pull_request", "ref": "provider:pr:1"},
			"role_kind": "reviewer",
			"outcome":   "pass",
			"page":      map[string]any{"page_size": 10},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.listReviewSignalsCalls != 1 {
		t.Fatalf("listReviewSignalsCalls = %d, want 1", governance.listReviewSignalsCalls)
	}
}

func TestGovernanceSummaryGetRoutesToOwnerWithIntegrationSelector(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceSummaryGet,
		Arguments: map[string]any{
			"meta": validGovernanceQueryMetaArgs(),
			"integration_ref": map[string]any{
				"domain": "agent",
				"kind":   "acceptance",
				"ref":    "acceptance-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.getSummaryCalls != 1 {
		t.Fatalf("getSummaryCalls = %d, want 1", governance.getSummaryCalls)
	}
	if governance.lastSummaryScope.GetIntegrationRef().GetRef() != "acceptance-1" {
		t.Fatalf("lastSummaryScope integration ref = %+v, want acceptance-1", governance.lastSummaryScope.GetIntegrationRef())
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	for _, expected := range []string{"gate-request-1", "release-decision-1", "agent.acceptance", "agent_acceptance", "runtime-job-1", "pending_decisions_present", "record_gate_decision", "pending_required_gate_count", "required_gate_count"} {
		if !strings.Contains(string(data), expected) {
			t.Fatalf("structured content does not include %q: %s", expected, data)
		}
	}
	for _, forbidden := range []string{"raw_provider_payload", "transcript", "stdout", "kubeconfig", "secret-token"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("structured content exposes forbidden marker %q: %s", forbidden, data)
		}
	}
}

func TestGovernanceSummaryGetAcceptsSelfDeployPlanTarget(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceSummaryGet,
		Arguments: map[string]any{
			"meta":   validGovernanceQueryMetaArgs(),
			"target": map[string]any{"type": "self_deploy_plan", "ref": "agent:self-deploy-plan:1"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.getSummaryCalls != 1 {
		t.Fatalf("getSummaryCalls = %d, want 1", governance.getSummaryCalls)
	}
	if governance.lastSummaryScope.GetTarget().GetType() != governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN ||
		governance.lastSummaryScope.GetTarget().GetRef() != "agent:self-deploy-plan:1" {
		t.Fatalf("target scope = %+v, want self_deploy_plan", governance.lastSummaryScope.GetTarget())
	}
}

func TestGovernanceSummaryGetRejectsMixedSelectors(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceSummaryGet,
		Arguments: map[string]any{
			"meta":                        validGovernanceQueryMetaArgs(),
			"release_decision_package_id": "release-package-1",
			"release_candidate_ref":       "release-candidate:v1.2.3",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() IsError = false, want true")
	}
	if governance.getSummaryCalls != 0 {
		t.Fatalf("getSummaryCalls = %d, want 0", governance.getSummaryCalls)
	}
}

func TestGovernanceSummaryGetOwnerErrorIsSafe(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	governance.err = fakeOwnerError()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceSummaryGet,
		Arguments: map[string]any{
			"meta":                        validGovernanceQueryMetaArgs(),
			"release_decision_package_id": "release-package-1",
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

func TestGovernanceSummaryInputSchemaRejectsRawPayload(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools(): %v", err)
	}
	foundTool := false
	for _, tool := range tools.Tools {
		if tool.Name != ToolGovernanceSummaryGet {
			continue
		}
		foundTool = true
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("Marshal(input schema): %v", err)
		}
		for _, forbidden := range []string{"payload_json", "raw_provider_payload", "transcript", "kubeconfig"} {
			if strings.Contains(string(schema), forbidden) {
				t.Fatalf("summary input schema exposes forbidden field %q: %s", forbidden, schema)
			}
		}
		break
	}
	if !foundTool {
		t.Fatalf("%s tool is not registered", ToolGovernanceSummaryGet)
	}

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceSummaryGet,
		Arguments: map[string]any{
			"meta":                        validGovernanceQueryMetaArgs(),
			"release_decision_package_id": "release-package-1",
			"raw_provider_payload":        `{"secret":"must not be accepted"}`,
		},
	})
	if err == nil {
		t.Fatalf("CallTool() err = nil, want schema validation error; result = %+v", result)
	}
	if governance.getSummaryCalls != 0 {
		t.Fatalf("getSummaryCalls = %d, want 0 after invalid raw payload input", governance.getSummaryCalls)
	}
}

func TestGovernanceGateRequestRoutesToOwner(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceGateRequest,
		Arguments: map[string]any{
			"meta":               validGovernanceCommandMetaArgs("request_gate", nil),
			"risk_assessment_id": "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
			"gate_policy_id":     "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb",
			"target":             map[string]any{"type": "pull_request", "ref": "provider:pr:1"},
			"interaction_delivery_ref": map[string]any{
				"request_ref": "interaction-request-1",
			},
			"evidence_refs": []any{
				map[string]any{
					"kind":    "provider_review",
					"ref":     "provider-review-1",
					"summary": "review requested by policy",
					"digest":  "sha256:evidence",
				},
			},
			"evidence_summary": "bounded evidence summary",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.requestGateCalls != 1 {
		t.Fatalf("requestGateCalls = %d, want 1", governance.requestGateCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "gate-request-1") || strings.Contains(string(data), "raw_provider_payload") {
		t.Fatalf("structured content is not safe gate summary: %s", data)
	}
}

func TestGovernanceGateSubmitDecisionRoutesToOwnerWithExpectedVersion(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	expectedVersion := int64(3)
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceGateSubmitDecision,
		Arguments: map[string]any{
			"meta":                validGovernanceCommandMetaArgs("submit_gate_decision", &expectedVersion),
			"gate_request_id":     "gate-request-1",
			"decision_actor_ref":  "user:owner",
			"decision_policy_ref": "gate-policy:v1",
			"outcome":             "approve_with_conditions",
			"reason":              "bounded decision reason",
			"conditions_summary":  "ship after QA sign-off",
			"interaction_delivery_ref": map[string]any{
				"decision_ref": "interaction-decision-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.submitDecisionCalls != 1 {
		t.Fatalf("submitDecisionCalls = %d, want 1", governance.submitDecisionCalls)
	}
	if governance.lastExpectedVersion == nil || *governance.lastExpectedVersion != expectedVersion {
		t.Fatalf("lastExpectedVersion = %v, want %d", governance.lastExpectedVersion, expectedVersion)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "approve_with_conditions") {
		t.Fatalf("structured content does not include decision summary: %s", data)
	}
}

func TestGovernanceGateListRequiresAssessmentOrTarget(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceGateList,
		Arguments: map[string]any{
			"meta":   validGovernanceQueryMetaArgs(),
			"status": "requested",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() IsError = false, want true")
	}
	if governance.listGateRequestsCalls != 0 {
		t.Fatalf("listGateRequestsCalls = %d, want 0", governance.listGateRequestsCalls)
	}
}

func TestGovernanceGateOwnerErrorIsSafe(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	governance.err = fakeOwnerError()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceGateGet,
		Arguments: map[string]any{
			"meta":             validGovernanceQueryMetaArgs(),
			"gate_request_id":  "gate-request-1",
			"include_decision": true,
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

func TestGovernanceReleasePackagePrepareRoutesToOwner(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceReleasePrepareDecisionPackage,
		Arguments: map[string]any{
			"meta":                  validGovernanceCommandMetaArgs("prepare_release_package", nil),
			"release_candidate_ref": "release-candidate:v1.2.3",
			"project_context": map[string]any{
				"project_ref":        "project:core",
				"repository_ref":     "repository:kodex",
				"release_policy_ref": "release-policy:v1",
			},
			"repository_refs": []any{"repository:kodex"},
			"provider_refs": []any{map[string]any{
				"pull_request_ref":          "provider:pr:1",
				"changed_files_summary_ref": "changed-files-summary-1",
				"provider_operation_ref":    "provider-operation-1",
			}},
			"runtime_refs": []any{map[string]any{
				"job_ref":     "runtime-job-1",
				"summary_ref": "runtime-summary-1",
			}},
			"agent_context": map[string]any{
				"session_ref": "session-1",
				"run_ref":     "run-1",
			},
			"review_signal_ids":         []any{"review-signal-1"},
			"known_limitations_summary": "bounded limitations summary",
			"risk_assessment_id":        "risk-assessment-1",
			"evidence_refs":             []any{map[string]any{"kind": "document", "ref": "release-evidence-1", "summary": "bounded release evidence"}},
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.buildReleasePackageCalls != 1 {
		t.Fatalf("buildReleasePackageCalls = %d, want 1", governance.buildReleasePackageCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "release-package-1") || !strings.Contains(string(data), "release-policy:v1") {
		t.Fatalf("structured content does not include release package summary: %s", data)
	}
	if strings.Contains(string(data), "raw_provider_payload") || strings.Contains(string(data), "secret-token") {
		t.Fatalf("structured content exposes unsafe data: %s", data)
	}
}

func TestGovernanceReleaseDecisionSubmitRoutesToOwnerWithExpectedVersion(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	expectedVersion := int64(9)
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceReleaseSubmitDecision,
		Arguments: map[string]any{
			"meta":                        validGovernanceCommandMetaArgs("submit_release_decision", &expectedVersion),
			"release_decision_package_id": "release-package-1",
			"gate_decision_id":            "gate-decision-1",
			"outcome":                     "go_with_conditions",
			"decision_actor_ref":          "user:owner",
			"decision_policy_ref":         "release-policy:v1",
			"reason":                      "bounded release decision reason",
			"conditions_summary":          "watch postdeploy metrics",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", result.Content)
	}
	if governance.submitReleaseCalls != 1 {
		t.Fatalf("submitReleaseCalls = %d, want 1", governance.submitReleaseCalls)
	}
	if governance.lastExpectedVersion == nil || *governance.lastExpectedVersion != expectedVersion {
		t.Fatalf("lastExpectedVersion = %v, want %d", governance.lastExpectedVersion, expectedVersion)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "go_with_conditions") || strings.Contains(string(data), "transcript") {
		t.Fatalf("structured content is not bounded release decision summary: %s", data)
	}
}

func TestGovernanceReleaseDecisionListRequiresPackageOrProjectScope(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceReleaseListDecisions,
		Arguments: map[string]any{
			"meta":    validGovernanceQueryMetaArgs(),
			"outcome": "go",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(): %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() IsError = false, want true")
	}
	if governance.listReleaseCalls != 0 {
		t.Fatalf("listReleaseCalls = %d, want 0", governance.listReleaseCalls)
	}
}

func TestGovernanceBlockingSignalLifecycleRoutesToOwner(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	target := map[string]any{"type": "release_candidate", "ref": "release-candidate:v1.2.3"}
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceReleaseRecordBlockingSignal,
		Arguments: map[string]any{
			"meta":        validGovernanceCommandMetaArgs("record_blocking_signal", nil),
			"target":      target,
			"source_type": "runtime",
			"source_ref":  "runtime-summary-1",
			"severity":    "blocking",
			"summary":     "bounded blocking summary",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(record): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(record) returned tool error: %+v", result.Content)
	}

	expectedVersion := int64(2)
	result, err = session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceReleaseResolveBlockingSignal,
		Arguments: map[string]any{
			"meta":               validGovernanceCommandMetaArgs("resolve_blocking_signal", &expectedVersion),
			"blocking_signal_id": "blocking-signal-1",
			"terminal_status":    "resolved",
			"resolution_summary": "bounded resolution summary",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(resolve): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(resolve) returned tool error: %+v", result.Content)
	}
	if governance.recordBlockingCalls != 1 || governance.resolveBlockingCalls != 1 {
		t.Fatalf("blocking calls = record %d resolve %d, want 1/1", governance.recordBlockingCalls, governance.resolveBlockingCalls)
	}
	if governance.lastExpectedVersion == nil || *governance.lastExpectedVersion != expectedVersion {
		t.Fatalf("lastExpectedVersion = %v, want %d", governance.lastExpectedVersion, expectedVersion)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "resolved") || strings.Contains(string(data), "stdout") {
		t.Fatalf("structured content is not safe blocking signal summary: %s", data)
	}
}

func TestGovernanceReleaseSafetyStateRoutesToOwner(t *testing.T) {
	t.Parallel()

	governance := newFakeGovernanceManagerClient()
	server := newTestServerWithGovernance(t, governance)
	session, cleanup := connectClient(t, server)
	defer cleanup()

	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceReleaseRecordSafetyState,
		Arguments: map[string]any{
			"meta":                        validGovernanceCommandMetaArgs("record_safety_state", nil),
			"release_decision_package_id": "release-package-1",
			"current_state":               "postdeploy_observation",
			"runtime_job_ref":             "runtime-job-1",
			"last_state_reason":           "bounded safety-loop reason",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(record): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(record) returned tool error: %+v", result.Content)
	}
	result, err = session.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: ToolGovernanceReleaseGetSafetyState,
		Arguments: map[string]any{
			"meta":                        validGovernanceQueryMetaArgs(),
			"release_decision_package_id": "release-package-1",
		},
	})
	if err != nil {
		t.Fatalf("CallTool(get): %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(get) returned tool error: %+v", result.Content)
	}
	if governance.recordSafetyCalls != 1 || governance.getSafetyCalls != 1 {
		t.Fatalf("safety calls = record %d get %d, want 1/1", governance.recordSafetyCalls, governance.getSafetyCalls)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "postdeploy_observation") || strings.Contains(string(data), "kubeconfig") {
		t.Fatalf("structured content is not safe safety-loop summary: %s", data)
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

	return newTestServerWithOwners(t, newFakeAgentManagerClient(), newFakeProviderHubClient(), newFakeGovernanceManagerClient(), newFakeInteractionHubClient())
}

func newTestServerWithAgent(t *testing.T, agentManager AgentManagerClient) *Server {
	t.Helper()

	return newTestServerWithOwners(t, agentManager, newFakeProviderHubClient(), newFakeGovernanceManagerClient(), newFakeInteractionHubClient())
}

func newTestServerWithProvider(t *testing.T, provider ProviderHubClient) *Server {
	t.Helper()

	return newTestServerWithOwners(t, newFakeAgentManagerClient(), provider, newFakeGovernanceManagerClient(), newFakeInteractionHubClient())
}

func newTestServerWithGovernance(t *testing.T, governance GovernanceManagerClient) *Server {
	t.Helper()

	return newTestServerWithOwners(t, newFakeAgentManagerClient(), newFakeProviderHubClient(), governance, newFakeInteractionHubClient())
}

func newTestServerWithInteraction(t *testing.T, interaction InteractionHubClient) *Server {
	t.Helper()

	return newTestServerWithOwners(t, newFakeAgentManagerClient(), newFakeProviderHubClient(), newFakeGovernanceManagerClient(), interaction)
}

func newTestServerWithOwners(t *testing.T, agentManager AgentManagerClient, provider ProviderHubClient, governance GovernanceManagerClient, interaction InteractionHubClient) *Server {
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
			Service:   ownerclients.ServiceProviderHub,
			GRPCAddr:  "provider-hub:9090",
			AuthToken: "secret-token",
			Timeout:   3 * time.Second,
			Enabled:   true,
		},
		{
			Service:   ownerclients.ServiceGovernanceManager,
			GRPCAddr:  "governance-manager:9090",
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
		{
			Service:   ownerclients.ServiceInteractionHub,
			GRPCAddr:  "interaction-hub:9090",
			AuthToken: "secret-token",
			Timeout:   3 * time.Second,
			Enabled:   true,
		},
	})
	if err != nil {
		t.Fatalf("NewCatalog(): %v", err)
	}
	server, err := NewServer(Config{
		ServiceName:       "platform-mcp-server",
		RegistryVersion:   "mcp-2",
		ToolsPageSize:     100,
		JSONResponse:      true,
		SessionTimeout:    time.Minute,
		OwnerRoutes:       routes,
		AgentManager:      agentManager,
		ProviderHub:       provider,
		GovernanceManager: governance,
		InteractionHub:    interaction,
		AuthRequired:      false,
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

func validProviderCommandMetaArgs(operationType string) map[string]any {
	meta := validCommandMetaArgs()
	meta["operation_policy_context"] = map[string]any{
		"project_id":     "project-1",
		"repository_id":  "repository-1",
		"operation_type": operationType,
		"target_ref":     "github/codex-k8s/kodex#780",
		"changed_fields": []any{"title"},
		"risk_level":     "low",
		"policy_version": "risk-policy-1",
	}
	return meta
}

func validProviderQueryMetaArgs() map[string]any {
	return validQueryMetaArgs()
}

func validGovernanceCommandMetaArgs(commandID string, expectedVersion *int64) map[string]any {
	meta := validCommandMetaArgs()
	meta["command_id"] = commandID
	if expectedVersion != nil {
		meta["expected_version"] = *expectedVersion
	}
	return meta
}

func validGovernanceQueryMetaArgs() map[string]any {
	return validQueryMetaArgs()
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
		{
			Service:  ownerclients.ServiceProviderHub,
			GRPCAddr: "provider-hub:9090",
			Timeout:  3 * time.Second,
			Enabled:  true,
		},
		{
			Service:  ownerclients.ServiceGovernanceManager,
			GRPCAddr: "governance-manager:9090",
			Timeout:  3 * time.Second,
			Enabled:  true,
		},
		{
			Service:  ownerclients.ServiceInteractionHub,
			GRPCAddr: "interaction-hub:9090",
			Timeout:  3 * time.Second,
			Enabled:  true,
		},
	})
	if err != nil {
		t.Fatalf("NewCatalog(): %v", err)
	}
	server, err := NewServer(Config{
		ServiceName:       "platform-mcp-server",
		RegistryVersion:   "mcp-2",
		ToolsPageSize:     100,
		JSONResponse:      true,
		SessionTimeout:    time.Minute,
		OwnerRoutes:       routes,
		AgentManager:      newFakeAgentManagerClient(),
		ProviderHub:       newFakeProviderHubClient(),
		GovernanceManager: newFakeGovernanceManagerClient(),
		InteractionHub:    newFakeInteractionHubClient(),
		AuthRequired:      true,
		AuthToken:         "test-token",
		AuthScope:         "kodex.mcp",
		AuthTokenTTL:      24 * time.Hour,
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
	requestGateCalls    int
	getGateCalls        int
	listGatesCalls      int
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

func (f *fakeAgentManagerClient) RequestHumanGate(_ context.Context, request *agentsv1.RequestHumanGateRequest) (*agentsv1.HumanGateRequestResponse, error) {
	f.requestGateCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &agentsv1.HumanGateRequestResponse{HumanGateRequest: fakeHumanGateRequest(request.GetSessionId(), agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_WAITING)}, nil
}

func (f *fakeAgentManagerClient) GetHumanGateRequest(_ context.Context, request *agentsv1.GetHumanGateRequestRequest) (*agentsv1.HumanGateRequestResponse, error) {
	f.getGateCalls++
	if f.err != nil {
		return nil, f.err
	}
	gate := fakeHumanGateRequest("session-1", agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_RESOLVED)
	gate.Id = request.GetHumanGateRequestId()
	gate.Outcome = agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_APPROVE
	resolvedAt := "2026-05-22T00:20:00Z"
	gate.ResolvedAt = &resolvedAt
	return &agentsv1.HumanGateRequestResponse{HumanGateRequest: gate}, nil
}

func (f *fakeAgentManagerClient) ListHumanGateRequests(_ context.Context, _ *agentsv1.ListHumanGateRequestsRequest) (*agentsv1.ListHumanGateRequestsResponse, error) {
	f.listGatesCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &agentsv1.ListHumanGateRequestsResponse{
		HumanGateRequests: []*agentsv1.HumanGateRequest{fakeHumanGateRequest("session-1", agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_WAITING)},
		Page:              &agentsv1.PageResponse{},
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

func fakeHumanGateRequest(sessionID string, gateStatus agentsv1.HumanGateStatus) *agentsv1.HumanGateRequest {
	summary := "Нужно решение владельца"
	return &agentsv1.HumanGateRequest{
		Id:                       "human-gate-1",
		SessionId:                sessionID,
		RunId:                    optionalString("run-1"),
		StageId:                  optionalString("stage-1"),
		ProviderTarget:           &agentsv1.ProviderTargetRef{PullRequestRef: optionalString("provider:pr:1")},
		TargetRef:                optionalString("provider:pr:1"),
		RequestKind:              "owner_review",
		ReasonCode:               "needs_owner_decision",
		SafeSummary:              &summary,
		InteractionRequestRef:    optionalString("interaction-request-1"),
		GovernanceGateRequestRef: optionalString("gate-request-1"),
		Status:                   gateStatus,
		Outcome:                  agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_NONE,
		IdempotencyKey:           "human-gate-key",
		Version:                  1,
		CreatedAt:                "2026-05-22T00:10:00Z",
		UpdatedAt:                "2026-05-22T00:10:00Z",
	}
}

type fakeInteractionHubClient struct {
	listInboxCalls int
	getInboxCalls  int
	respondCalls   int
	err            error
}

func newFakeInteractionHubClient() *fakeInteractionHubClient {
	return &fakeInteractionHubClient{}
}

func (f *fakeInteractionHubClient) ListOwnerInboxItems(_ context.Context, _ *interactionsv1.ListOwnerInboxItemsRequest) (*interactionsv1.ListOwnerInboxItemsResponse, error) {
	f.listInboxCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &interactionsv1.ListOwnerInboxItemsResponse{
		Items: []*interactionsv1.OwnerInboxItem{fakeOwnerInboxItem()},
		Page:  &interactionsv1.PageResponse{},
	}, nil
}

func (f *fakeInteractionHubClient) GetOwnerInboxItem(_ context.Context, _ *interactionsv1.GetOwnerInboxItemRequest) (*interactionsv1.OwnerInboxItemResponse, error) {
	f.getInboxCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &interactionsv1.OwnerInboxItemResponse{Item: fakeOwnerInboxItem()}, nil
}

func (f *fakeInteractionHubClient) RecordInteractionResponse(_ context.Context, request *interactionsv1.RecordInteractionResponseRequest) (*interactionsv1.InteractionResponseResponse, error) {
	f.respondCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &interactionsv1.InteractionResponseResponse{
		Request:  fakeInteractionRequest(request.GetRequestId()),
		Response: fakeInteractionResponse(request),
	}, nil
}

func fakeOwnerInboxItem() *interactionsv1.OwnerInboxItem {
	return &interactionsv1.OwnerInboxItem{
		RequestId:     "interaction-request-1",
		RequestKind:   interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_HUMAN_GATE,
		RequestStatus: interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_WAITING,
		Scope:         &interactionsv1.ScopeRef{Type: interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT, Ref: "project-1"},
		Requester:     &interactionsv1.SourceOwnerRef{Kind: interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_AGENT_MANAGER, Ref: optionalString("human-gate-1")},
		DecisionOwner: &interactionsv1.DecisionOwnerRef{
			OwnerKind:       interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_AGENT_MANAGER,
			OwnerRequestRef: "human-gate-1",
		},
		AssigneeRefs: []*interactionsv1.ActorRef{{RefKind: "user", Ref: "user-1"}},
		ContextRefs:  []*interactionsv1.ExternalRef{{RefKind: "agent_run", Ref: "run-1"}},
		Title:        "Owner decision",
		Summary:      "Нужен ответ владельца",
		DeliverySummary: &interactionsv1.OwnerInboxDeliverySummary{
			AttemptCount: 1,
			LatestStatus: interactionsv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_DELIVERED,
		},
		LatestResponse: &interactionsv1.OwnerInboxResponseSummary{
			ResponseId:             "interaction-response-1",
			ResponseAction:         interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_APPROVE,
			RespondedByActorRef:    "user:1",
			SourceKind:             interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_MCP,
			ResponseSummary:        optionalString("approved"),
			InteractionResponseRef: optionalString("interaction-response-ref-1"),
			CreatedAt:              "2026-05-27T00:01:00Z",
		},
		AllowedActions: []*interactionsv1.InteractionAction{{
			ActionKey:  "approve",
			IsTerminal: true,
		}},
		CreatedAt: "2026-05-27T00:00:00Z",
		UpdatedAt: "2026-05-27T00:01:00Z",
		Version:   2,
	}
}

func fakeInteractionRequest(requestID string) *interactionsv1.InteractionRequest {
	return &interactionsv1.InteractionRequest{
		Id:          requestID,
		RequestKind: interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_HUMAN_GATE,
		Scope:       &interactionsv1.ScopeRef{Type: interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT, Ref: "project-1"},
		SourceOwner: &interactionsv1.SourceOwnerRef{Kind: interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_AGENT_MANAGER, Ref: optionalString("human-gate-1")},
		DecisionOwner: &interactionsv1.DecisionOwnerRef{
			OwnerKind:       interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_AGENT_MANAGER,
			OwnerRequestRef: "human-gate-1",
		},
		TargetRefs:    []*interactionsv1.ActorRef{{RefKind: "user", Ref: "user-1"}},
		ContextRefs:   []*interactionsv1.ExternalRef{{RefKind: "agent_run", Ref: "run-1"}},
		PromptSummary: "Нужен ответ владельца",
		Status:        interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ANSWERED,
		Version:       3,
		CreatedAt:     "2026-05-27T00:00:00Z",
		UpdatedAt:     "2026-05-27T00:02:00Z",
		ResolvedAt:    optionalString("2026-05-27T00:02:00Z"),
	}
}

func fakeInteractionResponse(request *interactionsv1.RecordInteractionResponseRequest) *interactionsv1.InteractionResponse {
	return &interactionsv1.InteractionResponse{
		Id:                  "interaction-response-1",
		RequestId:           request.GetRequestId(),
		ResponseAction:      request.GetResponseAction(),
		RespondedByActorRef: request.GetRespondedByActorRef(),
		ResponseSummary:     request.ResponseSummary,
		ResponseObject:      request.GetResponseObject(),
		SourceKind:          request.GetSourceKind(),
		SourceRef:           request.SourceRef,
		OwnerDecisionRef:    request.OwnerDecisionRef,
		CreatedAt:           "2026-05-27T00:02:00Z",
	}
}

type fakeProviderHubClient struct {
	getProjectionCalls      int
	findProjectionCalls     int
	listProjectionsCalls    int
	listCommentsCalls       int
	listRelationshipsCalls  int
	registerSignalCalls     int
	createIssueCalls        int
	updateIssueCalls        int
	createCommentCalls      int
	updateCommentCalls      int
	createPullRequestCalls  int
	updatePullRequestCalls  int
	createReviewSignalCalls int
	updateRelationshipCalls int
	createRepositoryCalls   int
	createBootstrapPRCalls  int
	createAdoptionPRCalls   int
	lastCommandID           string
	lastArtifactPayload     string
	err                     error
}

func newFakeProviderHubClient() *fakeProviderHubClient {
	return &fakeProviderHubClient{}
}

func (f *fakeProviderHubClient) GetWorkItemProjection(_ context.Context, _ *providersv1.GetWorkItemProjectionRequest) (*providersv1.WorkItemProjectionResponse, error) {
	f.getProjectionCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &providersv1.WorkItemProjectionResponse{WorkItemProjection: fakeWorkItemProjection()}, nil
}

func (f *fakeProviderHubClient) FindWorkItemByProviderRef(_ context.Context, _ *providersv1.FindWorkItemByProviderRefRequest) (*providersv1.WorkItemProjectionResponse, error) {
	f.findProjectionCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &providersv1.WorkItemProjectionResponse{WorkItemProjection: fakeWorkItemProjection()}, nil
}

func (f *fakeProviderHubClient) ListWorkItemProjections(_ context.Context, _ *providersv1.ListWorkItemProjectionsRequest) (*providersv1.ListWorkItemProjectionsResponse, error) {
	f.listProjectionsCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &providersv1.ListWorkItemProjectionsResponse{
		WorkItemProjections: []*providersv1.WorkItemProjection{fakeWorkItemProjection()},
		Page:                &providersv1.PageResponse{},
	}, nil
}

func (f *fakeProviderHubClient) ListComments(_ context.Context, _ *providersv1.ListCommentsRequest) (*providersv1.ListCommentsResponse, error) {
	f.listCommentsCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &providersv1.ListCommentsResponse{
		Comments: []*providersv1.CommentProjection{fakeCommentProjection()},
		Page:     &providersv1.PageResponse{},
	}, nil
}

func (f *fakeProviderHubClient) ListRelationships(_ context.Context, _ *providersv1.ListRelationshipsRequest) (*providersv1.ListRelationshipsResponse, error) {
	f.listRelationshipsCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &providersv1.ListRelationshipsResponse{
		Relationships: []*providersv1.ProviderRelationship{fakeProviderRelationship()},
		Page:          &providersv1.PageResponse{},
	}, nil
}

func (f *fakeProviderHubClient) RegisterProviderArtifactSignal(_ context.Context, request *providersv1.RegisterProviderArtifactSignalRequest) (*providersv1.ProviderArtifactSignalResponse, error) {
	f.registerSignalCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	f.lastArtifactPayload = request.GetPayloadJson()
	if f.err != nil {
		return nil, f.err
	}
	return &providersv1.ProviderArtifactSignalResponse{
		SignalId: request.GetSignalId(),
		Status:   "accepted",
		Target:   request.GetTarget(),
	}, nil
}

func (f *fakeProviderHubClient) CreateIssue(_ context.Context, request *providersv1.CreateIssueRequest) (*providersv1.ProviderOperationResponse, error) {
	f.createIssueCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	if f.err != nil {
		return nil, f.err
	}
	return fakeProviderOperationResponse(providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ISSUE), nil
}

func (f *fakeProviderHubClient) UpdateIssue(_ context.Context, request *providersv1.UpdateIssueRequest) (*providersv1.ProviderOperationResponse, error) {
	f.updateIssueCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	if f.err != nil {
		return nil, f.err
	}
	return fakeProviderOperationResponse(providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_ISSUE), nil
}

func (f *fakeProviderHubClient) CreateComment(_ context.Context, request *providersv1.CreateCommentRequest) (*providersv1.ProviderOperationResponse, error) {
	f.createCommentCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	if f.err != nil {
		return nil, f.err
	}
	response := fakeProviderOperationResponse(providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_COMMENT)
	response.CommentProjection = fakeCommentProjection()
	return response, nil
}

func (f *fakeProviderHubClient) UpdateComment(_ context.Context, request *providersv1.UpdateCommentRequest) (*providersv1.ProviderOperationResponse, error) {
	f.updateCommentCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	if f.err != nil {
		return nil, f.err
	}
	response := fakeProviderOperationResponse(providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_COMMENT)
	response.CommentProjection = fakeCommentProjection()
	return response, nil
}

func (f *fakeProviderHubClient) CreatePullRequest(_ context.Context, request *providersv1.CreatePullRequestRequest) (*providersv1.ProviderOperationResponse, error) {
	f.createPullRequestCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	if f.err != nil {
		return nil, f.err
	}
	return fakeProviderOperationResponse(providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_PULL_REQUEST), nil
}

func (f *fakeProviderHubClient) UpdatePullRequest(_ context.Context, request *providersv1.UpdatePullRequestRequest) (*providersv1.ProviderOperationResponse, error) {
	f.updatePullRequestCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	if f.err != nil {
		return nil, f.err
	}
	return fakeProviderOperationResponse(providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_PULL_REQUEST), nil
}

func (f *fakeProviderHubClient) CreateReviewSignal(_ context.Context, request *providersv1.CreateReviewSignalRequest) (*providersv1.ProviderOperationResponse, error) {
	f.createReviewSignalCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	if f.err != nil {
		return nil, f.err
	}
	return fakeProviderOperationResponse(providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_REVIEW_SIGNAL), nil
}

func (f *fakeProviderHubClient) UpdateRelationship(_ context.Context, request *providersv1.UpdateRelationshipRequest) (*providersv1.ProviderOperationResponse, error) {
	f.updateRelationshipCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	if f.err != nil {
		return nil, f.err
	}
	response := fakeProviderOperationResponse(providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UPDATE_RELATIONSHIP)
	response.Relationship = fakeProviderRelationship()
	return response, nil
}

func (f *fakeProviderHubClient) CreateRepository(_ context.Context, request *providersv1.CreateRepositoryRequest) (*providersv1.ProviderOperationResponse, error) {
	f.createRepositoryCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	if f.err != nil {
		return nil, f.err
	}
	return fakeProviderOperationResponse(providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_REPOSITORY), nil
}

func (f *fakeProviderHubClient) CreateBootstrapPullRequest(_ context.Context, request *providersv1.CreateBootstrapPullRequestRequest) (*providersv1.ProviderOperationResponse, error) {
	f.createBootstrapPRCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	if f.err != nil {
		return nil, f.err
	}
	return fakeProviderOperationResponse(providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_BOOTSTRAP_PULL_REQUEST), nil
}

func (f *fakeProviderHubClient) CreateAdoptionPullRequest(_ context.Context, request *providersv1.CreateAdoptionPullRequestRequest) (*providersv1.ProviderOperationResponse, error) {
	f.createAdoptionPRCalls++
	f.lastCommandID = request.GetMeta().GetCommandId()
	if f.err != nil {
		return nil, f.err
	}
	return fakeProviderOperationResponse(providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ADOPTION_PULL_REQUEST), nil
}

type fakeGovernanceManagerClient struct {
	evaluateRiskCalls        int
	reevaluateRiskCalls      int
	getRiskAssessmentCalls   int
	listRiskAssessmentsCalls int
	requestGateCalls         int
	getGateRequestCalls      int
	listGateRequestsCalls    int
	submitDecisionCalls      int
	cancelGateCalls          int
	expireGateCalls          int
	buildReleasePackageCalls int
	getReleasePackageCalls   int
	listReleasePackageCalls  int
	requestReleaseCalls      int
	submitReleaseCalls       int
	getReleaseCalls          int
	listReleaseCalls         int
	recordBlockingCalls      int
	resolveBlockingCalls     int
	listBlockingCalls        int
	recordSafetyCalls        int
	getSafetyCalls           int
	recordReviewSignalCalls  int
	listReviewSignalsCalls   int
	getSummaryCalls          int
	lastExpectedVersion      *int64
	lastSummaryScope         *governancev1.GovernanceSummaryScope
	err                      error
}

func newFakeGovernanceManagerClient() *fakeGovernanceManagerClient {
	return &fakeGovernanceManagerClient{}
}

func (f *fakeGovernanceManagerClient) EvaluateRisk(_ context.Context, request *governancev1.EvaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error) {
	f.evaluateRiskCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	return fakeRiskAssessmentOnlyResponse(request.GetTarget()), nil
}

func (f *fakeGovernanceManagerClient) ReevaluateRisk(_ context.Context, request *governancev1.ReevaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error) {
	f.reevaluateRiskCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	return fakeRiskAssessmentOnlyResponse(&governancev1.TargetRef{
		Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST,
		Ref:  "provider:pr:1",
	}), nil
}

func (f *fakeGovernanceManagerClient) GetRiskAssessment(_ context.Context, _ *governancev1.GetRiskAssessmentRequest) (*governancev1.RiskAssessmentResponse, error) {
	f.getRiskAssessmentCalls++
	if f.err != nil {
		return nil, f.err
	}
	return fakeRiskAssessmentResponse(&governancev1.TargetRef{
		Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST,
		Ref:  "provider:pr:1",
	}), nil
}

func (f *fakeGovernanceManagerClient) ListRiskAssessments(_ context.Context, _ *governancev1.ListRiskAssessmentsRequest) (*governancev1.ListRiskAssessmentsResponse, error) {
	f.listRiskAssessmentsCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ListRiskAssessmentsResponse{
		RiskAssessments: []*governancev1.RiskAssessment{fakeRiskAssessment(&governancev1.TargetRef{
			Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST,
			Ref:  "provider:pr:1",
		})},
		Page: &governancev1.PageResponse{},
	}, nil
}

func (f *fakeGovernanceManagerClient) RequestGate(_ context.Context, request *governancev1.RequestGateRequest) (*governancev1.GateRequestResponse, error) {
	f.requestGateCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.GateRequestResponse{GateRequest: fakeGateRequest(request.GetTarget())}, nil
}

func (f *fakeGovernanceManagerClient) GetGateRequest(_ context.Context, request *governancev1.GetGateRequestRequest) (*governancev1.GateRequestResponse, error) {
	f.getGateRequestCalls++
	if f.err != nil {
		return nil, f.err
	}
	response := &governancev1.GateRequestResponse{GateRequest: fakeGateRequest(&governancev1.TargetRef{
		Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST,
		Ref:  "provider:pr:1",
	})}
	if request.GetIncludeDecision() {
		response.GateDecision = fakeGateDecision()
	}
	return response, nil
}

func (f *fakeGovernanceManagerClient) ListGateRequests(_ context.Context, _ *governancev1.ListGateRequestsRequest) (*governancev1.ListGateRequestsResponse, error) {
	f.listGateRequestsCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ListGateRequestsResponse{
		GateRequests: []*governancev1.GateRequest{fakeGateRequest(&governancev1.TargetRef{
			Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST,
			Ref:  "provider:pr:1",
		})},
		Page: &governancev1.PageResponse{},
	}, nil
}

func (f *fakeGovernanceManagerClient) SubmitGateDecision(_ context.Context, request *governancev1.SubmitGateDecisionRequest) (*governancev1.GateDecisionResponse, error) {
	f.submitDecisionCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.GateDecisionResponse{
		GateDecision: fakeGateDecision(),
		GateRequest:  fakeResolvedGateRequest(),
	}, nil
}

func (f *fakeGovernanceManagerClient) CancelGate(_ context.Context, request *governancev1.CancelGateRequest) (*governancev1.GateRequestResponse, error) {
	f.cancelGateCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	gate := fakeGateRequest(&governancev1.TargetRef{
		Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST,
		Ref:  "provider:pr:1",
	})
	gate.Status = governancev1.GateRequestStatus_GATE_REQUEST_STATUS_CANCELLED
	return &governancev1.GateRequestResponse{GateRequest: gate}, nil
}

func (f *fakeGovernanceManagerClient) ExpireGate(_ context.Context, request *governancev1.ExpireGateRequest) (*governancev1.GateRequestResponse, error) {
	f.expireGateCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	gate := fakeGateRequest(&governancev1.TargetRef{
		Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST,
		Ref:  "provider:pr:1",
	})
	gate.Status = governancev1.GateRequestStatus_GATE_REQUEST_STATUS_EXPIRED
	return &governancev1.GateRequestResponse{GateRequest: gate}, nil
}

func (f *fakeGovernanceManagerClient) BuildReleaseDecisionPackage(_ context.Context, request *governancev1.BuildReleaseDecisionPackageRequest) (*governancev1.ReleaseDecisionPackageResponse, error) {
	f.buildReleasePackageCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ReleaseDecisionPackageResponse{ReleaseDecisionPackage: fakeReleaseDecisionPackage(request.GetReleaseCandidateRef())}, nil
}

func (f *fakeGovernanceManagerClient) GetReleaseDecisionPackage(_ context.Context, _ *governancev1.GetReleaseDecisionPackageRequest) (*governancev1.ReleaseDecisionPackageResponse, error) {
	f.getReleasePackageCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ReleaseDecisionPackageResponse{ReleaseDecisionPackage: fakeReleaseDecisionPackage("release-candidate:v1.2.3")}, nil
}

func (f *fakeGovernanceManagerClient) ListReleaseDecisionPackages(_ context.Context, _ *governancev1.ListReleaseDecisionPackagesRequest) (*governancev1.ListReleaseDecisionPackagesResponse, error) {
	f.listReleasePackageCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ListReleaseDecisionPackagesResponse{
		ReleaseDecisionPackages: []*governancev1.ReleaseDecisionPackage{fakeReleaseDecisionPackage("release-candidate:v1.2.3")},
		Page:                    &governancev1.PageResponse{},
	}, nil
}

func (f *fakeGovernanceManagerClient) RequestReleaseDecision(_ context.Context, request *governancev1.RequestReleaseDecisionRequest) (*governancev1.ReleaseDecisionResponse, error) {
	f.requestReleaseCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	decision := fakeReleaseDecision(request.GetReleaseDecisionPackageId())
	decision.Status = governancev1.ReleaseDecisionStatus_RELEASE_DECISION_STATUS_REQUESTED
	return &governancev1.ReleaseDecisionResponse{
		ReleaseDecision:        decision,
		ReleaseDecisionPackage: fakeReleaseDecisionPackage("release-candidate:v1.2.3"),
	}, nil
}

func (f *fakeGovernanceManagerClient) SubmitReleaseDecision(_ context.Context, request *governancev1.SubmitReleaseDecisionRequest) (*governancev1.ReleaseDecisionResponse, error) {
	f.submitReleaseCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ReleaseDecisionResponse{
		ReleaseDecision:        fakeReleaseDecision(request.GetReleaseDecisionPackageId()),
		ReleaseDecisionPackage: fakeReleaseDecisionPackage("release-candidate:v1.2.3"),
	}, nil
}

func (f *fakeGovernanceManagerClient) GetReleaseDecision(_ context.Context, _ *governancev1.GetReleaseDecisionRequest) (*governancev1.ReleaseDecisionResponse, error) {
	f.getReleaseCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ReleaseDecisionResponse{
		ReleaseDecision:        fakeReleaseDecision("release-package-1"),
		ReleaseDecisionPackage: fakeReleaseDecisionPackage("release-candidate:v1.2.3"),
	}, nil
}

func (f *fakeGovernanceManagerClient) ListReleaseDecisions(_ context.Context, _ *governancev1.ListReleaseDecisionsRequest) (*governancev1.ListReleaseDecisionsResponse, error) {
	f.listReleaseCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ListReleaseDecisionsResponse{
		ReleaseDecisions: []*governancev1.ReleaseDecision{fakeReleaseDecision("release-package-1")},
		Page:             &governancev1.PageResponse{},
	}, nil
}

func (f *fakeGovernanceManagerClient) RecordBlockingSignal(_ context.Context, request *governancev1.RecordBlockingSignalRequest) (*governancev1.BlockingSignalResponse, error) {
	f.recordBlockingCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.BlockingSignalResponse{BlockingSignal: fakeBlockingSignal(request.GetTarget())}, nil
}

func (f *fakeGovernanceManagerClient) ResolveBlockingSignal(_ context.Context, request *governancev1.ResolveBlockingSignalRequest) (*governancev1.BlockingSignalResponse, error) {
	f.resolveBlockingCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	signal := fakeBlockingSignal(&governancev1.TargetRef{
		Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RELEASE_CANDIDATE,
		Ref:  "release-candidate:v1.2.3",
	})
	signal.Status = request.GetTerminalStatus()
	signal.ResolvedAt = stringPtr("2026-05-26T00:10:00Z")
	return &governancev1.BlockingSignalResponse{BlockingSignal: signal}, nil
}

func (f *fakeGovernanceManagerClient) ListBlockingSignals(_ context.Context, _ *governancev1.ListBlockingSignalsRequest) (*governancev1.ListBlockingSignalsResponse, error) {
	f.listBlockingCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ListBlockingSignalsResponse{
		BlockingSignals: []*governancev1.BlockingSignal{fakeBlockingSignal(&governancev1.TargetRef{
			Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_RELEASE_CANDIDATE,
			Ref:  "release-candidate:v1.2.3",
		})},
		Page: &governancev1.PageResponse{},
	}, nil
}

func (f *fakeGovernanceManagerClient) RecordReleaseSafetyState(_ context.Context, request *governancev1.RecordReleaseSafetyStateRequest) (*governancev1.ReleaseSafetyStateResponse, error) {
	f.recordSafetyCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	state := fakeReleaseSafetyState(request.GetReleaseDecisionPackageId())
	state.CurrentState = request.GetCurrentState()
	return &governancev1.ReleaseSafetyStateResponse{ReleaseSafetyState: state}, nil
}

func (f *fakeGovernanceManagerClient) GetReleaseSafetyState(_ context.Context, _ *governancev1.GetReleaseSafetyStateRequest) (*governancev1.ReleaseSafetyStateResponse, error) {
	f.getSafetyCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ReleaseSafetyStateResponse{ReleaseSafetyState: fakeReleaseSafetyState("release-package-1")}, nil
}

func (f *fakeGovernanceManagerClient) RecordReviewSignal(_ context.Context, request *governancev1.RecordReviewSignalRequest) (*governancev1.ReviewSignalResponse, error) {
	f.recordReviewSignalCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ReviewSignalResponse{ReviewSignal: fakeReviewSignal(request.GetTarget())}, nil
}

func (f *fakeGovernanceManagerClient) ListReviewSignals(_ context.Context, _ *governancev1.ListReviewSignalsRequest) (*governancev1.ListReviewSignalsResponse, error) {
	f.listReviewSignalsCalls++
	if f.err != nil {
		return nil, f.err
	}
	return &governancev1.ListReviewSignalsResponse{
		ReviewSignals: []*governancev1.ReviewSignal{fakeReviewSignal(&governancev1.TargetRef{
			Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST,
			Ref:  "provider:pr:1",
		})},
		Page: &governancev1.PageResponse{},
	}, nil
}

func (f *fakeGovernanceManagerClient) GetGovernanceSummary(_ context.Context, request *governancev1.GetGovernanceSummaryRequest) (*governancev1.GovernanceSummaryResponse, error) {
	f.getSummaryCalls++
	f.lastSummaryScope = request.GetScope()
	if f.err != nil {
		return nil, f.err
	}
	return fakeGovernanceSummaryResponse(request.GetScope()), nil
}

func fakeGovernanceSummaryResponse(scope *governancev1.GovernanceSummaryScope) *governancev1.GovernanceSummaryResponse {
	return &governancev1.GovernanceSummaryResponse{Summary: &governancev1.GovernanceSummary{
		Scope: scope,
		Status: &governancev1.GovernanceSummaryStatus{
			Attention:                 governancev1.GovernanceDecisionAttention_GOVERNANCE_DECISION_ATTENTION_PENDING,
			MaxRiskClass:              governancev1.RiskClass_RISK_CLASS_R2,
			PendingDecisionCount:      1,
			BlockedDecisionCount:      0,
			CompletedDecisionCount:    1,
			PendingGateCount:          1,
			PendingRequiredGateCount:  1,
			ActiveBlockingSignalCount: 0,
			EvidenceCount:             2,
			DiagnosticCount:           1,
			SummaryCode:               "pending_decisions_present",
			NextActionCode:            "record_gate_decision",
		},
		PendingDecisions: []*governancev1.GovernanceDecisionSummary{{
			Kind:                     governancev1.GovernanceDecisionSummaryKind_GOVERNANCE_DECISION_SUMMARY_KIND_GATE_REQUEST,
			Attention:                governancev1.GovernanceDecisionAttention_GOVERNANCE_DECISION_ATTENTION_PENDING,
			Id:                       "gate-request-1",
			Target:                   &governancev1.TargetRef{Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST, Ref: "provider:pr:1"},
			ReleaseDecisionPackageId: stringPtr("release-package-1"),
			GateRequestStatus:        governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION,
			Severity:                 governancev1.SignalSeverity_SIGNAL_SEVERITY_WARNING,
			RequiredGateCount:        1,
			SafeSummary:              "Нужно решение владельца по release gate",
			EvidenceRefs: []*governancev1.EvidenceRef{{
				Kind:    governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_REVIEW,
				Ref:     "provider-review-1",
				Summary: "bounded review summary",
			}, {
				Kind:    governancev1.EvidenceKind_EVIDENCE_KIND_AGENT_ACCEPTANCE,
				Ref:     "agent:acceptance/acceptance-1",
				Summary: "bounded agent acceptance ref",
			}},
			IntegrationRefs: []*governancev1.ReleaseIntegrationRef{{
				Domain:  "agent",
				Kind:    "acceptance",
				Ref:     "acceptance-1",
				Status:  stringPtr("passed"),
				Summary: stringPtr("bounded acceptance summary"),
			}},
			AgentContext: &governancev1.AgentContextRef{SessionRef: stringPtr("session-1"), RunRef: stringPtr("run-1"), AcceptanceRef: stringPtr("acceptance-1")},
			Version:      3,
			CreatedAt:    "2026-05-29T00:00:00Z",
			UpdatedAt:    "2026-05-29T00:01:00Z",
			ObservedAt:   stringPtr("2026-05-29T00:01:00Z"),
		}},
		CompletedDecisions: []*governancev1.GovernanceDecisionSummary{{
			Kind:                     governancev1.GovernanceDecisionSummaryKind_GOVERNANCE_DECISION_SUMMARY_KIND_RELEASE_DECISION,
			Attention:                governancev1.GovernanceDecisionAttention_GOVERNANCE_DECISION_ATTENTION_COMPLETED,
			Id:                       "release-decision-1",
			ReleaseDecisionPackageId: stringPtr("release-package-1"),
			ReleaseDecisionStatus:    governancev1.ReleaseDecisionStatus_RELEASE_DECISION_STATUS_RESOLVED,
			ReleaseDecisionOutcome:   governancev1.ReleaseDecisionOutcome_RELEASE_DECISION_OUTCOME_GO_WITH_CONDITIONS,
			SafeSummary:              "Релиз разрешён с условиями",
			Version:                  4,
			UpdatedAt:                "2026-05-29T00:03:00Z",
		}},
		EvidenceSummaries: []*governancev1.GovernanceEvidenceSummary{{
			SourceKind:  "agent.acceptance",
			SourceRef:   "acceptance-1",
			Status:      stringPtr("passed"),
			Outcome:     stringPtr("accepted"),
			SafeSummary: "bounded agent acceptance summary",
			Digest:      stringPtr("sha256:acceptance"),
			ObservedAt:  stringPtr("2026-05-29T00:02:00Z"),
			Version:     stringPtr("7"),
			IntegrationRefs: []*governancev1.ReleaseIntegrationRef{{
				Domain:  "runtime",
				Kind:    "job",
				Ref:     "runtime-job-1",
				Status:  stringPtr("succeeded"),
				Summary: stringPtr("bounded runtime summary"),
			}},
		}},
		Diagnostics: []string{"partial: provider projection is not loaded by owner domain"},
	}}
}

func fakeRiskAssessmentResponse(target *governancev1.TargetRef) *governancev1.RiskAssessmentResponse {
	return &governancev1.RiskAssessmentResponse{
		RiskAssessment: fakeRiskAssessment(target),
		RiskFactors: []*governancev1.RiskFactor{
			{
				Id:               "risk-factor-1",
				RiskAssessmentId: "risk-assessment-1",
				SourceType:       governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_POLICY,
				SourceRef:        stringPtr("rule:path-sensitive"),
				RiskClass:        governancev1.RiskClass_RISK_CLASS_R2,
				Summary:          "path requires careful review",
				CreatedAt:        "2026-05-25T00:00:00Z",
			},
		},
		ReviewSignals: []*governancev1.ReviewSignal{{Id: "review-signal-1"}},
	}
}

func fakeReviewSignal(target *governancev1.TargetRef) *governancev1.ReviewSignal {
	return &governancev1.ReviewSignal{
		Id:               "review-signal-1",
		RiskAssessmentId: stringPtr("risk-assessment-1"),
		Target:           target,
		RoleKind:         governancev1.ReviewRoleKind_REVIEW_ROLE_KIND_REVIEWER,
		AuthorRef:        "agent:reviewer-1",
		Outcome:          governancev1.ReviewSignalOutcome_REVIEW_SIGNAL_OUTCOME_PASS,
		Severity:         governancev1.SignalSeverity_SIGNAL_SEVERITY_INFO,
		Confidence:       governancev1.Confidence_CONFIDENCE_HIGH.Enum(),
		EvidenceRefs: []*governancev1.EvidenceRef{{
			Kind:    governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_REVIEW,
			Ref:     "provider-review-1",
			Summary: "review summary",
		}},
		Summary:   "review passed",
		CreatedAt: "2026-05-26T00:00:00Z",
	}
}

func fakeRiskAssessmentOnlyResponse(target *governancev1.TargetRef) *governancev1.RiskAssessmentResponse {
	return &governancev1.RiskAssessmentResponse{RiskAssessment: fakeRiskAssessment(target)}
}

func fakeRiskAssessment(target *governancev1.TargetRef) *governancev1.RiskAssessment {
	return &governancev1.RiskAssessment{
		Id:     "risk-assessment-1",
		Target: target,
		ProjectContext: &governancev1.ProjectContextRef{
			ProjectRef:    stringPtr("project:core"),
			RepositoryRef: stringPtr("repository:kodex"),
		},
		ProviderContext: &governancev1.ProviderContextRef{
			PullRequestRef:         stringPtr("provider:pr:1"),
			ChangedFilesSummaryRef: stringPtr("changed-files-summary-1"),
		},
		AgentContext: &governancev1.AgentContextRef{
			SessionRef: stringPtr("session-1"),
			RunRef:     stringPtr("run-1"),
		},
		RuntimeContext: &governancev1.RuntimeContextRef{
			SlotRef: stringPtr("slot-1"),
			JobRef:  stringPtr("job-1"),
		},
		InitialRiskClass:   governancev1.RiskClass_RISK_CLASS_R1,
		EffectiveRiskClass: governancev1.RiskClass_RISK_CLASS_R2,
		Status:             governancev1.RiskAssessmentStatus_RISK_ASSESSMENT_STATUS_ACTIVE,
		Explanation:        "bounded risk summary",
		RequiredGates: []*governancev1.RequiredGate{{
			GatePolicyId: "gate-policy-1",
			GateKind:     governancev1.GateKind_GATE_KIND_QA,
			MinRiskClass: governancev1.RiskClass_RISK_CLASS_R2,
			Reason:       "qa gate required",
		}},
		Version:            2,
		CreatedAt:          "2026-05-25T00:00:00Z",
		UpdatedAt:          "2026-05-25T00:01:00Z",
		RiskProfileId:      stringPtr("risk-profile-1"),
		RiskProfileVersion: int64Ptr(3),
		EvaluationSummary: &governancev1.RiskEvaluationSummary{
			ChangedFilesSummaryRef: stringPtr("changed-files-summary-1"),
			Summary:                "bounded classifier summary",
			Factors: []*governancev1.RiskEvaluationFactor{{
				SourceType: governancev1.RiskFactorSourceType_RISK_FACTOR_SOURCE_TYPE_CHANGED_FILE,
				Ref:        "path:services/internal/platform-mcp-server",
				Summary:    "MCP surface changed",
				Tags:       []string{"mcp", "governance"},
			}},
		},
		EvidenceRefs: []*governancev1.EvidenceRef{{
			Kind:    governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_REVIEW,
			Ref:     "provider-review-1",
			Summary: "review requested by policy",
		}},
	}
}

func fakeGateRequest(target *governancev1.TargetRef) *governancev1.GateRequest {
	return &governancev1.GateRequest{
		Id:               "gate-request-1",
		RiskAssessmentId: stringPtr("risk-assessment-1"),
		GatePolicyId:     stringPtr("gate-policy-1"),
		Target:           target,
		InteractionDeliveryRef: &governancev1.InteractionDeliveryRef{
			RequestRef: stringPtr("interaction-request-1"),
		},
		EvidenceRefs: []*governancev1.EvidenceRef{{
			Kind:    governancev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_REVIEW,
			Ref:     "provider-review-1",
			Summary: "review requested by policy",
			Digest:  stringPtr("sha256:evidence"),
		}},
		EvidenceSummary: "bounded evidence summary",
		Status:          governancev1.GateRequestStatus_GATE_REQUEST_STATUS_REQUESTED,
		Version:         1,
		CreatedAt:       "2026-05-25T00:00:00Z",
		UpdatedAt:       "2026-05-25T00:00:00Z",
	}
}

func fakeResolvedGateRequest() *governancev1.GateRequest {
	gate := fakeGateRequest(&governancev1.TargetRef{
		Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_PULL_REQUEST,
		Ref:  "provider:pr:1",
	})
	gate.Status = governancev1.GateRequestStatus_GATE_REQUEST_STATUS_RESOLVED
	gate.Version = 4
	gate.UpdatedAt = "2026-05-25T00:01:00Z"
	return gate
}

func fakeGateDecision() *governancev1.GateDecision {
	return &governancev1.GateDecision{
		Id:                "gate-decision-1",
		GateRequestId:     "gate-request-1",
		DecisionActorRef:  "user:owner",
		DecisionPolicyRef: "gate-policy:v1",
		Outcome:           governancev1.GateOutcome_GATE_OUTCOME_APPROVE_WITH_CONDITIONS,
		Reason:            "bounded decision reason",
		ConditionsSummary: stringPtr("ship after QA sign-off"),
		SourceRef:         stringPtr("interaction-decision-1"),
		DecidedAt:         "2026-05-25T00:01:00Z",
	}
}

func fakeReleaseDecisionPackage(candidateRef string) *governancev1.ReleaseDecisionPackage {
	return &governancev1.ReleaseDecisionPackage{
		Id:                  "release-package-1",
		ReleaseCandidateRef: candidateRef,
		ProjectContext: &governancev1.ProjectContextRef{
			ProjectRef:       stringPtr("project:core"),
			RepositoryRef:    stringPtr("repository:kodex"),
			ReleasePolicyRef: stringPtr("release-policy:v1"),
		},
		RepositoryRefs:   []string{"repository:kodex"},
		RiskAssessmentId: stringPtr("risk-assessment-1"),
		ProviderRefs: []*governancev1.ProviderContextRef{{
			PullRequestRef:         stringPtr("provider:pr:1"),
			ChangedFilesSummaryRef: stringPtr("changed-files-summary-1"),
		}},
		RuntimeRefs: []*governancev1.RuntimeContextRef{{
			JobRef:      stringPtr("runtime-job-1"),
			SummaryRef:  stringPtr("runtime-summary-1"),
			ArtifactRef: stringPtr("release-artifact-1"),
		}},
		AgentContext: &governancev1.AgentContextRef{
			SessionRef: stringPtr("session-1"),
			RunRef:     stringPtr("run-1"),
		},
		ReviewSignalIds:         []string{"review-signal-1"},
		EvidenceRefs:            []*governancev1.EvidenceRef{{Kind: governancev1.EvidenceKind_EVIDENCE_KIND_DOCUMENT, Ref: "release-evidence-1", Summary: "bounded release evidence"}},
		KnownLimitationsSummary: "bounded limitations summary",
		Status:                  governancev1.ReleaseDecisionPackageStatus_RELEASE_DECISION_PACKAGE_STATUS_READY,
		Version:                 7,
		CreatedAt:               "2026-05-26T00:00:00Z",
		UpdatedAt:               "2026-05-26T00:01:00Z",
	}
}

func fakeReleaseDecision(packageID string) *governancev1.ReleaseDecision {
	return &governancev1.ReleaseDecision{
		Id:                       "release-decision-1",
		ReleaseDecisionPackageId: packageID,
		GateDecisionId:           stringPtr("gate-decision-1"),
		Outcome:                  governancev1.ReleaseDecisionOutcome_RELEASE_DECISION_OUTCOME_GO_WITH_CONDITIONS,
		DecisionActorRef:         "user:owner",
		DecisionPolicyRef:        "release-policy:v1",
		Reason:                   "bounded release decision reason",
		ConditionsSummary:        stringPtr("watch postdeploy metrics"),
		Status:                   governancev1.ReleaseDecisionStatus_RELEASE_DECISION_STATUS_RESOLVED,
		Version:                  3,
		DecidedAt:                "2026-05-26T00:05:00Z",
	}
}

func fakeBlockingSignal(target *governancev1.TargetRef) *governancev1.BlockingSignal {
	return &governancev1.BlockingSignal{
		Id:         "blocking-signal-1",
		Target:     target,
		SourceType: governancev1.BlockingSignalSourceType_BLOCKING_SIGNAL_SOURCE_TYPE_RUNTIME,
		SourceRef:  stringPtr("runtime-summary-1"),
		Severity:   governancev1.SignalSeverity_SIGNAL_SEVERITY_BLOCKING,
		Summary:    "bounded blocking summary",
		Status:     governancev1.BlockingSignalStatus_BLOCKING_SIGNAL_STATUS_ACTIVE,
		Version:    2,
		CreatedAt:  "2026-05-26T00:02:00Z",
	}
}

func fakeReleaseSafetyState(packageID string) *governancev1.ReleaseSafetyState {
	return &governancev1.ReleaseSafetyState{
		Id:                       "release-safety-state-1",
		ReleaseDecisionPackageId: packageID,
		CurrentState:             governancev1.ReleaseSafetyStateKind_RELEASE_SAFETY_STATE_KIND_POSTDEPLOY_OBSERVATION,
		RuntimeJobRef:            stringPtr("runtime-job-1"),
		BlockingSignalCount:      1,
		LastStateReason:          "bounded safety-loop reason",
		Version:                  4,
		CreatedAt:                "2026-05-26T00:06:00Z",
		UpdatedAt:                "2026-05-26T00:07:00Z",
	}
}

func fakeWorkItemProjection() *providersv1.WorkItemProjection {
	return &providersv1.WorkItemProjection{
		WorkItemProjectionId: "projection-1",
		ProviderSlug:         "github",
		ProviderWorkItemId:   "provider-work-item-1",
		ProjectId:            stringPtr("project-1"),
		RepositoryId:         stringPtr("repository-1"),
		RepositoryFullName:   "codex-k8s/kodex",
		Kind:                 providersv1.WorkItemKind_WORK_ITEM_KIND_ISSUE,
		Number:               780,
		WebUrl:               "https://github.com/codex-k8s/kodex/issues/780",
		Title:                "MCP-4",
		State:                "open",
		WorkItemType:         stringPtr("task"),
		Labels:               []string{"mcp"},
		WatermarkStatus:      providersv1.WorkItemWatermarkStatus_WORK_ITEM_WATERMARK_STATUS_VALID,
		BodyDigest:           "sha256:body",
		SyncedAt:             "2026-05-25T00:00:00Z",
		DriftStatus:          providersv1.WorkItemDriftStatus_WORK_ITEM_DRIFT_STATUS_FRESH,
		Version:              1,
	}
}

func fakeCommentProjection() *providersv1.CommentProjection {
	return &providersv1.CommentProjection{
		CommentProjectionId:  "comment-projection-1",
		WorkItemProjectionId: "projection-1",
		ProviderCommentId:    "provider-comment-1",
		Kind:                 providersv1.CommentKind_COMMENT_KIND_COMMENT,
		AuthorProviderLogin:  "kodex-agent",
		BodyDigest:           "sha256:comment",
		Summary:              "Короткая безопасная сводка",
		ProviderCreatedAt:    stringPtr("2026-05-25T00:00:00Z"),
		ProviderUpdatedAt:    stringPtr("2026-05-25T00:00:00Z"),
		ReviewState:          providersv1.ReviewState_REVIEW_STATE_COMMENTED,
	}
}

func fakeProviderRelationship() *providersv1.ProviderRelationship {
	return &providersv1.ProviderRelationship{
		RelationshipId:             "relationship-1",
		SourceWorkItemProjectionId: "projection-1",
		TargetProviderRef:          stringPtr("https://github.com/codex-k8s/kodex/pull/1"),
		RelationshipType:           "tracks",
		Source:                     providersv1.RelationshipSource_RELATIONSHIP_SOURCE_MANUAL,
		Confidence:                 providersv1.RelationshipConfidence_RELATIONSHIP_CONFIDENCE_CONFIRMED,
		CreatedAt:                  "2026-05-25T00:00:00Z",
		Version:                    1,
	}
}

func fakeProviderOperationResponse(operationType providersv1.ProviderOperationType) *providersv1.ProviderOperationResponse {
	return &providersv1.ProviderOperationResponse{
		ProviderOperation: &providersv1.ProviderOperation{
			ProviderOperationId: "operation-1",
			CommandId:           "command-1",
			ActorId:             stringPtr("user-1"),
			ExternalAccountId:   "external-account-1",
			ProviderSlug:        "github",
			OperationType:       operationType,
			TargetRef:           "github/codex-k8s/kodex#780",
			Status:              providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_SUCCEEDED,
			ResultRef:           stringPtr("https://github.com/codex-k8s/kodex/issues/780"),
			StartedAt:           "2026-05-25T00:00:00Z",
			FinishedAt:          stringPtr("2026-05-25T00:00:01Z"),
		},
		WorkItemProjection: fakeWorkItemProjection(),
		Result: &providersv1.ProviderOperationCommandResult{
			Target: &providersv1.ProviderTarget{
				ProviderSlug:       "github",
				RepositoryFullName: stringPtr("codex-k8s/kodex"),
				WorkItemKind:       workItemKindPtr(providersv1.WorkItemKind_WORK_ITEM_KIND_ISSUE),
				Number:             int64Ptr(780),
				WebUrl:             stringPtr("https://github.com/codex-k8s/kodex/issues/780"),
			},
			ResultRef:              stringPtr("https://github.com/codex-k8s/kodex/issues/780"),
			ProviderObjectId:       stringPtr("provider-work-item-1"),
			ProviderVersion:        stringPtr("provider-version-1"),
			ReconciliationEnqueued: true,
			EmittedEventTypes:      []string{"provider.work_item.created"},
		},
	}
}

func stringPtr(value string) *string {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func workItemKindPtr(value providersv1.WorkItemKind) *providersv1.WorkItemKind {
	return &value
}

func fakeOwnerError() error {
	return status.Error(codes.Internal, "secret-token leaked by owner")
}
