package callback

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

func assistantAgentRuntimeConfigurationFixture(t *testing.T) (runtimecontract.RunnerInput, map[string]any, *cp.AssistantConfigurationCatalogResponse) {
	t.Helper()
	input, arguments, response := assistantAgentConfigurationFixture(t)
	arguments["assistant_configuration_catalog"].(map[string]any)["kind"] = "AGENT_RUNTIME_CONFIGURATION"
	digest := strings.Repeat("a", 64)
	manifest := runtimecontract.ImageToolManifest{Schema: runtimecontract.ImageInventorySchema, SpecSHA256: digest, ImmutableBuildSHA256: digest, RuntimeContractSHA256: digest, Platform: "linux/amd64"}
	for _, probe := range runtimecontract.ImageToolProbes() {
		manifest.Tools = append(manifest.Tools, runtimecontract.ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required})
	}
	manifestRaw, _ := json.Marshal(manifest)
	inventory := runtimecontract.ImageToolInventory{Schema: runtimecontract.ImageInventoryBindingSchema, ImageDigest: "sha256:" + digest, ProvenanceSHA256: digest, Platforms: []runtimecontract.ImagePlatformInventory{{PlatformDigest: "sha256:" + digest, ManifestSHA256: runtimecontract.ImageInventorySHA256(manifestRaw), Manifest: manifest}}}
	snapshot := map[string]any{"agent_ref": input.AssistantContext.EntityRef, "project_ref": input.ProjectRef, "version": *input.AssistantContext.EntityVersion,
		"binding_ref": "aenv_binding123", "binding_version": 7, "binding_digest": digest,
		"environment_ref": "renv_fixture123", "environment_name": "Окружение Developer", "environment_version": 12,
		"published_version_ref": "renvv_fixture123", "published_revision": 4, "published_digest": digest,
		"image":            map[string]any{"artifact_ref": "imgart_fixture123", "recipe_ref": "recipe_fixture123", "recipe_generation": 2, "reference": "registry.test/runtime@sha256:" + digest, "digest": "sha256:" + digest},
		"configured_tools": []string{"Configured tool"}, "verified_tool_inventory": inventory}
	raw, _ := json.Marshal(snapshot)
	_ = json.Unmarshal(raw, &snapshot)
	inventoryRaw, _ := json.Marshal(snapshot["verified_tool_inventory"])
	snapshot["verified_tool_inventory_sha256"] = runtimecontract.ImageInventorySHA256(inventoryRaw)
	raw, _ = json.Marshal(snapshot)
	sum := sha256.Sum256(raw)
	response.Kind = cp.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_AGENT_RUNTIME_CONFIGURATION
	response.AgentConfiguration = nil
	response.AgentRuntimeConfiguration = &cp.AssistantAgentRuntimeConfiguration{AgentRef: input.AssistantContext.EntityRef, ProjectRef: input.ProjectRef, Version: *input.AssistantContext.EntityVersion, ConfigurationJson: raw, ConfigurationSha256: hex.EncodeToString(sum[:])}
	return input, arguments, response
}

func TestAssistantAgentRuntimeConfigurationMCPFullRead(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			input, arguments, response := assistantAgentRuntimeConfigurationFixture(t)
			input.AssistantScope = scope
			request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
			if err != nil {
				t.Fatal("runtime selector", err)
			}
			var typed assistantAgentRuntimeReadSnapshot
			if err := json.Unmarshal(response.AgentRuntimeConfiguration.ConfigurationJson, &typed); err != nil {
				t.Fatal("typed decode", err)
			}
			if !validAssistantAgentRuntimeReadSnapshot(typed) {
				t.Fatal("typed runtime validation")
			}
			if _, err := castAssistantConfigurationCatalog(input, request, response); err != nil {
				t.Fatal("runtime caster", err)
			}
			arguments["assistant_configuration_catalog"].(map[string]any)["maximum_bytes"] = 71
			client := &assistantFreshCatalogMCPClient{assistantDefinitionCatalogClient: &assistantDefinitionCatalogClient{response: &cp.SearchAssistantResourcesResponse{AssistantConfigurationCatalog: response}}}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			readAssistantConfigurationMCP(t, input, arguments, server, "agent_runtime_configuration", response.AgentRuntimeConfiguration.ConfigurationJson)
			if client.request.GetAssistantConfigurationCatalog().GetEntityRef() != input.AssistantContext.EntityRef || client.request.GetGeneration() != input.LeaseGeneration {
				t.Fatal("exact context and lease pins lost")
			}
		})
	}
}

func TestAssistantAgentRuntimeConfigurationProducerContractFixture(t *testing.T) {
	input, arguments, response := assistantAgentRuntimeConfigurationFixture(t)
	golden, err := os.ReadFile("../../../../../contracts/proto/testdata/assistant_agent_runtime_configuration.json")
	if err != nil {
		t.Fatal("read shared producer contract fixture", err)
	}
	raw := bytes.TrimSuffix(golden, []byte("\n"))
	if !bytes.Equal(raw, response.AgentRuntimeConfiguration.ConfigurationJson) {
		t.Fatal("RC fixture differs from exact producer projection")
	}
	sum := sha256.Sum256(raw)
	response.AgentRuntimeConfiguration.ConfigurationJson = raw
	response.AgentRuntimeConfiguration.ConfigurationSha256 = hex.EncodeToString(sum[:])
	request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := castAssistantConfigurationCatalog(input, request, response); err != nil {
		t.Fatal("exact producer projection rejected", err)
	}
}

func TestAssistantAgentRuntimeConfigurationRejectsForeignAndOwnSelectors(t *testing.T) {
	for _, mutate := range []func(*runtimecontract.RunnerInput, map[string]any){
		func(i *runtimecontract.RunnerInput, _ map[string]any) {
			i.AssistantScope = runtimecontract.AssistantScopeNone
		},
		func(i *runtimecontract.RunnerInput, _ map[string]any) { i.AssistantContext.EntityKind = "WORKFLOW" },
		func(i *runtimecontract.RunnerInput, _ map[string]any) {
			i.AssistantContext.AllowedOperations = []string{"CHANGE_INTEGRATION_GRANT"}
		},
		func(_ *runtimecontract.RunnerInput, s map[string]any) { s["assistant_ref"] = "agt_foreign123" },
		func(_ *runtimecontract.RunnerInput, s map[string]any) { s["entity_ref"] = "agt_foreign123" },
		func(_ *runtimecontract.RunnerInput, s map[string]any) { s["entity_ref"] = "" },
		func(_ *runtimecontract.RunnerInput, s map[string]any) { s["query"] = "Developer" },
		func(_ *runtimecontract.RunnerInput, s map[string]any) { s["offset"] = 10 },
		func(_ *runtimecontract.RunnerInput, s map[string]any) { s["account_ref"] = "acc_foreign123" },
		func(_ *runtimecontract.RunnerInput, s map[string]any) { s["owner_ref"] = "usr_injected123" },
	} {
		input, arguments, _ := assistantAgentRuntimeConfigurationFixture(t)
		selector := arguments["assistant_configuration_catalog"].(map[string]any)
		mutate(&input, selector)
		if _, err := parseAssistantConfigurationCatalog(input, arguments, selector); err == nil {
			t.Fatal("foreign or unsupported selector accepted")
		}
		kinds := assistantConfigurationCatalogInputSchema(input)["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]string)
		if !assistantAgentConfigurationAvailable(input) && slices.Contains(kinds, "AGENT_RUNTIME_CONFIGURATION") {
			t.Fatal("catalog exposed without exact context")
		}
	}
	input, arguments, _ := assistantAgentRuntimeConfigurationFixture(t)
	selector := arguments["assistant_configuration_catalog"].(map[string]any)
	selector["kind"], selector["assistant_ref"] = "CURRENT_CONFIGURATION", input.AssistantContext.EntityRef
	delete(selector, "entity_kind")
	delete(selector, "entity_ref")
	if _, err := parseAssistantConfigurationCatalog(input, arguments, selector); err == nil {
		t.Fatal("CURRENT_CONFIGURATION own-only relaxed")
	}
}

func TestAssistantAgentRuntimeConfigurationRejectsMismatchAndSecrets(t *testing.T) {
	for _, mutate := range []func(*cp.AssistantConfigurationCatalogResponse, map[string]any){
		func(r *cp.AssistantConfigurationCatalogResponse, _ map[string]any) {
			r.AgentRuntimeConfiguration.Version++
		},
		func(r *cp.AssistantConfigurationCatalogResponse, _ map[string]any) { r.ProjectRef = "prj_foreign123" },
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) { s["values"] = "PRIVATE_SENTINEL" },
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) {
			s["configured_tools"] = []map[string]any{{"name": "git", "command": "PRIVATE_SENTINEL"}}
		},
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) {
			s["image"].(map[string]any)["credential"] = "PRIVATE_SENTINEL"
		},
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) { s["binding_version"] = float64(0) },
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) {
			s["verified_tool_inventory_sha256"] = strings.Repeat("b", 64)
		},
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) {
			s["verified_tool_inventory"].(map[string]any)["imageDigest"] = "sha256:" + strings.Repeat("b", 64)
		},
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) {
			s["verified_tool_inventory"].(map[string]any)["provenanceSHA256"] = strings.Repeat("b", 64)
		},
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) {
			var typed runtimecontract.ImageToolInventory
			raw, _ := json.Marshal(s["verified_tool_inventory"])
			_ = json.Unmarshal(raw, &typed)
			raw, _ = json.Marshal(typed)
			s["verified_tool_inventory_sha256"] = runtimecontract.ImageInventorySHA256(raw)
		},
	} {
		input, arguments, fixture := assistantAgentRuntimeConfigurationFixture(t)
		response := proto.Clone(fixture).(*cp.AssistantConfigurationCatalogResponse)
		var snapshot map[string]any
		_ = json.Unmarshal(response.AgentRuntimeConfiguration.ConfigurationJson, &snapshot)
		mutate(response, snapshot)
		raw, _ := json.Marshal(snapshot)
		sum := sha256.Sum256(raw)
		response.AgentRuntimeConfiguration.ConfigurationJson, response.AgentRuntimeConfiguration.ConfigurationSha256 = raw, hex.EncodeToString(sum[:])
		request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := castAssistantConfigurationCatalog(input, request, response); err == nil || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
			t.Fatal("invalid or secret-bearing owner projection accepted")
		}
	}
}
