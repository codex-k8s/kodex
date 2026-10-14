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
		{name: "absent context", reads: []string{"AGENT_RUNTIME_CONFIGURATION"}, schemaDigest: "3270906be2494aa624eee2374b406cc728c5e11c23f32fb7fffb461087abaf6f"},
		{name: "RUN", kind: "RUN", reads: []string{"AGENT_RUNTIME_CONFIGURATION"}, schemaDigest: "c6f1b0882fbba2553abad85a6217be415eab1597a5c312501ea442255177e5f1"},
		{name: "WORKFLOW", kind: "WORKFLOW", operations: []string{"UPDATE_WORKFLOW"}, reads: []string{"WORKFLOW_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION"}, schemaDigest: "81eaab470a9957812aca96a943cfed3773b637aa3d102946550ad601d97d94b0"},
		{name: "AGENT update", kind: "AGENT", operations: []string{"UPDATE_AGENT"}, reads: []string{"AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION"}, schemaDigest: "23b161d4906b6004501e8e02f5f0e1625e2c4daa6e5d6bc94b6f9b2bc7db321a"},
		{name: "AGENT instruction", kind: "AGENT", operations: []string{"CREATE_INSTRUCTION_DRAFT"}, reads: []string{"AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION"}, schemaDigest: "23b161d4906b6004501e8e02f5f0e1625e2c4daa6e5d6bc94b6f9b2bc7db321a"},
		{name: "WORKFLOW revoked", kind: "WORKFLOW", reads: []string{"AGENT_RUNTIME_CONFIGURATION"}, schemaDigest: "c6f1b0882fbba2553abad85a6217be415eab1597a5c312501ea442255177e5f1"},
		{name: "AGENT revoked", kind: "AGENT", reads: []string{"AGENT_RUNTIME_CONFIGURATION"}, schemaDigest: "c6f1b0882fbba2553abad85a6217be415eab1597a5c312501ea442255177e5f1"},
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
				t.Fatalf("catalog descriptor mismatch: actual=%s", actual)
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
