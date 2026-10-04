package runtimecontract

import (
	"strings"
	"testing"
)

func TestNativeSearchOverlayCanonicalSafeReadback(t *testing.T) {
	seen := map[string]bool{}
	for _, mode := range []string{"disabled", "cached", "indexed", "live"} {
		raw := "web_search = \"" + mode + "\"\n"
		canonical, digest, err := CanonicalConfigOverlay(raw)
		if err != nil || seen[digest] {
			t.Fatal("mode was rejected or collapsed into another immutable digest")
		}
		seen[digest] = true
		parsed, err := ParseConfigOverlay(canonical)
		if err != nil || parsed.WebSearchMode != mode {
			t.Fatal("canonical mode was lost")
		}
		readback, err := RenderSafeEffectiveConfig(SafeEffectiveConfigInput{Model: "fixture-model", Overlay: canonical})
		if err != nil || !strings.Contains(readback, raw) || strings.Contains(readback, "allowed_domains") {
			t.Fatal("safe readback lost mode or conflated sandbox egress")
		}
	}
	for _, raw := range []string{`web_search = ""`, `web_search = "LIVE"`, `web_search = "future"`, `web_search = true`, `web_search = ["live"]`, `web_search = "live"` + "\nweb_search = \"cached\"", `[tools.web_search]` + "\nallowed_domains = [\"private-sentinel\"]"} {
		if _, err := ParseConfigOverlay(raw); err == nil || strings.Contains(err.Error(), "private-sentinel") {
			t.Fatal("unknown mode, structure, duplicate or secret-like input was accepted/disclosed")
		}
	}
	parsed, err := ParseConfigOverlay("")
	if err != nil || parsed.WebSearchMode != "" {
		t.Fatal("absent setting was silently materialized into a new owner selection")
	}
}
