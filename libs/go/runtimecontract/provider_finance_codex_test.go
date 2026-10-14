package runtimecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

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
