package runtimecontract

import (
	"strings"
	"testing"
)

// TestRetiredNativeSearchCannotEnterOpenCodeOverlay rejects every retired mode without exposing submitted values.
// TestRetiredNativeSearchCannotEnterOpenCodeOverlay отклоняет все удалённые режимы без раскрытия переданных значений.
func TestRetiredNativeSearchCannotEnterOpenCodeOverlay(t *testing.T) {
	for _, mode := range []string{"disabled", "cached", "indexed", "live"} {
		if _, err := ParseConfigOverlay("web_search = \"" + mode + "\"\n"); err == nil || strings.Contains(err.Error(), mode) {
			t.Fatal("retired native search accepted or disclosed")
		}
	}
	readback, err := RenderSafeEffectiveConfig(SafeEffectiveConfigInput{Model: "fixture-model"})
	if err != nil || strings.Contains(readback, "web_search") || strings.Contains(readback, "history") {
		t.Fatal("retired runtime settings were materialized")
	}
}
