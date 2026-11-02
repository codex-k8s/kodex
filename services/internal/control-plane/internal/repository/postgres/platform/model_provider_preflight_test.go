package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestOpenCodePreflightRejectsUnsupportedNativeConfiguration blocks known incompatibility before account catalog or launch.
// TestOpenCodePreflightRejectsUnsupportedNativeConfiguration блокирует known incompatibility до account catalog или launch.
func TestOpenCodePreflightRejectsUnsupportedNativeConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, overlay string
		tools         int32
	}{{"native overlay", "personality = \"friendly\"", 0}, {"native tool", "", 1}, {"missing environment", "", -1}} {
		t.Run(test.name, func(t *testing.T) {
			context := providerUsageContext{Input: &entity.ProviderAccountUsageContext{Purpose: "LAUNCH"}, Agent: providerUsageAgent{Enabled: true, State: "READY", ConfigRef: "arcfg_synthetic", Provider: "opencode-test-gateway", Model: "test-model", Overlay: test.overlay, NativeToolCount: test.tools}}
			result := providerModelCompatibility(providerUsageAccount{}, context, "", "")
			if result.State != "BLOCKED" || result.Reason != "RUNTIME_PROFILE_UNAVAILABLE" {
				t.Fatal("incompatible native configuration reached launch")
			}
		})
	}
}
