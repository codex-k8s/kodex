// Package providerfinancewire projects runtime financial observations into the
// control-plane protobuf without assigning identity, eligibility or authority.
// Пакет providerfinancewire переводит runtime-наблюдения финансов в control-plane
// protobuf, не назначая идентичность, допустимость использования и полномочия.
package providerfinancewire

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// ObservationToProto preserves field presence, window order and exact balance
// text without validation, rounding or inferring paid admission. It allocates
// protobuf containers but retains optional scalar pointers as the original
// adapters did; callers must keep those values immutable while using the result.
// ObservationToProto сохраняет наличие полей, порядок окон и точный текст баланса
// без проверки, округления и вывода о допуске за кредиты. Создаёт protobuf-объекты,
// но сохраняет указатели необязательных скаляров как прежние адаптеры; caller
// обязан не менять эти значения при использовании результата.
func ObservationToProto(value runtimecontract.ProviderFinancialObservation) *controlplanev1.ProviderFinancialObservation {
	result := &controlplanev1.ProviderFinancialObservation{Sequence: value.Sequence, State: value.State, OrdinaryUsageAllowed: value.OrdinaryUsageAllowed, SpendControlReached: value.SpendControlReached, Restriction: value.Restriction}
	for _, window := range value.Windows {
		result.Windows = append(result.Windows, &controlplanev1.ProviderFinancialWindow{UsedPercent: window.UsedPercent, DurationMinutes: window.DurationMinutes, ResetsAt: window.ResetsAt})
	}
	if value.Credits != nil {
		result.Credits = &controlplanev1.ProviderFinancialCredits{HasCredits: value.Credits.HasCredits, Unlimited: value.Credits.Unlimited, Balance: value.Credits.Balance}
	}
	return result
}
