package callback

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestNativeToolIdentitySurvivesLeaseReplacement keeps a receipt key stable only within its exact native session.
// It does not prove current tool authorization, which the owner independently rechecks on every invocation.
// TestNativeToolIdentitySurvivesLeaseReplacement сохраняет receipt key лишь внутри exact native session.
// Current tool authorization независимо проверяется владельцем при каждом вызове.
func TestNativeToolIdentitySurvivesLeaseReplacement(t *testing.T) {
	input := runtimecontract.RunnerInput{Provider: "opencode", SessionRef: "ses_syntheticA", LeaseRef: "lea_first"}
	id := json.RawMessage(`"ncall_` + strings.Repeat("a", 64) + `"`)
	first := runtimeToolMutationKey(input, id)
	input.LeaseRef = "lea_replacement"
	if first != runtimeToolMutationKey(input, id) {
		t.Fatal("native receipt identity changed with lease")
	}
	input.SessionRef = "ses_syntheticB"
	if first == runtimeToolMutationKey(input, id) {
		t.Fatal("native receipt escaped session")
	}
	for _, id := range []json.RawMessage{json.RawMessage(`"ncall_invalid"`), json.RawMessage(`"rpc-1"`), json.RawMessage(`1`)} {
		if runtimeToolMutationKey(input, id) != stableKey(input.LeaseRef, string(id)) {
			t.Fatal("ordinary transport identity acquired native semantics")
		}
	}
}
