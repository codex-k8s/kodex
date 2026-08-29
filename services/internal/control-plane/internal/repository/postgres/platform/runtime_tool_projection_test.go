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
