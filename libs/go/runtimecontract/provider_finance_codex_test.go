package runtimecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCodexFinancialSupportedSubset preserves supported credit precision and sparse-field presence while proving private provider metadata is absent from the serialized projection.
// TestCodexFinancialSupportedSubset сохраняет точность кредитов и присутствие разреженных полей, подтверждая отсутствие частных метаданных провайдера в сериализованной проекции.
func TestCodexFinancialSupportedSubset(t *testing.T) {
	raw := json.RawMessage(`{"accountId":"discard-me","ordinaryUsageAllowed":false,"rateLimits":{"limitId":"codex","primary":{"usedPercent":100,"windowDurationMins":10080,"resetsAt":1800000000},"credits":{"hasCredits":true,"unlimited":false,"balance":"59999.1234567890123456789"},"spendControlReached":false,"rateLimitReachedType":"rate_limit_reached"},"rateLimitUpsell":{"private":"discard-me"}}`)
	value := ParseCodexFinancialObservation(raw, 1)
	if value.State != "AVAILABLE" || len(value.Windows) != 1 || value.Windows[0].UsedPercent == nil || *value.Windows[0].UsedPercent != 100 || value.OrdinaryUsageAllowed == nil || *value.OrdinaryUsageAllowed || value.Credits == nil || value.Credits.Balance == nil || *value.Credits.Balance != "59999.1234567890123456789" {
		t.Fatalf("supported measurement was lost: %#v", value)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "discard-me") || strings.Contains(string(encoded), "accountId") {
		t.Fatal("private fields escaped")
	}
	// Sparse/null остаётся unknown; notification никогда не назначает account.
	value = ParseCodexFinancialObservation(json.RawMessage(`{"rateLimits":{},"ordinaryUsageAllowed":null}`), 2)
	if value.State != "AVAILABLE" || value.Credits != nil || value.OrdinaryUsageAllowed != nil || len(value.Windows) != 0 {
		t.Fatal("sparse response invented data")
	}
}

// TestCodexFinancialRejectsMalformedAndAmbiguous leaves invalid envelopes unavailable and selects the exact codex bucket without conflating unlimited credits with included limits.
// TestCodexFinancialRejectsMalformedAndAmbiguous оставляет некорректные конверты недоступными и выбирает точный раздел codex, не смешивая безлимитные кредиты с включёнными лимитами.
func TestCodexFinancialRejectsMalformedAndAmbiguous(t *testing.T) {
	cases := []string{
		`{}`, `{"rateLimits":null}`,

		`{"rateLimits":{},"rateLimits":{}}`, `{"rateLimits":{"primary":{"usedPercent":1,"usedPercent":100}}}`,
		`{"rateLimits":{"limitId":"other"}}`, `{"rateLimits":{},"rateLimitsByLimitId":{"other":{}}}`,
		`{"rateLimits":{}} {}`, strings.Repeat(" ", 64<<10+1),
	}
	for index, raw := range cases {
		value := ParseCodexFinancialObservation(json.RawMessage(raw), 1)
		if value.State != "UNAVAILABLE" || value.Validate() != nil {
			t.Fatalf("case %d did not remain unavailable", index)
		}
	}
	value := ParseCodexFinancialObservation(json.RawMessage(`{"rateLimits":{"limitId":"other"},"rateLimitsByLimitId":{"codex":{"credits":{"hasCredits":true,"unlimited":true,"balance":null}}}}`), 1)
	if value.State != "AVAILABLE" || (value.Credits.Unlimited == nil || !*value.Credits.Unlimited) || len(value.Windows) != 0 {
		t.Fatal("codex bucket/unlimited credits conflated with included limits")
	}
}

// TestFinancialPresenceFieldsRemainIndependent preserves measured zero and false values while keeping malformed or null sibling fields absent after serialization.
// TestFinancialPresenceFieldsRemainIndependent сохраняет измеренные ноль и false, оставляя некорректные или null соседние поля отсутствующими после сериализации.
func TestFinancialPresenceFieldsRemainIndependent(t *testing.T) {
	value := ParseCodexFinancialObservation(json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":0,"windowDurationMins":"invalid","resetsAt":null},"secondary":{"usedPercent":100,"resetsAt":1800000000},"credits":{"hasCredits":false,"unlimited":null,"balance":"malformed"},"spendControlReached":false},"ordinaryUsageAllowed":null}`), 1)
	if value.State != "AVAILABLE" || len(value.Windows) != 2 || value.Windows[0].UsedPercent == nil || *value.Windows[0].UsedPercent != 0 || value.Windows[0].DurationMinutes != nil || value.Windows[0].ResetsAt != nil || value.Windows[1].ResetsAt == nil || value.Credits == nil || value.Credits.HasCredits == nil || *value.Credits.HasCredits || value.Credits.Unlimited != nil || value.Credits.Balance != nil || value.SpendControlReached == nil || *value.SpendControlReached || value.OrdinaryUsageAllowed != nil {
		t.Fatal("independent presence was lost")
	}
	encoded, err := json.Marshal(value)
	if err != nil || strings.Contains(string(encoded), `"unlimited":false`) || strings.Contains(string(encoded), `"balance":`) {
		t.Fatal("unknown became a measurement")
	}
}

// TestCodexFinancialUnknownWireFieldsCloseProjection makes unknown fields close the measurement projection while allowing known presentation metadata to be discarded safely.
// TestCodexFinancialUnknownWireFieldsCloseProjection закрывает проекцию измерений при неизвестных полях, разрешая безопасно отбрасывать известные метаданные отображения.
func TestCodexFinancialUnknownWireFieldsCloseProjection(t *testing.T) {
	for _, raw := range []string{
		`{"rateLimits":{},"paidContinuationAllowed":true}`,
		`{"rateLimits":{"remaining":0}}`,
		`{"rateLimits":{"primary":{"usedPercent":100,"resetAt":1800000000}}}`,
		`{"rateLimits":{"credits":{"hasCredits":true,"balance":"59999","currency":"USD"}}}`,
		`{"rateLimitsByLimitId":{"codex":{"credits":{"unlimited":true,"paid":true}}},"rateLimits":{}}`,
	} {
		observation := ParseCodexFinancialObservation(json.RawMessage(raw), 3)
		if observation.State != "UNAVAILABLE" || observation.Sequence != 3 || observation.Validate() != nil {
			t.Fatal("unknown wire field produced a measurement")
		}
	}
	// Supported metadata отбрасываются, не расширяя публичный subset.
	observation := ParseCodexFinancialObservation(json.RawMessage(`{"rateLimits":{"limitId":"codex","limitName":null,"normalModelSlug":null,"planType":"pro","individualLimit":null,"primary":{"usedPercent":100},"credits":{"hasCredits":true,"balance":"59999"}},"rateLimitResetCredits":null,"rateLimitUpsell":null,"accountId":null,"ordinaryUsageAllowed":null}`), 4)
	if observation.State != "AVAILABLE" || observation.Credits == nil || observation.Credits.Balance == nil || *observation.Credits.Balance != "59999" {
		t.Fatal("supported metadata blocked independent measurements")
	}
}
