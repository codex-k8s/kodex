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
		{name: "absent context", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "cacfbadb416a5c0f1f48e5e843a5fd112c07efb379132a9f804165105b77ba3f"},
		{name: "RUN", kind: "RUN", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "1ce808f5f244347cdc83ce58608b896343a003109ad40193cb1445a43955b1c0"},
		{name: "WORKFLOW", kind: "WORKFLOW", operations: []string{"UPDATE_WORKFLOW"}, reads: []string{"WORKFLOW_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "5fc0f142d70cb09726a4914e3ea6dbd93b1382ee13c4f6cdb3341c170bb29ae7"},
		{name: "AGENT update", kind: "AGENT", operations: []string{"UPDATE_AGENT"}, reads: []string{"AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "801df59e797492ec83d1a774a585c40785237858b9ffbad3e548439fa81e8abb"},
		{name: "AGENT instruction", kind: "AGENT", operations: []string{"CREATE_INSTRUCTION_DRAFT"}, reads: []string{"AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "801df59e797492ec83d1a774a585c40785237858b9ffbad3e548439fa81e8abb"},
		{name: "WORKFLOW revoked", kind: "WORKFLOW", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "1ce808f5f244347cdc83ce58608b896343a003109ad40193cb1445a43955b1c0"},
		{name: "AGENT revoked", kind: "AGENT", reads: []string{"AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA"}, schemaDigest: "1ce808f5f244347cdc83ce58608b896343a003109ad40193cb1445a43955b1c0"},
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
