package codex

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestNativeCaptureCannotBeManufacturedFromResult closes exported metadata without the isolated proof.
// TestNativeCaptureCannotBeManufacturedFromResult закрыто отклоняет exported metadata без isolated proof.
func TestNativeCaptureCannotBeManufacturedFromResult(t *testing.T) {
	input := model.Input{Provider: "opencode", Mode: runtimecontract.RunnerModeTurn, OrganizationRef: "org_synthetic", ProjectRef: "prj_synthetic", AgentRef: "agt_synthetic", SessionRef: "ses_synthetic", TurnRef: "trn_synthetic", LeaseRef: "lea_synthetic", LeaseGeneration: 2, Attempt: 1, ExecutionBindingDigest: strings.Repeat("a", 64), RuntimeRevisionDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64)}
	path, _ := runtimecontract.NativeSessionExportPath("ses_native", 1)
	checkpoint := runtimecontract.NativeSessionCheckpoint{SessionID: "ses_native", Version: runtimecontract.OpenCodeVersion, Generation: 1, RelativePath: path, SHA256: strings.Repeat("d", 64), SizeBytes: 800}
	result := Result{NativeSession: &checkpoint}
	if result.HasVerifiedNativeCheckpoint(input) {
		t.Fatal("exported native tuple manufactured a verified checkpoint")
	}
	proof := nativeCaptureProof{Schema: nativeCaptureSchema, ExecutionBindingDigest: input.ExecutionBindingDigest, RuntimeRevisionDigest: input.RuntimeRevisionDigest, InputDigest: input.InputDigest, LeaseRef: input.LeaseRef, LeaseGeneration: input.LeaseGeneration, OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef, SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt, Checkpoint: checkpoint}
	result.nativeCapture = &proof
	if result.HasVerifiedNativeCheckpoint(input) {
		t.Fatal("unsealed decoded native proof accepted")
	}
	// Only the private seal is set for the tuple-negative oracle; this is not a file-capture test.
	// Private seal задаётся лишь для tuple-negative oracle; это не проверка захвата настоящего файла.
	proof.sealed = true
	if !proof.bound(input) {
		t.Fatal("synthetic proof tuple rejected")
	}
	for _, test := range []struct {
		name   string
		change func(*model.Input)
	}{
		{"organization", func(value *model.Input) { value.OrganizationRef = "org_foreign" }},
		{"project", func(value *model.Input) { value.ProjectRef = "prj_foreign" }},
		{"agent", func(value *model.Input) { value.AgentRef = "agt_foreign" }},
		{"session", func(value *model.Input) { value.SessionRef = "ses_foreign" }},
		{"turn", func(value *model.Input) { value.TurnRef = "trn_foreign" }},
		{"attempt", func(value *model.Input) { value.Attempt++ }},
		{"lease-generation", func(value *model.Input) { value.LeaseGeneration++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			foreign := input
			test.change(&foreign)
			if result.HasVerifiedNativeCheckpoint(foreign) {
				t.Fatal("native proof escaped its execution scope")
			}
		})
	}
	checkpoint.SHA256 = strings.Repeat("e", 64)
	if result.HasVerifiedNativeCheckpoint(input) {
		t.Fatal("native bytes changed behind the sealed proof")
	}
}
