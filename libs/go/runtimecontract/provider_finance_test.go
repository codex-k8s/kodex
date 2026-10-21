package runtimecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestFinancialObservationPreservesUnknownAndPaid validates precise balances and unknown or unlimited credits without treating exhausted included usage as a local paid-work prohibition.
// TestFinancialObservationPreservesUnknownAndPaid проверяет точные балансы и неизвестные либо безлимитные кредиты, не превращая исчерпание включённого расхода в локальный запрет платной работы.
func TestFinancialObservationPreservesUnknownAndPaid(t *testing.T) {
	balance := "59999.12345678901234567890123456789"
	ordinary := false
	value := ProviderFinancialObservation{Sequence: 1, State: "AVAILABLE", Windows: []ProviderFinancialWindow{{UsedPercent: new(int32(100))}}, Credits: &ProviderFinancialCredits{HasCredits: new(true), Balance: &balance}, OrdinaryUsageAllowed: &ordinary}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	if *value.Credits.Balance != balance {
		t.Fatal("balance precision changed")
	}
	// Included exhaustion и ordinary flag не являются локальным запретом paid continuation.
	value.Credits.Balance = nil
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	value.Credits.Unlimited = new(true)
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
}

// TestFinancialObservationBounds rejects unsupported states, out-of-range measurements and fabricated restrictions, while accepting a measurement-free unavailable observation.
// TestFinancialObservationBounds отклоняет неподдерживаемые состояния, измерения вне границ и выдуманные ограничения, принимая недоступное наблюдение без измерений.
func TestFinancialObservationBounds(t *testing.T) {
	invalidBalance := "1e9"
	invalidReset := int64(-1)
	invalidDuration := int64(0)
	tests := []ProviderFinancialObservation{
		{State: "AVAILABLE"},
		{Sequence: 65, State: "AVAILABLE"},
		{Sequence: 1, State: "UNKNOWN"},
		{Sequence: 1, State: "UNAVAILABLE", Credits: &ProviderFinancialCredits{}},
		{Sequence: 1, State: "AVAILABLE", Windows: []ProviderFinancialWindow{{UsedPercent: new(int32(101))}}},
		{Sequence: 1, State: "AVAILABLE", Windows: []ProviderFinancialWindow{{UsedPercent: new(int32(-1))}}},
		{Sequence: 1, State: "AVAILABLE", Windows: []ProviderFinancialWindow{{ResetsAt: &invalidReset}}},
		{Sequence: 1, State: "AVAILABLE", Windows: []ProviderFinancialWindow{{DurationMinutes: &invalidDuration}}},
		{Sequence: 1, State: "AVAILABLE", Credits: &ProviderFinancialCredits{Balance: &invalidBalance}},
		{Sequence: 1, State: "AVAILABLE", Restriction: "invented"},
	}
	for index, value := range tests {
		if value.Validate() == nil {
			t.Fatalf("case %d accepted invalid measurement", index)
		}
	}
	if err := (ProviderFinancialObservation{Sequence: 1, State: "UNAVAILABLE"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

// TestFinancialProjectionStrictReadPreservesPaidAndPresence round-trips precise credit data and rejects ambiguous, oversized or unknown projections without returning partial data or private diagnostics.
// TestFinancialProjectionStrictReadPreservesPaidAndPresence точно сохраняет данные кредитов и отклоняет неоднозначные, чрезмерные или неизвестные проекции без частичного результата и частных диагностических данных.
func TestFinancialProjectionStrictReadPreservesPaidAndPresence(t *testing.T) {
	raw := []byte(`{"sequence":64,"state":"AVAILABLE","windows":[{"usedPercent":100}],"credits":{"hasCredits":true,"balance":"59999.123456789012345678901"},"ordinaryUsageAllowed":false}`)
	observation, err := ParseProviderFinancialObservation(raw)
	if err != nil || observation.Credits == nil || observation.Credits.Unlimited != nil || observation.Credits.Balance == nil || *observation.Credits.Balance != "59999.123456789012345678901" {
		t.Fatal("strict projection lost paid precision or unknown presence")
	}
	encoded, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseProviderFinancialObservation(encoded); err != nil {
		t.Fatal("producer projection failed strict roundtrip")
	}
	for _, raw := range []string{
		`{"sequence":1,"state":"AVAILABLE","accountId":"synthetic-private"}`,
		`{"sequence":1,"state":"AVAILABLE","credits":{"balance":"59999","currency":"USD"}}`,
		`{"sequence":1,"sequence":2,"state":"AVAILABLE"}`,
		`{"sequence":1,"state":"AVAILABLE","windows":[{"usedPercent":null}]}`,
		`{"sequence":1,"state":"UNAVAILABLE","credits":{"hasCredits":true}}`,
		`{"sequence":1,"state":"AVAILABLE"} {}`,
		"null",
		strings.Repeat(" ", 4097),
	} {
		observation, err := ParseProviderFinancialObservation([]byte(raw))
		if err == nil || observation.State != "" || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("invalid projection was partially read or leaked diagnostics")
		}
	}
}
