package grpc

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func castFinancialObservation(value runtimecontract.ProviderFinancialObservation) *controlplanev1.ProviderFinancialObservation {
	result := &controlplanev1.ProviderFinancialObservation{Sequence: value.Sequence, State: value.State, OrdinaryUsageAllowed: value.OrdinaryUsageAllowed, SpendControlReached: value.SpendControlReached, Restriction: value.Restriction}
	for _, window := range value.Windows {
		result.Windows = append(result.Windows, &controlplanev1.ProviderFinancialWindow{UsedPercent: window.UsedPercent, DurationMinutes: window.DurationMinutes, ResetsAt: window.ResetsAt})
	}
	if value.Credits != nil {
		result.Credits = &controlplanev1.ProviderFinancialCredits{HasCredits: value.Credits.HasCredits, Unlimited: value.Credits.Unlimited, Balance: value.Credits.Balance}
	}
	return result
}

func financialObservation(value *controlplanev1.ProviderFinancialObservation) runtimecontract.ProviderFinancialObservation {
	result := runtimecontract.ProviderFinancialObservation{Sequence: value.GetSequence(), State: value.GetState(), OrdinaryUsageAllowed: value.OrdinaryUsageAllowed, SpendControlReached: value.SpendControlReached, Restriction: value.GetRestriction()}
	for _, window := range value.GetWindows() {
		result.Windows = append(result.Windows, runtimecontract.ProviderFinancialWindow{UsedPercent: window.UsedPercent, DurationMinutes: window.DurationMinutes, ResetsAt: window.ResetsAt})
	}
	if credits := value.GetCredits(); credits != nil {
		result.Credits = &runtimecontract.ProviderFinancialCredits{HasCredits: credits.HasCredits, Unlimited: credits.Unlimited, Balance: credits.Balance}
	}
	return result
}
