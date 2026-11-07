package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestConfigurationCatalogGuidanceMatchesContext checks seven context/capability fixtures against exact tool-descriptor digests excluding description, available read guidance and immutable input.
// TestConfigurationCatalogGuidanceMatchesContext проверяет семь конфигураций контекста и операций по точным дайджестам описания инструмента без description, доступным подсказкам чтения и неизменности входа.
func TestConfigurationCatalogGuidanceMatchesContext(t *testing.T) {
	for _, test := range []struct {
		name, kind, schemaDigest string
		operations               []string
		reads                    []string
	}{
		{name: "absent context", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "4798c89a63028ee3d8f2f45911d844bd0fb8441a52a4af691a18d55a44bbcb13"},
		{name: "RUN", kind: "RUN", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "1de378f92cbcb33b532a94d1cfa14b5c03e549ab75d2c71a2f32d3bce1c3a168"},
		{name: "WORKFLOW", kind: "WORKFLOW", operations: []string{"UPDATE_WORKFLOW"}, reads: []string{"WORKFLOW_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "972ffbb80c92b78c3e9a9c0ee37ed00dde28210a12c821d0c6c045e99daa3a9f"},
		{name: "AGENT update", kind: "AGENT", operations: []string{"UPDATE_AGENT"}, reads: []string{"AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "3eb1feab8659b175f53f04af4942929286f0638cf4cc376a3bab83a893aa1a8c"},
		{name: "AGENT instruction", kind: "AGENT", operations: []string{"CREATE_INSTRUCTION_DRAFT"}, reads: []string{"AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "3eb1feab8659b175f53f04af4942929286f0638cf4cc376a3bab83a893aa1a8c"},
		{name: "WORKFLOW revoked", kind: "WORKFLOW", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "1de378f92cbcb33b532a94d1cfa14b5c03e549ab75d2c71a2f32d3bce1c3a168"},
		{name: "AGENT revoked", kind: "AGENT", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "1de378f92cbcb33b532a94d1cfa14b5c03e549ab75d2c71a2f32d3bce1c3a168"},
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
			if !strings.Contains(string(encoded), "PREPARE_AGENT_RUNTIME_CONFIGURATION") {
				t.Fatal("project catalog lost the ordinary employee operation")
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

// TestAssistantCatalogReadGuidanceIncludesAgentRuntime checks recovery guidance mentions runtime metadata, byte/digest pagination, server-owned context and the bounded retry rule.
// TestAssistantCatalogReadGuidanceIncludesAgentRuntime проверяет упоминание метаданных выполнения, байтовых страниц и дайджеста, серверного контекста и ограниченного повтора в подсказке восстановления.
func TestAssistantCatalogReadGuidanceIncludesAgentRuntime(t *testing.T) {
	for _, fragment := range []string{"AGENT_RUNTIME_CONFIGURATION", "configuration_offset_bytes", "configuration_sha256", "current server-owned context", "at most once"} {
		if !strings.Contains(assistantCatalogReadInputGuidance, fragment) {
			t.Fatalf("missing read guidance: %s", fragment)
		}
	}
}
