package http

import (
	"encoding/json"
	"strings"
	"testing"
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
