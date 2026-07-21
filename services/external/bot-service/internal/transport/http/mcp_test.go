package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
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
		return
	}
	t.Fatal("MCP tool mattermost_complete_automation отсутствует")
}
