package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestEmptyMCPCollectionOutputsUseArrays(t *testing.T) {
	tests := map[string]any{
		"thread history":  emptyMCPThreadHistory(),
		"chat search":     emptyMCPChatSearch(),
		"chat catalog":    emptyMCPChatCatalog(),
		"chat details":    emptyMCPChatDetails(),
		"delegation list": emptyMCPDelegationList(),
	}

	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			payload, err := json.Marshal(output)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if strings.Contains(string(payload), "null") {
				t.Fatalf("structured MCP output contains null collection: %s", payload)
			}
		})
	}
}

func TestMCPAutomationCallbackOutputKeepsHumanGatePending(t *testing.T) {
	output := automationCallbackMCPOutput(statusservice.AutomationCallbackResult{
		Run: entity.ScheduledRun{
			PublicID: "scheduled-run-11111111111111111111111111111111",
			Status:   string(value.AutomationRunStatusWaitingOwner),
			Outcome:  string(value.AutomationRunOutcomeRequiresHuman),
		},
		Duplicate:           true,
		OwnerAttentionID:    71,
		HumanDecisionStatus: "open",
		DeliveryStatus:      "pending",
		NextAction:          "retry_same_callback",
	})
	payload, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(payload)
	for _, expected := range []string{`"status":"waiting_owner"`, `"outcome":"requires_human"`, `"owner_attention_id":71`, `"human_decision_status":"open"`, `"delivery_status":"pending"`, `"next_action":"retry_same_callback"`} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("pending MCP output не содержит %s: %s", expected, encoded)
		}
	}
	if strings.Contains(encoded, `"status":"succeeded"`) {
		t.Fatalf("pending MCP output ложно terminal: %s", encoded)
	}
}

func TestMCPAutomationCallbackContractIsDiscoverable(t *testing.T) {
	server := httptest.NewServer(newMCPHandler(statusservice.NewAgentSessionService(statusservice.AgentSessionServiceConfig{}), defaultMCPRequestBodyBytes))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "automation-contract-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp/sessions/contract-test"}, nil)
	if err != nil {
		t.Fatalf("MCP connect: %v", err)
	}
	defer func() { _ = session.Close() }()
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error=%v", err)
	}
	for _, tool := range result.Tools {
		if tool.Name != "mattermost_complete_automation" {
			continue
		}
		schema, marshalErr := json.Marshal(tool.InputSchema)
		if marshalErr != nil {
			t.Fatalf("marshal callback schema: %v", marshalErr)
		}
		for _, field := range []string{"schedule_run_id", "callback_contract", "outcome", "summary"} {
			if !strings.Contains(string(schema), `"`+field+`"`) {
				t.Fatalf("callback schema не содержит %s: %s", field, schema)
			}
		}
		var discovered map[string]any
		if err := json.Unmarshal(schema, &discovered); err != nil {
			t.Fatalf("decode callback schema: %v", err)
		}
		properties, _ := discovered["properties"].(map[string]any)
		summary, _ := properties["summary"].(map[string]any)
		if summary["maxLength"] != float64(1000) || discovered["additionalProperties"] != false {
			t.Fatalf("callback schema не задаёт fail-closed bounds: %s", schema)
		}
		return
	}
	t.Fatal("MCP tool mattermost_complete_automation отсутствует")
}
