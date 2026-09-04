package modelcatalog

import (
	"slices"
	"testing"
)

func TestModelsExposeEffortsPerModel(t *testing.T) {
	astra, ok := Find("gpt-6-astra", nil)
	if !ok || slices.Contains(astra.Efforts, "none") || !slices.Contains(astra.Efforts, "max") {
		t.Fatalf("gpt-6-astra capabilities = %#v", astra)
	}
	mini, ok := Find("gpt-5.4-mini", nil)
	if !ok || !slices.Contains(mini.Efforts, "none") || slices.Contains(mini.Efforts, "max") {
		t.Fatalf("gpt-5.4-mini capabilities = %#v", mini)
	}
	if _, ok := Find("gpt-5.3-codex", nil); !ok {
		t.Fatal("official gpt-5.3-codex model is absent")
	}
}

func TestSparkRequiresProviderReport(t *testing.T) {
	if _, ok := Find("gpt-5.3-codex-spark", nil); ok {
		t.Fatal("unreported spark model became available")
	}
	if _, ok := Find("gpt-5.3-codex-spark", []string{"gpt-5.3-codex-spark"}); !ok {
		t.Fatal("provider-reported spark model is absent")
	}
}
