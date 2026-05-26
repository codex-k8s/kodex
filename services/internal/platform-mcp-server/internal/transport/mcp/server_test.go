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
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "risk-assessment-1") || !strings.Contains(string(data), "gate-policy-1") {
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
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if !strings.Contains(string(data), "risk-assessment-1") {
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

	return newTestServerWithOwners(t, newFakeAgentManagerClient(), newFakeProviderHubClient(), newFakeGovernanceManagerClient())
}

func newTestServerWithAgent(t *testing.T, agentManager AgentManagerClient) *Server {
	t.Helper()

	return newTestServerWithOwners(t, agentManager, newFakeProviderHubClient(), newFakeGovernanceManagerClient())
}

func newTestServerWithProvider(t *testing.T, provider ProviderHubClient) *Server {
	t.Helper()

	return newTestServerWithOwners(t, newFakeAgentManagerClient(), provider, newFakeGovernanceManagerClient())
}

func newTestServerWithGovernance(t *testing.T, governance GovernanceManagerClient) *Server {
	t.Helper()

	return newTestServerWithOwners(t, newFakeAgentManagerClient(), newFakeProviderHubClient(), governance)
}

func newTestServerWithOwners(t *testing.T, agentManager AgentManagerClient, provider ProviderHubClient, governance GovernanceManagerClient) *Server {
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
	lastExpectedVersion      *int64
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
	return fakeRiskAssessmentResponse(request.GetTarget()), nil
}

func (f *fakeGovernanceManagerClient) ReevaluateRisk(_ context.Context, request *governancev1.ReevaluateRiskRequest) (*governancev1.RiskAssessmentResponse, error) {
	f.reevaluateRiskCalls++
	f.lastExpectedVersion = request.GetMeta().ExpectedVersion
	if f.err != nil {
		return nil, f.err
	}
	return fakeRiskAssessmentResponse(&governancev1.TargetRef{
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
