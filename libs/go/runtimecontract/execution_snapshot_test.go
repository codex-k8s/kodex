package runtimecontract

import (
	"slices"
	"testing"
)

func TestExecutionSnapshotRegistryRequiresCurrentPinnedExecution(t *testing.T) {
	input := processObservationInput()
	input.Mode, input.LeaseRef, input.LeaseFence, input.LeaseGeneration = RunnerModeTurn, "lease_fixture123", "private-fixture", 1
	for name, change := range map[string]func(*RunnerInput){
		"ordinary": func(*RunnerInput) {}, "assistant": func(v *RunnerInput) { v.AssistantScope = AssistantScopeSystem },
		"warm": func(v *RunnerInput) { v.Mode = RunnerModeWarm }, "lease": func(v *RunnerInput) { v.LeaseRef = "" },
		"fence": func(v *RunnerInput) { v.LeaseFence = "" }, "generation": func(v *RunnerInput) { v.LeaseGeneration = 0 },
		"image": func(v *RunnerInput) { v.ImageManifestDigest = "unknown" }, "revision": func(v *RunnerInput) { v.RuntimeRevisionVersion = 0 },
		"turn": func(v *RunnerInput) { v.TurnRef = "" }, "attempt": func(v *RunnerInput) { v.Attempt = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			value := input
			change(&value)
			expected := name == "ordinary" || name == "assistant"
			if RuntimeExecutionSnapshotAvailable(value) != expected || slices.Contains(RuntimeMCPToolNames(value), ExecutionSnapshotTool) != expected {
				t.Fatal("execution snapshot registry crossed current execution boundary")
			}
			if name == "ordinary" && slices.Contains(RuntimeMCPToolNames(value), "get_configuration_catalog") {
				t.Fatal("own observation opened privileged configuration catalog")
			}
		})
	}
}
