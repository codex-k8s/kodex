package runtimecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

// diagnosticFixture constructs matching synthetic input and request-failure pins
// without a real provider account, external request or durable business receipt.
// diagnosticFixture создаёт согласованные синтетические привязки отказа запроса
// без реального аккаунта, внешнего вызова и устойчивой бизнесовой квитанции.
func diagnosticFixture() (RunnerInput, ProviderFailureDiagnostic) {
	input := RunnerInput{SessionRef: "ses_fixture1", TurnRef: "trn_fixture1", Attempt: 1,
		RuntimeRevisionDigest: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), ExecutionBindingDigest: strings.Repeat("c", 64)}
	return input, ProviderFailureDiagnostic{Schema: ProviderFailureDiagnosticSchema,
		RuntimeRevisionDigest: input.RuntimeRevisionDigest, InputDigest: input.InputDigest, ExecutionBindingDigest: input.ExecutionBindingDigest,
		SessionRef: input.SessionRef, TurnRef: input.TurnRef, Attempt: input.Attempt,
		Kind: "REQUEST_FAILURE", Stage: "TERMINAL_WAIT", Class: "PROVIDER", Detail: "NOTIFICATION_INVALID",
		Notification: "thread/tokenUsage/updated", NotificationError: "TOKEN_USAGE_LAST_EXCEEDS_TOTAL", AccountRead: "NONE"}
}

// TestProviderDiagnosticCanonicalWireAndBinding checks wire round-trip, rejects
// every foreign binding coordinate and prevents failure metadata on success.
// TestProviderDiagnosticCanonicalWireAndBinding проверяет wire round-trip,
// отклоняет чужие привязки и запрещает диагностику отказа при успехе.
func TestProviderDiagnosticCanonicalWireAndBinding(t *testing.T) {
	input, value := diagnosticFixture()
	raw, err := json.Marshal(value)
	var decoded ProviderFailureDiagnostic
	if err != nil || json.Unmarshal(raw, &decoded) != nil || decoded != value || !decoded.Matches(input) {
		t.Fatal("closed diagnostic lost exact wire binding")
	}
	for _, change := range []func(*RunnerInput){
		func(i *RunnerInput) { i.InputDigest = strings.Repeat("d", 64) },
		func(i *RunnerInput) { i.ExecutionBindingDigest = strings.Repeat("d", 64) },
		func(i *RunnerInput) { i.RuntimeRevisionDigest = strings.Repeat("d", 64) },
		func(i *RunnerInput) { i.SessionRef = "ses_foreign" },
		func(i *RunnerInput) { i.TurnRef = "trn_foreign" },
		func(i *RunnerInput) { i.Attempt++ },
	} {
		other := input
		change(&other)
		if decoded.Matches(other) {
			t.Fatal("foreign diagnostic acquired exact binding")
		}
	}
	completion := RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: 1, SafeErrorCode: "PROVIDER_UNAVAILABLE", ProviderDiagnostic: &value}
	if completion.Validate() != nil {
		t.Fatal("failed completion rejected diagnostic")
	}
	completion.Success, completion.ResultSummary = true, "result"
	if completion.Validate() == nil {
		t.Fatal("success accepted failure observation")
	}
}

// TestProviderDiagnosticRejectsMalformedOrPrivateWire exercises ambiguous keys,
// nulls, aliases, oversize input and private sentinels without reflecting content.
// TestProviderDiagnosticRejectsMalformedOrPrivateWire проверяет неоднозначные ключи,
// null, иные имена, превышение размера и закрытый текст без его отражения.
func TestProviderDiagnosticRejectsMalformedOrPrivateWire(t *testing.T) {
	_, value := diagnosticFixture()
	raw, _ := json.Marshal(value)
	for _, candidate := range []string{
		`null`, `{}`, string(raw) + ` {}`, `{"schema":"duplicate",` + string(raw[1:]),
		strings.Replace(string(raw), `"stage":`, `"private":"PRIVATE_SENTINEL","stage":`, 1),
		strings.Replace(string(raw), `"stage":`, `"Stage":`, 1),
		strings.Replace(string(raw), `"attempt":1`, `"attempt":null`, 1),
		strings.Replace(string(raw), `"terminal_code":""`, `"terminal_code":null`, 1),
		strings.Replace(string(raw), `TERMINAL_WAIT`, `PRIVATE_SENTINEL`, 1),
		strings.Replace(string(raw), `thread/tokenUsage/updated`, `item/reasoning/textDelta`, 1),
		strings.Repeat(" ", 4097),
	} {
		var decoded ProviderFailureDiagnostic
		if err := json.Unmarshal([]byte(candidate), &decoded); err == nil || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
			t.Fatal("malformed diagnostic escaped closed decoder")
		}
	}
}

// TestProviderDiagnosticTerminalAndCrossFieldGuards accepts an exact terminal
// failure and rejects contradictory stages, classes and out-of-catalog codes.
// TestProviderDiagnosticTerminalAndCrossFieldGuards принимает точный terminal-отказ
// и отклоняет противоречивые этапы, классы и коды вне каталога.
func TestProviderDiagnosticTerminalAndCrossFieldGuards(t *testing.T) {
	_, value := diagnosticFixture()
	value.Kind, value.Stage, value.Detail = "TERMINAL_FAILURE", "TERMINAL_RESULT", "NONE"
	value.Notification, value.NotificationError, value.TerminalCode = "NONE", "NONE", "provider_interrupted"
	if value.Validate() != nil {
		t.Fatal("valid closed terminal failure was rejected")
	}
	for _, mutate := range []func(*ProviderFailureDiagnostic){
		func(v *ProviderFailureDiagnostic) { v.TerminalCode = "PRIVATE_SENTINEL" },
		func(v *ProviderFailureDiagnostic) { v.Kind = "UNKNOWN" },
		func(v *ProviderFailureDiagnostic) { v.Class = "UNKNOWN" },
		func(v *ProviderFailureDiagnostic) { v.Stage = "TURN_START" },
		func(v *ProviderFailureDiagnostic) { v.AccountRead = "SHUTDOWN" },
		func(v *ProviderFailureDiagnostic) { v.Schema = "legacy" },
	} {
		other := value
		mutate(&other)
		if err := other.Validate(); err == nil || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
			t.Fatal("invalid terminal diagnostic accepted or reflected")
		}
	}
}

// TestProviderDiagnosticResumeSourceClosedReasons verifies every allowed resume
// reason at its exact stage/class and rejects cross-field substitutions.
// TestProviderDiagnosticResumeSourceClosedReasons проверяет каждую разрешённую
// причину resume в точном этапе/классе и отклоняет подмену связанных полей.
func TestProviderDiagnosticResumeSourceClosedReasons(t *testing.T) {
	input, base := diagnosticFixture()
	base.Stage, base.Detail, base.Notification, base.NotificationError = "THREAD_READ", "NONE", "NONE", "NONE"
	for _, detail := range strings.Fields("RESUME_SOURCE_SCHEMA RESUME_SOURCE_ID RESUME_SOURCE_LOCATOR RESUME_SOURCE_OPEN RESUME_SOURCE_METADATA RESUME_SOURCE_IDENTITY") {
		value := base
		value.Detail = detail
		raw, err := json.Marshal(value)
		var decoded ProviderFailureDiagnostic
		if err != nil || json.Unmarshal(raw, &decoded) != nil || !decoded.Matches(input) || decoded != value {
			t.Fatal("valid resume source reason was lost at consumer")
		}
		for _, mutate := range []func(*ProviderFailureDiagnostic){
			func(v *ProviderFailureDiagnostic) { v.Stage = "ARCHIVE_CAPTURE" },
			func(v *ProviderFailureDiagnostic) { v.Class = "AUTHENTICATION" },
			func(v *ProviderFailureDiagnostic) { v.Kind = "TERMINAL_FAILURE" },
			func(v *ProviderFailureDiagnostic) { v.Detail = "RESUME_SOURCE_PRIVATE_SENTINEL" },
			func(v *ProviderFailureDiagnostic) { v.Notification = "UNKNOWN" },
			func(v *ProviderFailureDiagnostic) { v.AccountRead = "SHUTDOWN" },
			func(v *ProviderFailureDiagnostic) { v.TerminalCode = "provider_interrupted" },
		} {
			other := value
			mutate(&other)
			if other.Validate() == nil {
				t.Fatal("resume diagnostic cross-field guard accepted foreign context")
			}
		}
	}
}
