package grpc

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/providerfinancewire"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestFinancialWireRoundTripDoesNotInferPaidAdmission uses the shared forward
// caster and local reverse projection to preserve independent usage/credit facts,
// exact balance precision and absent optional fields without inventing permission.
// TestFinancialWireRoundTripDoesNotInferPaidAdmission проверяет общий прямой caster
// и локальную обратную проекцию: независимые usage/credit facts, точность balance
// и отсутствие необязательных полей сохраняются без придуманного права запуска.
func TestFinancialWireRoundTripDoesNotInferPaidAdmission(t *testing.T) {
	ordinary := false
	spending := true
	balance := "59999.1234567890123456789"
	reset := int64(1800000000)
	observation := runtimecontract.ProviderFinancialObservation{Sequence: 1, State: "AVAILABLE", Windows: []runtimecontract.ProviderFinancialWindow{{UsedPercent: new(int32(100)), ResetsAt: &reset}}, Credits: &runtimecontract.ProviderFinancialCredits{HasCredits: new(true), Balance: &balance}, OrdinaryUsageAllowed: &ordinary, SpendControlReached: &spending, Restriction: "workspace_owner_usage_limit_reached"}
	if actual := financialObservation(providerfinancewire.ObservationToProto(observation)); !reflect.DeepEqual(actual, observation) {
		t.Fatal("wire conversion lost independent financial facts")
	}
	observation.Credits.Balance = nil
	observation.OrdinaryUsageAllowed = nil
	observation.SpendControlReached = nil
	if actual := financialObservation(providerfinancewire.ObservationToProto(observation)); !reflect.DeepEqual(actual, observation) {
		t.Fatal("unknown wire fields invented")
	}
}
