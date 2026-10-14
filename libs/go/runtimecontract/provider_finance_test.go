package runtimecontract

import "testing"

func TestFinancialObservationPreservesUnknownAndPaid(t *testing.T) {
	balance := "59999.12345678901234567890123456789"
	ordinary := false
	value := ProviderFinancialObservation{Sequence: 1, State: "AVAILABLE", Windows: []ProviderFinancialWindow{{UsedPercent: 100}}, Credits: &ProviderFinancialCredits{HasCredits: new(true), Balance: &balance}, OrdinaryUsageAllowed: &ordinary}
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
func TestFinancialObservationBounds(t *testing.T) {
	invalidBalance := "1e9"
	invalidReset := int64(-1)
	invalidDuration := int64(0)
	tests := []ProviderFinancialObservation{
		{State: "AVAILABLE"}, {Sequence: 65, State: "AVAILABLE"}, {Sequence: 1, State: "UNKNOWN"},
		{Sequence: 1, State: "UNAVAILABLE", Credits: &ProviderFinancialCredits{}},
		{Sequence: 1, State: "AVAILABLE", Windows: []ProviderFinancialWindow{{UsedPercent: 101}}},
		{Sequence: 1, State: "AVAILABLE", Windows: []ProviderFinancialWindow{{UsedPercent: -1}}},
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
