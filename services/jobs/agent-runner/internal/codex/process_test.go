package codex

import (
	"errors"
	"strings"
	"testing"
)

// TestProviderStageErrorKeepsClassificationAndHidesDiagnostic checks errors.Is still reaches the
// private cause while the outer stage Error stays fixed and absent wrappers map to UNKNOWN.
// TestProviderStageErrorKeepsClassificationAndHidesDiagnostic проверяет доступность приватной причины
// через errors.Is при фиксированном внешнем Error и UNKNOWN без stage wrapper.
func TestProviderStageErrorKeepsClassificationAndHidesDiagnostic(t *testing.T) {
	secret := errors.New("provider response with secret diagnostic")
	err := atProviderStage(providerStageThreadCall, secret)
	if !errors.Is(err, secret) {
		t.Fatal("wrapped failure lost its original classification")
	}
	if got := providerStageOf(err); got != providerStageThreadCall {
		t.Fatalf("providerStageOf() = %q, want %q", got, providerStageThreadCall)
	}
	if strings.Contains(err.Error(), "secret") || err.Error() != "Codex provider execution stage failed" {
		t.Fatalf("provider stage error exposed diagnostic: %q", err.Error())
	}
	if got := providerStageOf(errors.New("unclassified")); got != providerStageUnknown {
		t.Fatalf("unclassified providerStageOf() = %q, want %q", got, providerStageUnknown)
	}
}
