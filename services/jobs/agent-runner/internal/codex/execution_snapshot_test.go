package codex

import (
	"errors"
	"slices"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestExecutionSnapshotConsumerRequiresExactOwnToolBeforeTurn(t *testing.T) {
	input := providerProcessInput()
	input.Mode = runtimecontract.RunnerModeTurn
	input.LeaseRef, input.LeaseFence, input.LeaseGeneration = "lease_fixture123", "private-fixture", 1
	names := RequiredMCPToolNames(input)
	if !slices.Contains(names, runtimecontract.ExecutionSnapshotTool) || slices.Contains(names, "get_configuration_catalog") {
		t.Fatal("own execution readiness lost or opened privileged catalog")
	}
	state := newProtocolState(testThreadID)
	state.threadID = testThreadID
	if ready, err := state.bindRequiredMCPStatus(mcpStatusResponse("connected", names), names); !ready || err != nil {
		t.Fatal("complete own-read catalog did not become ready")
	}
	state = newProtocolState(testThreadID)
	state.threadID = testThreadID
	missing := slices.DeleteFunc(slices.Clone(names), func(name string) bool { return name == runtimecontract.ExecutionSnapshotTool })
	if ready, err := state.bindRequiredMCPStatus(mcpStatusResponse("connected", missing), names); ready || !errors.Is(err, ErrRequiredMCPUnavailable) {
		t.Fatal("missing own-read consumer started execution")
	}
	input.Mode = runtimecontract.RunnerModeWarm
	if slices.Contains(RequiredMCPToolNames(input), runtimecontract.ExecutionSnapshotTool) {
		t.Fatal("warm process exposed current-turn read")
	}
}
