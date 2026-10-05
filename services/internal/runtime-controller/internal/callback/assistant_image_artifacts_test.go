package callback

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

func assistantCandidateInventoryFixture(entry *cp.AssistantConfigurationCatalogEntry) *cp.ImageToolInventory {
	digest := strings.Repeat("a", 64)
	inventory := &cp.ImageToolInventory{Status: "VERIFIED", Sha256: digest, ImageDigest: entry.ManifestDigest, ProvenanceSha256: digest,
		Platforms: []*cp.ImagePlatformToolInventory{{Platform: "linux/amd64", PlatformDigest: entry.ManifestDigest, ManifestSha256: digest}}}
	for _, probe := range runtimecontract.ImageToolProbes() {
		inventory.Platforms[0].Tools = append(inventory.Platforms[0].Tools, &cp.ImageToolObservation{
			Name: probe.Name, Status: "VERIFIED", Path: probe.Paths[0], Version: "1.0.0", Sha256: digest, Required: probe.Required})
	}
	return inventory
}

func TestAssistantCandidateInventoryTraversesMCPWithExactPromotionPins(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			input, arguments, response := assistantFreshCatalogFixture(scope, "IMAGE_ARTIFACTS")
			entry := response.AssistantConfigurationCatalog.Entries[0]
			entry.VerifiedToolInventory = assistantCandidateInventoryFixture(entry)
			client := &assistantFreshCatalogMCPClient{assistantDefinitionCatalogClient: &assistantDefinitionCatalogClient{response: response}}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			params, _ := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": arguments})
			recorder := httptest.NewRecorder()
			server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"candidate"`), Params: params}, input)
			var wire struct {
				Result struct {
					IsError           bool           `json:"isError"`
					StructuredContent map[string]any `json:"structuredContent"`
				} `json:"result"`
			}
			if json.Unmarshal(recorder.Body.Bytes(), &wire) != nil || wire.Result.IsError {
				t.Fatal("verified candidate did not traverse exact admitted MCP/RPC path")
			}
			catalog := wire.Result.StructuredContent["assistant_configuration_catalog"].(map[string]any)
			candidate := catalog["entries"].([]any)[0].(map[string]any)
			inventory := candidate["verified_tool_inventory"].(map[string]any)
			tools := inventory["platforms"].([]any)[0].(map[string]any)["tools"].([]any)
			required := 0
			for _, value := range tools {
				if value.(map[string]any)["required"] == true {
					required++
				}
			}
			if candidate["admission_verdict"] != "ACCEPTED" || candidate["promotion_state"] != "PROMOTED" ||
				inventory["status"] != "VERIFIED" || inventory["image_digest"] != entry.ManifestDigest ||
				inventory["sha256"] != entry.VerifiedToolInventory.Sha256 || inventory["provenance_sha256"] != entry.VerifiedToolInventory.ProvenanceSha256 ||
				len(tools) != len(runtimecontract.ImageToolProbes()) || required != 38 {
				t.Fatal("candidate inventory lost exact state, immutable pins or complete required tools")
			}
			if strings.Contains(recorder.Body.String(), input.LeaseFence) ||
				strings.Contains(client.projection.GetSafeResult(), entry.VerifiedToolInventory.Sha256) ||
				len(client.projection.GetSafeParameters().AsMap()) != 1 {
				t.Fatal("candidate evidence leaked authority or persisted full inventory")
			}
		})
	}
}

func TestAssistantCandidateInventoryRejectsDetachedOrUnboundedEvidence(t *testing.T) {
	input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, "IMAGE_ARTIFACTS")
	response.AssistantConfigurationCatalog.Entries[0].VerifiedToolInventory = assistantCandidateInventoryFixture(response.AssistantConfigurationCatalog.Entries[0])
	request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	for _, testcase := range []struct {
		name   string
		mutate func(*cp.AssistantConfigurationCatalogEntry)
	}{
		{"not accepted", func(e *cp.AssistantConfigurationCatalogEntry) { e.AdmissionVerdict = "REJECTED" }},
		{"not promoted", func(e *cp.AssistantConfigurationCatalogEntry) { e.PromotionState = "CLAIMED" }},
		{"foreign owner", func(e *cp.AssistantConfigurationCatalogEntry) { e.OrganizationRef = "org_foreign123" }},
		{"missing inventory", func(e *cp.AssistantConfigurationCatalogEntry) { e.VerifiedToolInventory = nil }},
		{"foreign image", func(e *cp.AssistantConfigurationCatalogEntry) {
			e.VerifiedToolInventory.ImageDigest = "sha256:" + strings.Repeat("c", 64)
		}},
		{"bad provenance", func(e *cp.AssistantConfigurationCatalogEntry) { e.VerifiedToolInventory.ProvenanceSha256 = "" }},
		{"unknown fields", func(e *cp.AssistantConfigurationCatalogEntry) {
			e.VerifiedToolInventory.ProtoReflect().SetUnknown([]byte{0xa0, 6, 1})
		}},
		{"extra observation", func(e *cp.AssistantConfigurationCatalogEntry) {
			e.VerifiedToolInventory.Platforms[0].Tools = append(e.VerifiedToolInventory.Platforms[0].Tools, proto.Clone(e.VerifiedToolInventory.Platforms[0].Tools[0]).(*cp.ImageToolObservation))
		}},
		{"fake verified tool", func(e *cp.AssistantConfigurationCatalogEntry) {
			e.VerifiedToolInventory.Platforms[0].Tools[0].Sha256 = ""
		}},
		{"platform bound", func(e *cp.AssistantConfigurationCatalogEntry) {
			for range 3 {
				e.VerifiedToolInventory.Platforms = append(e.VerifiedToolInventory.Platforms, e.VerifiedToolInventory.Platforms[0])
			}
		}},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			value := proto.Clone(response.AssistantConfigurationCatalog).(*cp.AssistantConfigurationCatalogResponse)
			testcase.mutate(value.Entries[0])
			if _, err := castAssistantConfigurationCatalog(input, request, value); err == nil {
				t.Fatal("detached candidate evidence accepted")
			}
		})
	}
	for _, kind := range []string{"ROLE_IMAGE_RECIPES", "ROLE_ENVIRONMENTS", "ASSISTANTS"} {
		i, a, r := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, kind)
		r.AssistantConfigurationCatalog.Entries[0].VerifiedToolInventory = response.AssistantConfigurationCatalog.Entries[0].VerifiedToolInventory
		q, err := parseAssistantConfigurationCatalog(i, a, a["assistant_configuration_catalog"])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := castAssistantConfigurationCatalog(i, q, r.AssistantConfigurationCatalog); err == nil {
			t.Fatal("inventory escaped closed catalog kind")
		}
	}
}

func TestAssistantCandidateInventoryPreservesHistoricalUnavailable(t *testing.T) {
	input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, "IMAGE_ARTIFACTS")
	request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	result, err := castAssistantConfigurationCatalog(input, request, response.AssistantConfigurationCatalog)
	if err != nil {
		t.Fatal("historical candidate poisoned the catalog")
	}
	inventory := result["entries"].([]map[string]any)[0]["verified_tool_inventory"].(map[string]any)
	if inventory["status"] != "UNAVAILABLE" || inventory["sha256"] != "" || inventory["image_digest"] != "" || inventory["provenance_sha256"] != "" || len(inventory["platforms"].([]any)) != 0 {
		t.Fatal("historical candidate invented verified evidence")
	}
}
