package callback

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

// workflowMetadataFixture reads exact checked-in producer metadata bytes, computes
// their digest and builds a screen-independent synthetic project read selector/response.
// No private workflow draft or real owner connection is created.
// workflowMetadataFixture читает exact checked-in producer metadata bytes, считает
// digest и строит screen-independent synthetic project selector/response.
// Private workflow draft и real owner connection не создаются.
func workflowMetadataFixture(t *testing.T) (runtimecontract.RunnerInput, map[string]any, *cp.AssistantConfigurationCatalogResponse) {
	t.Helper()
	input, args, _ := assistantFreshCatalogFixture(runtimecontract.AssistantScopeProject, "WORKFLOW_METADATA")
	input.ProjectRef = "prj_fixture123"
	input.AssistantContext = nil
	selector := args["assistant_configuration_catalog"].(map[string]any)
	delete(selector, "query")
	selector["entity_kind"] = "WORKFLOW"
	selector["entity_ref"] = "wfl_fixture123"
	raw, err := os.ReadFile("../../../../../contracts/proto/testdata/assistant_workflow_metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.TrimSuffix(raw, []byte("\n"))
	hash := sha256.Sum256(raw)
	response := &cp.AssistantConfigurationCatalogResponse{Kind: cp.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_WORKFLOW_METADATA, AssistantRef: input.AgentRef, ScopeKind: "PROJECT", OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef,
		WorkflowConfiguration: &cp.AssistantWorkflowConfiguration{WorkflowRef: "wfl_fixture123", ProjectRef: input.ProjectRef, Version: 7, ConfigurationJson: raw, ConfigurationSha256: hex.EncodeToString(hash[:])}}
	return input, args, response
}

// TestAssistantWorkflowMetadataNativePagesWithoutCurrentScreen checks safe metadata
// is advertised without exposing full-source reads and reassembles local MCP pages
// for both assistant scopes through a fake owner.
// TestAssistantWorkflowMetadataNativePagesWithoutCurrentScreen проверяет discovery safe
// metadata без full-source reads и собирает local MCP pages обоих assistant scopes
// через fake owner.
func TestAssistantWorkflowMetadataNativePagesWithoutCurrentScreen(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeProject, runtimecontract.AssistantScopeSystem} {
		input, args, response := workflowMetadataFixture(t)
		input.AssistantScope = scope
		kinds := assistantConfigurationCatalogInputSchema(input)["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]string)
		if !slices.Contains(kinds, "WORKFLOW_METADATA") || slices.Contains(kinds, "WORKFLOW_CONFIGURATION") {
			t.Fatal("metadata availability widened full source read")
		}
		args["assistant_configuration_catalog"].(map[string]any)["maximum_bytes"] = 71
		client := &assistantFreshCatalogMCPClient{assistantDefinitionCatalogClient: &assistantDefinitionCatalogClient{response: &cp.SearchAssistantResourcesResponse{AssistantConfigurationCatalog: response}}}
		server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
		readAssistantConfigurationMCP(t, input, args, server, "workflow_metadata", response.WorkflowConfiguration.ConfigurationJson)
	}
}

// TestAssistantWorkflowMetadataClosedPinsAndPrivateFields mutates rehashed private,
// null, project/version, duplicate-step, mixed-response and ordinary-runtime cases
// to verify metadata cannot widen its closed projection or caller scope.
// TestAssistantWorkflowMetadataClosedPinsAndPrivateFields меняет rehashed private,null,
// project/version,duplicate-step,mixed-response и ordinary-runtime cases, проверяя
// невозможность расширить closed projection и caller scope.
func TestAssistantWorkflowMetadataClosedPinsAndPrivateFields(t *testing.T) {
	input, args, response := workflowMetadataFixture(t)
	request, err := parseAssistantConfigurationCatalog(input, args, args["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"private", "null", "project", "version", "duplicate", "mixed", "ordinary"} {
		t.Run(mutation, func(t *testing.T) {
			r := proto.Clone(response).(*cp.AssistantConfigurationCatalogResponse)
			in := input
			var m map[string]any
			_ = json.Unmarshal(r.WorkflowConfiguration.ConfigurationJson, &m)
			switch mutation {
			case "private":
				m["instructions"] = "PRIVATE_SENTINEL"
			case "null":
				m["steps"].([]any)[0].(map[string]any)["parallel"] = nil
			case "project":
				r.ProjectRef = "prj_foreign123"
			case "version":
				r.WorkflowConfiguration.Version++
			case "duplicate":
				m["steps"].([]any)[1].(map[string]any)["key"] = "implementation"
			case "mixed":
				r.AgentConfiguration = &cp.AssistantAgentConfiguration{}
			case "ordinary":
				in.AssistantScope = runtimecontract.AssistantScopeNone
			}
			raw, _ := json.Marshal(m)
			hash := sha256.Sum256(raw)
			r.WorkflowConfiguration.ConfigurationJson = raw
			r.WorkflowConfiguration.ConfigurationSha256 = hex.EncodeToString(hash[:])
			if _, err := castAssistantConfigurationCatalog(in, request, r); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}
