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

// assistantAgentRuntimeConfigurationFixture builds safe runtime metadata with exact
// canonical projected inventory digest and truthful MISSING tool observations.
// Its fixed model/image/ref data are synthetic and do not prove an admitted live image.
// assistantAgentRuntimeConfigurationFixture создаёт safe runtime metadata с exact canonical
// projected inventory digest и MISSING tool observations. Fixed model/image/ref data
// синтетические и не доказывают допуск live image.
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
		"configured_tools": []string{"Configured tool"}, "verified_tool_inventory": inventory,
		"model_configuration": map[string]any{"ref": "arcfg_fixture123", "version": 5, "digest": digest, "provider": "openai-codex", "model": "gpt-6.1-sol", "runtime_profile_ref": "builtin-safe-runtime", "provider_policy_mode": "FIXED", "provider_accounts": []any{map[string]any{"accountRef": "pacc_fixture123", "weight": 1}}, "reasoning_effort": "high", "web_search_mode": "", "published_overlay_ref": "ovl_fixture123", "published_overlay_version": 2, "published_overlay_digest": digest, "has_draft_overlay": false},
		"resources":           runtimecontract.DefaultRuntimeEnvironmentPolicy().Resources}
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

// TestAssistantAgentRuntimeConfigurationMCPFullRead checks typed validation and
// caster acceptance, then reassembles seventy-one-byte local MCP pages through a
// fake owner for both scopes, preserving exact target and lease generation.
// TestAssistantAgentRuntimeConfigurationMCPFullRead проверяет typed validation/caster
// и собирает local MCP pages до семидесяти одного byte через fake owner обоих
// scopes, сохраняя exact target и lease generation.
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

// TestAssistantAgentRuntimeConfigurationProducerContractFixture compares exact bytes
// against the checked-in shared producer fixture and checks caster acceptance of
// its recomputed digest; this is a local fixture contract check, not a live RPC proof.
// TestAssistantAgentRuntimeConfigurationProducerContractFixture сравнивает exact bytes
// с checked-in shared producer fixture и проверяет caster с пересчитанным digest;
// это local fixture contract check, не live RPC proof.
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

// TestAssistantNamedRuntimeConfigurationWithoutCurrentAgentScreen checks safe runtime
// metadata selectors/casters work without a current agent screen for both scopes,
// while switching the same selector to full AGENT_CONFIGURATION remains rejected.
// TestAssistantNamedRuntimeConfigurationWithoutCurrentAgentScreen проверяет safe runtime
// metadata selectors/casters без current agent screen обоих scopes; смена selector
// на full AGENT_CONFIGURATION по-прежнему отвергается.
func TestAssistantNamedRuntimeConfigurationWithoutCurrentAgentScreen(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		input, arguments, response := assistantAgentRuntimeConfigurationFixture(t)
		input.AssistantScope, input.AssistantContext = scope, nil
		selector := arguments["assistant_configuration_catalog"].(map[string]any)
		request, err := parseAssistantConfigurationCatalog(input, arguments, selector)
		if err != nil {
			t.Fatal("resolved metadata target rejected without screen", err)
		}
		if _, err := castAssistantConfigurationCatalog(input, request, response); err != nil {
			t.Fatal("fresh named metadata response rejected", err)
		}
		selector["kind"] = "AGENT_CONFIGURATION"
		if _, err := parseAssistantConfigurationCatalog(input, arguments, selector); err == nil {
			t.Fatal("full instructions lost current-context boundary")
		}
	}
}

// TestAssistantAgentRuntimeConfigurationRejectsForeignAndOwnSelectors checks invalid
// scope, kind, helper/entity refs and extra selectors are rejected locally, and
// CURRENT_CONFIGURATION remains own-only rather than reading the selected employee.
// TestAssistantAgentRuntimeConfigurationRejectsForeignAndOwnSelectors проверяет локальный
// отказ неверных scope,kind,helper/entity refs и extra selectors; CURRENT_CONFIGURATION
// остаётся own-only, а не чтением выбранного employee.
func TestAssistantAgentRuntimeConfigurationRejectsForeignAndOwnSelectors(t *testing.T) {
	for _, mutate := range []func(*runtimecontract.RunnerInput, map[string]any){
		func(i *runtimecontract.RunnerInput, _ map[string]any) {
			i.AssistantScope = runtimecontract.AssistantScopeNone
		},
		func(_ *runtimecontract.RunnerInput, s map[string]any) { s["entity_kind"] = "WORKFLOW" },
		func(_ *runtimecontract.RunnerInput, s map[string]any) { s["assistant_ref"] = "agt_foreign123" },
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
		if !assistantAgentRuntimeConfigurationAvailable(input) && slices.Contains(kinds, "AGENT_RUNTIME_CONFIGURATION") {
			t.Fatal("catalog exposed outside assistant scope")
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

// TestAssistantAgentRuntimeConfigurationRejectsMismatchAndSecrets checks rehashed
// private fields, mismatched versions/project/inventory and invalid resource/tool shapes
// are rejected with no sentinel reflection; recomputing an outer digest is insufficient.
// TestAssistantAgentRuntimeConfigurationRejectsMismatchAndSecrets проверяет отказ rehashed
// private fields,mismatched versions/project/inventory и invalid resource/tool shapes
// без sentinel reflection; пересчёта outer digest недостаточно.
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
			s["model_configuration"].(map[string]any)["account_ref"] = "PRIVATE_SENTINEL"
		},
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) {
			s["model_configuration"].(map[string]any)["provider_accounts"].([]any)[0].(map[string]any)["credential"] = "PRIVATE_SENTINEL"
		},
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) {
			s["model_configuration"].(map[string]any)["provider_accounts"].([]any)[0].(map[string]any)["weight"] = float64(2)
		},
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) {
			delete(s["model_configuration"].(map[string]any), "has_draft_overlay")
		},
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) {
			s["resources"].(map[string]any)["memory_limit_mib"] = float64(-1)
		},
		func(_ *cp.AssistantConfigurationCatalogResponse, s map[string]any) {
			s["resources"].(map[string]any)["workspace_limits"] = map[string]any{}
		},
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

// TestAssistantAgentRuntimeConfigurationRollingReadCompatibility accepts only exact v1 and v2 read projections, returns v1 without invented configuration fields and rejects mixed/incomplete shapes.
// TestAssistantAgentRuntimeConfigurationRollingReadCompatibility принимает только точные проекции чтения v1 и v2, возвращает v1 без выдуманных полей конфигурации и отвергает смешанные и неполные формы.
func TestAssistantAgentRuntimeConfigurationRollingReadCompatibility(t *testing.T) {
	for _, variant := range []string{"v1", "v2", "v2-defaults", "mixed", "incomplete-v2", "invalid-v1", "null-effort", "null-search", "null-draft"} {
		t.Run(variant, func(t *testing.T) {
			input, arguments, response := assistantAgentRuntimeConfigurationFixture(t)
			var snapshot map[string]any
			if json.Unmarshal(response.AgentRuntimeConfiguration.ConfigurationJson, &snapshot) != nil {
				t.Fatal("decode rolling fixture")
			}
			model := snapshot["model_configuration"].(map[string]any)
			if variant == "v1" || variant == "mixed" || variant == "invalid-v1" {
				legacy := map[string]any{}
				for _, key := range []string{"ref", "version", "digest", "provider", "model"} {
					legacy[key] = model[key]
				}
				model = legacy
				snapshot["model_configuration"] = model
			}
			switch variant {
			case "v2-defaults":
				model["reasoning_effort"], model["web_search_mode"] = "", ""
			case "null-effort":
				model["reasoning_effort"] = nil
			case "null-search":
				model["web_search_mode"] = nil
			case "null-draft":
				model["has_draft_overlay"] = nil
			case "mixed":
				model["reasoning_effort"] = "high"
			case "incomplete-v2":
				delete(model, "published_overlay_digest")
			case "invalid-v1":
				model["digest"] = "invalid"
			}
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(raw)
			response.AgentRuntimeConfiguration.ConfigurationJson = raw
			response.AgentRuntimeConfiguration.ConfigurationSha256 = hex.EncodeToString(hash[:])
			request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
			if err != nil {
				t.Fatal(err)
			}
			result, err := castAssistantAgentRuntimeConfiguration(input, request, response)
			valid := variant == "v1" || variant == "v2" || variant == "v2-defaults"
			if (err == nil) != valid {
				t.Fatalf("rolling %s acceptance: %v", variant, err)
			}
			if variant == "v1" {
				projected := result["agent_runtime_configuration"].(map[string]any)["configuration"].(map[string]any)["model_configuration"].(map[string]any)
				if len(projected) != 5 || !onlyKeys(projected, "ref", "version", "digest", "provider", "model") {
					t.Fatal("legacy read invented missing owner pins")
				}
			}
		})
	}
}
