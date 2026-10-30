package runtimecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestProviderOverloadRequiresExactStructuredFailure checks the additive completion
// code against missing, contradictory and cross-attempt diagnostics; legacy generic
// failures remain decodable and do not acquire an overload signal from text.
// TestProviderOverloadRequiresExactStructuredFailure проверяет дополнительный код
// completion при отсутствии, противоречиях и подмене попытки в диагностике; прежние
// общие ошибки читаются и не получают признак перегрузки из текста.
func TestProviderOverloadRequiresExactStructuredFailure(t *testing.T) {
	input, diagnostic := diagnosticFixture()
	diagnostic.Kind, diagnostic.Stage, diagnostic.Detail = "TERMINAL_FAILURE", "TERMINAL_RESULT", "NONE"
	diagnostic.Notification, diagnostic.NotificationError, diagnostic.AccountRead = "NONE", "NONE", "NONE"
	diagnostic.TerminalCode = "server_overloaded"
	for _, name := range []string{"exact", "missing", "success", "quota", "auth", "transport", "request", "attempt", "revision", "invalid detail", "legacy", "text"} {
		t.Run(name, func(t *testing.T) {
			value := diagnostic
			request := RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt,
				SafeErrorCode: ProviderOverloadedFailureCode, ProviderDiagnostic: &value, ResultSummary: "i18n:" + ProviderOverloadedFailureCode}
			want := name == "exact" || name == "legacy" || name == "text"
			switch name {
			case "missing":
				request.ProviderDiagnostic = nil
			case "success":
				request.Success = true
			case "quota":
				value.TerminalCode = "usage_limit_exceeded"
			case "auth":
				value.TerminalCode = "unauthorized"
			case "transport":
				value.TerminalCode = "provider_transport_failure"
			case "request":
				value.Kind, value.Stage, value.TerminalCode = "REQUEST_FAILURE", "TERMINAL_WAIT", ""
			case "attempt":
				value.Attempt++
			case "revision":
				value.RuntimeRevisionDigest = strings.Repeat("d", 64)
			case "invalid detail":
				value.Detail = "PRIVATE_SENTINEL"
			case "legacy":
				request.SafeErrorCode = "PROVIDER_UNAVAILABLE"
			case "text":
				request.SafeErrorCode, request.ProviderDiagnostic = "PROVIDER_UNAVAILABLE", nil
				request.ResultSummary = "server_overloaded"
			}
			raw, err := json.Marshal(request)
			var decoded RunnerCompletionRequest
			if err != nil {
				t.Fatal("encode isolated completion")
			}
			valid := json.Unmarshal(raw, &decoded) == nil && decoded.Validate() == nil
			if valid != want {
				t.Fatalf("completion validity=%t want=%t", valid, want)
			}
			if valid && (decoded.SafeErrorCode == ProviderOverloadedFailureCode) != (name == "exact") {
				t.Fatal("completion invented an overload classification")
			}
		})
	}
}
