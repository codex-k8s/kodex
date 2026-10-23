package providerfinancewire

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestObservationToProtoPreservesIndependentMeasurements checks exact decimal
// balance, false/true presence and ordered window pins without inferring that
// credits override an ordinary-usage restriction or spend-control decision.
// TestObservationToProtoPreservesIndependentMeasurements проверяет точный
// десятичный баланс, наличие false/true и порядок окон без вывода, что кредиты
// отменяют ограничение обычного расхода либо решение spend control.
func TestObservationToProtoPreservesIndependentMeasurements(t *testing.T) {
	ordinary, spending, hasCredits, unlimited := false, true, true, false
	balance := "59999.1234567890123456789"
	first, second, duration, reset := int32(100), int32(0), int64(10080), int64(1800000000)
	value := runtimecontract.ProviderFinancialObservation{
		Sequence: 7, State: "AVAILABLE", OrdinaryUsageAllowed: &ordinary, SpendControlReached: &spending,
		Restriction: "workspace_owner_usage_limit_reached",
		Windows: []runtimecontract.ProviderFinancialWindow{
			{UsedPercent: &first, DurationMinutes: &duration, ResetsAt: &reset},
			{UsedPercent: &second},
		},
		Credits: &runtimecontract.ProviderFinancialCredits{HasCredits: &hasCredits, Unlimited: &unlimited, Balance: &balance},
	}
	got := ObservationToProto(value)
	if got == nil || got.Sequence != 7 || got.State != "AVAILABLE" ||
		got.Restriction != "workspace_owner_usage_limit_reached" || got.Credits == nil || len(got.Windows) != 2 ||
		got.Windows[0] == nil || got.Windows[1] == nil {
		t.Fatal("financial projection changed measurements or presence")
	}
	if got.OrdinaryUsageAllowed != &ordinary || got.SpendControlReached != &spending ||
		got.Credits.HasCredits != &hasCredits || got.Credits.Unlimited != &unlimited || got.Credits.Balance != &balance ||
		got.Windows[0].UsedPercent != &first || got.Windows[0].DurationMinutes != &duration || got.Windows[0].ResetsAt != &reset ||
		got.Windows[1].UsedPercent != &second || got.Windows[1].DurationMinutes != nil || got.Windows[1].ResetsAt != nil {
		t.Fatal("financial projection changed scalar pointer sharing")
	}
	got.Windows[0].UsedPercent = nil
	got.Credits.Balance = nil
	if value.Windows[0].UsedPercent != &first || value.Credits.Balance != &balance {
		t.Fatal("protobuf containers alias runtime containers")
	}
}

// TestObservationToProtoPreservesAbsence distinguishes missing credits/windows
// and scalars from an explicitly present empty credits object or empty window;
// a nonnil empty source window slice retains the original nil-result behavior.
// TestObservationToProtoPreservesAbsence отличает отсутствие credits/windows
// и скаляров от явно присутствующих пустых credits либо окна; ненулевой пустой
// source slice сохраняет прежний nil-result для списка окон.
func TestObservationToProtoPreservesAbsence(t *testing.T) {
	for _, windows := range [][]runtimecontract.ProviderFinancialWindow{nil, {}} {
		got := ObservationToProto(runtimecontract.ProviderFinancialObservation{Sequence: 1, State: "UNAVAILABLE", Windows: windows})
		if got.Windows != nil || got.Credits != nil || got.OrdinaryUsageAllowed != nil || got.SpendControlReached != nil {
			t.Fatal("absent financial fields were inferred")
		}
	}
	got := ObservationToProto(runtimecontract.ProviderFinancialObservation{
		Credits: &runtimecontract.ProviderFinancialCredits{}, Windows: []runtimecontract.ProviderFinancialWindow{{}},
	})
	if got.Credits == nil || got.Credits.HasCredits != nil || got.Credits.Unlimited != nil || got.Credits.Balance != nil ||
		len(got.Windows) != 1 || got.Windows[0] == nil || got.Windows[0].UsedPercent != nil ||
		got.Windows[0].DurationMinutes != nil || got.Windows[0].ResetsAt != nil {
		t.Fatal("explicit empty financial containers lost presence")
	}
}

// TestObservationToProtoDoesNotBecomeAdmission verifies the caster preserves
// even invalid domain measurements verbatim; callers still own validation,
// eligibility and grant issuance rather than receiving a new permissive policy.
// TestObservationToProtoDoesNotBecomeAdmission проверяет буквальный перенос
// даже неверных доменных измерений: проверка, допустимость и выдача grants
// остаются у caller, новая разрешающая policy не появляется.
func TestObservationToProtoDoesNotBecomeAdmission(t *testing.T) {
	negative := int32(-1)
	got := ObservationToProto(runtimecontract.ProviderFinancialObservation{
		Sequence: -3, State: "UNKNOWN", Restriction: "synthetic-unknown",
		Windows: []runtimecontract.ProviderFinancialWindow{{UsedPercent: &negative}},
	})
	if got.Sequence != -3 || got.State != "UNKNOWN" || got.Restriction != "synthetic-unknown" ||
		len(got.Windows) != 1 || got.Windows[0].UsedPercent != &negative {
		t.Fatal("caster unexpectedly changed validation semantics")
	}
}
