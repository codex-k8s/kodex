package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// TestInitializationCompletionUsesClosedOwnerSummary checks the real pre-effect validator.
// Legacy failure summaries remain valid; no init code permits success or arbitrary content.
// TestInitializationCompletionUsesClosedOwnerSummary проверяет настоящий pre-effect validator.
// Legacy failure summaries допустимы; init code не разрешает success либо произвольный контент.
func TestInitializationCompletionUsesClosedOwnerSummary(t *testing.T) {
	good := command.CompleteExecutionInput{SafeErrorCode: runtimecontract.InitializationFailureCode, ResultSummary: runtimecontract.InitializationFailureSummary("ARTIFACT_TRANSFER", "AUTHORITY_REJECTED")}
	if validateRuntimeCompletion(good) != nil {
		t.Fatal("closed init completion rejected")
	}
	for _, change := range []func(*command.CompleteExecutionInput){
		func(v *command.CompleteExecutionInput) { v.Success = true },
		func(v *command.CompleteExecutionInput) { v.ResultSummary = "PRIVATE_SENTINEL" },
		func(v *command.CompleteExecutionInput) { v.ResultSummary = "i18n:RUNTIME_INIT_PRIVATE_PRIVATE" },
		func(v *command.CompleteExecutionInput) { v.SafeErrorCode = "RUNTIME_UNAVAILABLE" },
	} {
		bad := good
		change(&bad)
		if validateRuntimeCompletion(bad) == nil {
			t.Fatal("inconsistent initialization completion accepted")
		}
	}
	legacy := command.CompleteExecutionInput{SafeErrorCode: "RUNTIME_UNAVAILABLE", ResultSummary: "i18n:RUNTIME_WORKLOAD_EXITED"}
	if validateRuntimeCompletion(legacy) != nil {
		t.Fatal("legacy completion changed")
	}
}
