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
		{name: "absent context", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "983d6e7e41d4c31b8360567807b1704e1d6d1e2cc5bc39495a42a469ea514fdb"},
		{name: "RUN", kind: "RUN", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "481c398a0a31778bea1cfc32a7f37a0401e37bab9a0bc3e3ea04c80d1460bc64"},
		{name: "WORKFLOW", kind: "WORKFLOW", operations: []string{"UPDATE_WORKFLOW"}, reads: []string{"WORKFLOW_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "99b4759c2eeb1479fef4303e1226f63995eef2259e840c68747ab8fdf6d4c920"},
		{name: "AGENT update", kind: "AGENT", operations: []string{"UPDATE_AGENT"}, reads: []string{"AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "080496a8661ef244e674a1fb99eabf665cc3598e3ee4c8284fe2fe3e6de67b69"},
		{name: "AGENT instruction", kind: "AGENT", operations: []string{"CREATE_INSTRUCTION_DRAFT"}, reads: []string{"AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "080496a8661ef244e674a1fb99eabf665cc3598e3ee4c8284fe2fe3e6de67b69"},
		{name: "WORKFLOW revoked", kind: "WORKFLOW", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "481c398a0a31778bea1cfc32a7f37a0401e37bab9a0bc3e3ea04c80d1460bc64"},
		{name: "AGENT revoked", kind: "AGENT", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "481c398a0a31778bea1cfc32a7f37a0401e37bab9a0bc3e3ea04c80d1460bc64"},
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
			for _, kind := range []string{"WORKFLOW_CONFIGURATION", "WORKFLOW_METADATA", "AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION"} {
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
