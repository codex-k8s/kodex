package providercredential

import (
	"errors"
)

// errManagedRenewalUnknown marks an unproven managed OAuth outcome, including publication or cleanup
// uncertainty; callers may observe durable state but must not infer permission to refresh again.
// errManagedRenewalUnknown обозначает недоказанный managed OAuth исход, включая неопределённость
// публикации или cleanup; вызывающий может наблюдать durable состояние, но не выводить разрешение
// повторного refresh.
var errManagedRenewalUnknown = errors.New("managed provider credential renewal outcome is unknown")

// ManagedRenewalOutcomeUnknown recognizes the unknown-outcome sentinel through wrapped or joined errors
// without inspecting potentially sensitive error text.
// ManagedRenewalOutcomeUnknown распознаёт sentinel неопределённого исхода через wrapped или joined
// ошибки без анализа потенциально чувствительного текста.
func ManagedRenewalOutcomeUnknown(err error) bool { return errors.Is(err, errManagedRenewalUnknown) }
