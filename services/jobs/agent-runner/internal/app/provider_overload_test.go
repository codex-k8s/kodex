package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestProviderCompletionOverloadClassification preserves the exact structured
// capacity reason while keeping quota, auth, transport, generic unavailable and
// caller-assigned JSON on their previous paths. Diagnostics here are isolated
// fixtures; broker tests separately prove the private exact-input accessor.
// TestProviderCompletionOverloadClassification сохраняет точную структурированную
// причину capacity, оставляя квоту, auth, transport, общую unavailable и JSON
// вызывающего на прежних путях. Диагностика здесь — изолированная оснастка;
// broker-тесты отдельно проверяют private exact-input accessor.
func TestProviderCompletionOverloadClassification(t *testing.T) {
	base := runtimecontract.ProviderFailureDiagnostic{Schema: runtimecontract.ProviderFailureDiagnosticSchema,
		RuntimeRevisionDigest: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), ExecutionBindingDigest: strings.Repeat("c", 64),
		SessionRef: "ses_fixture1", TurnRef: "trn_fixture1", Attempt: 1, Kind: "TERMINAL_FAILURE", Stage: "TERMINAL_RESULT",
		Class: "PROVIDER", Detail: "NONE", Notification: "NONE", NotificationError: "NONE", AccountRead: "NONE", TerminalCode: "server_overloaded"}
	for _, name := range []string{"exact", "missing", "success", "quota", "auth", "transport", "generic", "timeout", "invalid", "contradiction"} {
		t.Run(name, func(t *testing.T) {
			value := base
			diagnostic := &value
			outcome, code, want := "FAILED", "server_overloaded", "PROVIDER_UNAVAILABLE"
			switch name {
			case "exact":
				want = runtimecontract.ProviderOverloadedFailureCode
			case "missing":
				diagnostic = nil
			case "success":
				outcome = "SUCCEEDED"
			case "quota":
				code, value.TerminalCode, want = "usage_limit_exceeded", "usage_limit_exceeded", "PROVIDER_RATE_LIMITED"
			case "auth":
				code, value.TerminalCode, want = "unauthorized", "unauthorized", "PROVIDER_AUTH_REJECTED"
			case "transport":
				code, value.TerminalCode = "provider_transport_failure", "provider_transport_failure"
			case "generic":
				code = "RUNTIME_PROVIDER_UNAVAILABLE"
			case "timeout":
				code, want = "RUNTIME_TIMEOUT", "RUNTIME_TIMEOUT"
			case "invalid":
				value.Detail = "PRIVATE_SENTINEL"
			case "contradiction":
				value.TerminalCode = "usage_limit_exceeded"
			}
			if got := providerCompletionFailureCode(outcome, code, diagnostic); got != want {
				t.Fatalf("classification=%s want=%s", got, want)
			}
		})
	}
	var forged codex.Result
	if json.Unmarshal([]byte(`{"Outcome":"FAILED","FailureCode":"server_overloaded","providerDiagnostic":{"terminal_code":"server_overloaded"}}`), &forged) != nil {
		t.Fatal("decode isolated public result")
	}
	if got := providerCompletionFailureCode(forged.Outcome, forged.FailureCode, forged.ProviderDiagnostic(model.Input{})); got != "PROVIDER_UNAVAILABLE" {
		t.Fatal("public result JSON forged a capacity observation")
	}
}
