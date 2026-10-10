package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestConfigurationCatalogGuidanceMatchesContext(t *testing.T) {
	for _, test := range []struct {
		name, kind, schemaDigest string
		operations               []string
		reads                    []string
	}{
		{name: "absent context", schemaDigest: "c4211af11169b18a49c242b739ebcde9af456c8788c88fec0517de04c9ddf23b"},
		{name: "RUN", kind: "RUN", schemaDigest: "acb24d937258069daf0439b3315242c05c7dc332c6e4f4038a76dfd112915359"},
		{name: "WORKFLOW", kind: "WORKFLOW", operations: []string{"UPDATE_WORKFLOW"}, reads: []string{"WORKFLOW_CONFIGURATION"}, schemaDigest: "353a996e447a7e3b2b29a80439720142286dc70964cee5c7aefbae3318bcb31a"},
		{name: "AGENT update", kind: "AGENT", operations: []string{"UPDATE_AGENT"}, reads: []string{"AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION"}, schemaDigest: "23b161d4906b6004501e8e02f5f0e1625e2c4daa6e5d6bc94b6f9b2bc7db321a"},
		{name: "AGENT instruction", kind: "AGENT", operations: []string{"CREATE_INSTRUCTION_DRAFT"}, reads: []string{"AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION"}, schemaDigest: "23b161d4906b6004501e8e02f5f0e1625e2c4daa6e5d6bc94b6f9b2bc7db321a"},
		{name: "WORKFLOW revoked", kind: "WORKFLOW", schemaDigest: "acb24d937258069daf0439b3315242c05c7dc332c6e4f4038a76dfd112915359"},
		{name: "AGENT revoked", kind: "AGENT", schemaDigest: "acb24d937258069daf0439b3315242c05c7dc332c6e4f4038a76dfd112915359"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeProject, AgentRef: "agt_helper123", ProjectRef: "prj_project123"}
			if test.kind != "" {
				version := int64(1)
				input.AssistantContext = &runtimecontract.RunnerAssistantContext{EntityKind: test.kind, EntityRef: "ref_resource123", EntityVersion: &version, AllowedOperations: test.operations}
			}
			before, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			tool := configurationCatalogTool(input)
			description := tool["description"].(string)
			// Baseline фиксирует весь tools/list descriptor, кроме description.
			delete(tool, "description")
			encoded, err := json.Marshal(tool)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(encoded)
			actual := hex.EncodeToString(digest[:])
			if actual != test.schemaDigest {
				t.Fatal("catalog schema or non-description descriptor changed")
			}
			for _, kind := range []string{"WORKFLOW_CONFIGURATION", "AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION"} {
				wanted := false
				for _, allowed := range test.reads {
					wanted = wanted || kind == allowed
				}
				if strings.Contains(description, kind) != wanted {
					t.Fatalf("description read %s availability mismatch", kind)
				}
			}
			if (len(test.reads) != 0) != strings.Contains(description, "configuration_offset_bytes") {
				t.Fatal("description advertises unavailable pagination")
			}
			for _, fragment := range []string{"matching native resource route", "new turn", "server-owned context"} {
				if !strings.Contains(description, fragment) {
					t.Fatalf("missing closed context guidance: %s", fragment)
				}
			}
			after, err := json.Marshal(input)
			if err != nil || string(before) != string(after) {
				t.Fatal("catalog description changed its input")
			}
		})
	}
}

func TestAssistantCatalogReadGuidanceIncludesAgentRuntime(t *testing.T) {
	for _, fragment := range []string{"AGENT_RUNTIME_CONFIGURATION", "configuration_offset_bytes", "configuration_sha256", "current server-owned context", "at most once"} {
		if !strings.Contains(assistantCatalogReadInputGuidance, fragment) {
			t.Fatalf("missing read guidance: %s", fragment)
		}
	}
}
