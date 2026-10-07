package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestNativeToolProjectionDoesNotRequireMCPGrant(t *testing.T) {
	for _, kind := range []string{
		runtimecontract.NativeToolKindShell, runtimecontract.NativeToolKindFileChange,
		runtimecontract.NativeToolKindWebSearch, runtimecontract.NativeToolKindDynamicTool,
		runtimecontract.NativeToolKindImageView, runtimecontract.NativeToolKindImageGeneration,
		runtimecontract.NativeToolKindSleep,
	} {
		if !toolCapabilityMatches(kind, "", false, false) {
			t.Fatalf("native tool kind %s was rejected", kind)
		}
		if toolCapabilityMatches(kind, "platform.configuration.read", false, false) || toolCapabilityMatches(kind, "", true, false) {
			t.Fatalf("native tool kind %s crossed an MCP capability boundary", kind)
		}
	}
}

func TestAssistantConfigurationProjectionRequiresEligibleAssistant(t *testing.T) {
	t.Parallel()
	for tool, capability := range map[string]string{
		"get_configuration_catalog":  "platform.configuration.read",
		"find_platform_resources":    "platform.resources.search",
		"propose_configuration_plan": "platform.configuration.plan",
		"propose_assistant_metadata": "platform.presentation.propose",
	} {
		if !toolCapabilityMatches(tool, capability, false, true) {
			t.Fatalf("eligible configuration assistant projection %s was rejected", tool)
		}
		if toolCapabilityMatches(tool, capability, false, false) ||
			toolCapabilityMatches(tool, "platform.unknown", false, true) ||
			toolCapabilityMatches(tool, capability, true, true) {
			t.Fatalf("configuration assistant projection %s crossed its exact capability boundary", tool)
		}
	}
}

func TestIntegrationCatalogProjectionHasExactReadCapability(t *testing.T) {
	t.Parallel()
	if !toolCapabilityMatches("get_integration_catalog", "platform.integration.catalog", false, false) ||
		!toolCapabilityMatches("get_integration_catalog", "platform.integration.catalog", false, true) {
		t.Fatal("integration catalog projection was rejected")
	}
	if toolCapabilityMatches("get_integration_catalog", "", false, true) ||
		toolCapabilityMatches("get_integration_catalog", "platform.integration.catalog", true, true) ||
		toolCapabilityMatches("invoke_integration", "platform.integration.catalog", false, true) {
		t.Fatal("integration catalog projection crossed the invocation boundary")
	}
}

func TestManagedMCPToolProjectionRequiresExactCapabilityAndGrant(t *testing.T) {
	t.Parallel()
	for tool, capability := range map[string]string{
		runtimecontract.Context7ResolveTool: runtimecontract.Context7ResolveCapability,
		runtimecontract.Context7QueryTool:   runtimecontract.Context7QueryCapability,
	} {
		t.Run(tool, func(t *testing.T) {
			for _, assistant := range []bool{false, true} {
				if !toolCapabilityMatches(tool, capability, true, assistant) {
					t.Fatal("exact managed MCP grant projection was rejected")
				}
				for _, wrongCapability := range []string{"", "platform.configuration.read", "context7.unknown", runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
					if wrongCapability != capability && toolCapabilityMatches(tool, wrongCapability, true, assistant) {
						t.Fatal("managed MCP alias crossed its exact capability boundary")
					}
				}
				if toolCapabilityMatches(tool, capability, false, assistant) {
					t.Fatal("managed MCP alias was accepted without a grant")
				}
			}
		})
	}
	if !toolCapabilityMatches("invoke_integration", "context7.library.resolve", true, false) ||
		toolCapabilityMatches("invoke_integration", "", true, true) ||
		toolCapabilityMatches("context7_arbitrary_alias", runtimecontract.Context7ResolveCapability, true, true) {
		t.Fatal("generic integration or closed alias boundary changed")
	}
}
