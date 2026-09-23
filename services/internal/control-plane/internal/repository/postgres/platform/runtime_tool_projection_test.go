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

func TestAssistantResourceSearchProjectionIsSystemOnly(t *testing.T) {
	t.Parallel()
	if !toolCapabilityMatches("find_platform_resources", "platform.resources.search", false, true) {
		t.Fatal("system assistant resource search projection was rejected")
	}
	if toolCapabilityMatches("find_platform_resources", "platform.resources.search", false, false) ||
		toolCapabilityMatches("find_platform_resources", "platform.configuration.read", false, true) ||
		toolCapabilityMatches("find_platform_resources", "platform.resources.search", true, true) {
		t.Fatal("resource search projection crossed its exact capability boundary")
	}
}
