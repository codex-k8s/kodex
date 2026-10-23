package grpc

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// financialObservation copies a nonnil protobuf observation and nonnil windows
// into runtime values, retaining scalar pointers and exact balance text. Callers
// own prior validation; this reverse projection neither validates eligibility
// nor infers paid admission and retains the original nil-input panic behavior.
// financialObservation переносит ненулевое protobuf-наблюдение и ненулевые окна
// в runtime-значения, сохраняя scalar pointers и точный текст balance. Проверка
// входа принадлежит caller; обратная проекция не проверяет eligibility, не выводит
// допуск за кредиты и сохраняет прежнюю panic-семантику nil input.
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
