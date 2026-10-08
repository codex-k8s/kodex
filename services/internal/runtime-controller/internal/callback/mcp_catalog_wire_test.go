package callback

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// Public entrypoint: scripts/tests/runtime-mcp-catalog-test.sh. Ответ получается
// настоящим serveMCP, а не копией metadata/schema в consumer fixture.
func TestRuntimeMCPCatalogWireProducer(t *testing.T) {
	path := os.Getenv("KODEX_RUNTIME_MCP_CATALOG_FIXTURE")
	if path == "" {
		t.Skip("KODEX_RUNTIME_MCP_CATALOG_FIXTURE is not configured")
	}
	type fixture struct {
		Name    string
		Input   runtimecontract.RunnerInput
		Catalog json.RawMessage
	}
	files := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, ProjectRef: "prj_fixture", LeaseRef: "lea_fixture", LeaseFence: "fence", LeaseGeneration: 1, FileCatalog: &runtimecontract.RuntimeFileCatalog{Ref: "vfc_fixture", Digest: strings.Repeat("a", 64), Purposes: []string{runtimecontract.FilePurposeProject}}}
	email := files
	email.IntegrationGrants = []runtimecontract.RunnerIntegrationGrant{{Ref: "igr_fixture", GrantVersion: 1, ConnectionRef: "int_fixture", ConnectionVersion: 1, ApprovalPolicy: "HUMAN_EACH_EFFECT", DefinitionKey: "email", DefinitionVersion: "1.4.1", DefinitionDigest: strings.Repeat("b", 64), CapabilityKey: "email.message.send", Operation: "SEND", InputSchema: `{"type":"object","properties":{"subject":{"type":"string"}}}`, InputSchemaSHA256: strings.Repeat("c", 64)}}
	schemaDigest := sha256.Sum256([]byte(email.IntegrationGrants[0].InputSchema))
	email.IntegrationGrants[0].InputSchemaSHA256 = hex.EncodeToString(schemaDigest[:])
	delegation := runtimecontract.RunnerInput{}
	for index := range 128 {
		delegation.DelegationTargets = append(delegation.DelegationTargets, runtimecontract.RunnerDelegationTarget{
			Ref: fmt.Sprintf("agt_fixture_%03d", index), Name: fmt.Sprintf("Developer_%03d", index),
			Purpose: strings.Repeat("Я", 240), RoleDescription: strings.Repeat("Я", 240),
			WorkflowStepKey: fmt.Sprintf("step_%03d", index), WorkflowStepName: "Разработка",
		})
	}
	inputs := []fixture{{Name: "ordinary", Input: runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeNone}}, {Name: "system-assistant", Input: runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeSystem}},
		{Name: "project-assistant", Input: runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeProject, AssistantProfileRef: "asstprof_fixture123", ProjectRef: "prj_fixture123", AgentRef: "agt_fixture123"}},
		{Name: "files", Input: files}, {Name: "email-and-files", Input: email}, {Name: "delegation", Input: delegation}}
	for _, launch := range []bool{false, true} {
		for _, vfs := range []bool{false, true} {
			for _, delegation := range []bool{false, true} {
				input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone, ProjectRef: "prj_fixture"}
				if vfs {
					input = files
					input.AssistantScope = runtimecontract.AssistantScopeNone
				}
				if launch {
					input.Capabilities = []string{"platform.run.launch"}
				}
				if delegation {
					input.DelegationTargets = []runtimecontract.RunnerDelegationTarget{{Ref: "agt_fixture"}}
				}
				inputs = append(inputs, fixture{Name: fmt.Sprintf("ordinary-launch-%t-vfs-%t-delegation-%t", launch, vfs, delegation), Input: input})
			}
		}
	}
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, AssistantScope: scope, ProjectRef: "prj_fixture", AgentRef: "agt_fixture", Capabilities: []string{"platform.run.launch"}}
		if scope == runtimecontract.AssistantScopeProject {
			input.AssistantProfileRef = "asstprof_fixture123"
		}
		inputs = append(inputs, fixture{Name: "assistant-launch-" + string(scope), Input: input})
	}
	for i := range inputs {
		if inputs[i].Name == "system-assistant" || inputs[i].Name == "project-assistant" {
			version := int64(9)
			inputs[i].Input.AgentRef = "agt_fixture123"
			inputs[i].Input.AssistantContext = &runtimecontract.RunnerAssistantContext{EntityKind: "AGENT", EntityRef: "agt_recipient123", EntityVersion: &version, AllowedOperations: []string{"CREATE_INSTRUCTION_DRAFT", "UPDATE_AGENT"}}
		}
		request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":"agent-runner-tools","method":"tools/list","params":{}}`))
		response := httptest.NewRecorder()
		(&Server{}).serveMCP(response, request, inputs[i].Input)
		if response.Code != http.StatusOK {
			t.Fatal("actual MCP catalog unavailable")
		}
		inputs[i].Catalog = append([]byte(nil), response.Body.Bytes()...)
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
