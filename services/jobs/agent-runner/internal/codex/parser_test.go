package codex

import (
	"encoding/json"
	"testing"
)

const (
	// testThreadID identifies the synthetic thread shared by protocol fixtures; it grants no session access.
	// testThreadID обозначает синтетический thread protocol fixtures и не даёт доступ к сессии.
	testThreadID = "01980000-0000-7000-8000-000000000001"
	// testTurnID identifies a distinct synthetic turn so fixtures can detect thread/turn binding mistakes.
	// testTurnID обозначает отдельный синтетический ход для выявления ошибок thread/turn binding.
	testTurnID = "01980000-0000-7000-8000-000000000002"
)

// TestOnlyServerOverloadedIsCapacity distinguishes overload from quota/auth/policy and malformed
// provider error variants, checking both their closed classification and the capacity predicate.
// TestOnlyServerOverloadedIsCapacity отличает перегрузку от quota/auth/policy и неверных
// provider error variants, проверяя закрытую классификацию и capacity predicate.
func TestOnlyServerOverloadedIsCapacity(t *testing.T) {
	for _, code := range []string{"server_overloaded", "usage_limit_exceeded", "unauthorized", "policy_denied", "unknown"} {
		if CapacityFailure(code) != (code == "server_overloaded") {
			t.Fatal("capacity classification changed")
		}
	}
}

// TestTerminalPresentationKeepsCapacityQuotaAndPolicySeparate checks explicit outcome/action
// mappings for overload, quota, auth, policy and invalid info, requiring nonempty presentation text.
// TestTerminalPresentationKeepsCapacityQuotaAndPolicySeparate проверяет явные outcome/action
// mappings перегрузки, quota, auth, policy и invalid info с непустым presentation text.
func TestTerminalPresentationKeepsCapacityQuotaAndPolicySeparate(t *testing.T) {
	tests := []struct {
		code, outcome, action string
	}{
		{"server_overloaded", "FAILED", "RETRY_LATER"},
		{"usage_limit_exceeded", "BLOCKED", "CHECK_PROVIDER_QUOTA"},
		{"unauthorized", "BLOCKED", "REAUTH_DEVICE_CODE"},
		{"cyber_policy", "BLOCKED", "REVIEW_POLICY"},
		{"provider_error_info_invalid", "FAILED", "RETRY_FRESH_TURN"},
	}
	for _, test := range tests {
		outcome, markdown, action := TerminalPresentation(test.code)
		if outcome != test.outcome || action != test.action || markdown == "" {
			t.Fatalf("unexpected terminal mapping for %q: %q %q %q", test.code, outcome, action, markdown)
		}
	}
}

// raw converts a trusted literal to RawMessage without parsing or validating its bytes.
// raw преобразует доверенный литерал в RawMessage без разбора и проверки его байтов.
func raw(value string) json.RawMessage {
	return json.RawMessage(value)
}
