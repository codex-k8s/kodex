package entity

import (
	"fmt"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestFinancialOverviewRanksCompleteSetWithBoundedStorage scans 6,202 synthetic accounts, verifies
// global ranking with three display rows and deterministic ties, preserves the exact independent
// paid-credit balance string, and checks small sets and stale observations. It does not exercise
// provider billing or launch gating.
// TestFinancialOverviewRanksCompleteSetWithBoundedStorage просматривает 6 202 синтетических аккаунта,
// проверяет общее ранжирование с тремя строками и устойчивым порядком при равенстве, сохраняет точную
// отдельную строку платного баланса и проверяет малые наборы и старые наблюдения. Тест не обращается к
// оплате провайдера и не проверяет допуск запуска.
func TestFinancialOverviewRanksCompleteSetWithBoundedStorage(t *testing.T) {
	at := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	summary := func(used int32) *ProviderFinancialSummary {
		return &ProviderFinancialSummary{Observation: runtimecontract.ProviderFinancialObservation{Sequence: 1, State: "AVAILABLE", Windows: []runtimecontract.ProviderFinancialWindow{{UsedPercent: new(int32(used))}}}, ReceivedAt: at, Version: 1}
	}
	result := ProviderFinancialOverview{ObservedAt: at, Accounts: []ProviderFinancialDisplayAccount{}}
	for i := 0; i < 6200; i++ {
		result.Add(ProviderFinancialDisplayAccount{Ref: fmt.Sprintf("acct_%08d", i), Name: "Fixture", FinancialSummary: summary(50)})
	}
	paid := summary(100)
	balance := "59999.999999999999999999"
	paid.Observation.Credits = &runtimecontract.ProviderFinancialCredits{HasCredits: new(true), Balance: &balance}
	result.Add(ProviderFinancialDisplayAccount{Ref: "acct_zzzz_paid", Name: "Paid fixture", FinancialSummary: paid})
	result.Add(ProviderFinancialDisplayAccount{Ref: "acct_zzzz_unknown", Name: "Unknown fixture"})
	if result.Total != 6202 || result.Exhausted != 1 || result.Unknown != 1 || len(result.Accounts) != 3 || result.Accounts[0].Ref != "acct_zzzz_paid" || result.Accounts[1].Ref != "acct_00000000" || result.Accounts[2].Ref != "acct_00000001" || *result.Accounts[0].FinancialSummary.Observation.Credits.Balance != balance {
		t.Fatal("complete rank or independent paid balance was lost")
	}
	for count := 0; count <= 3; count++ {
		result := ProviderFinancialOverview{ObservedAt: at}
		for i := 0; i < count; i++ {
			result.Add(ProviderFinancialDisplayAccount{Ref: fmt.Sprint(i), Name: "Fixture"})
		}
		if len(result.Accounts) != count || result.Total != int64(count) || result.Unknown != int64(count) {
			t.Fatal("small summary bounds")
		}
	}
	old := summary(100)
	old.ReceivedAt = at.Add(-6 * time.Minute)
	if _, known := FinancialRemaining(ProviderFinancialDisplayAccount{FinancialSummary: old}, at); known {
		t.Fatal("stale window became a rank measurement")
	}
}
