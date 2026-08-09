
package generated

type ProviderConnectionMaskedStatus uint

const (
  ProviderConnectionMaskedStatusAvailable ProviderConnectionMaskedStatus = iota
  ProviderConnectionMaskedStatusDegraded
  ProviderConnectionMaskedStatusIneligible
  ProviderConnectionMaskedStatusArchived
)

// Value returns the value of the enum.
func (op ProviderConnectionMaskedStatus) Value() any {
	if op >= ProviderConnectionMaskedStatus(len(ProviderConnectionMaskedStatusValues)) {
		return nil
	}
	return ProviderConnectionMaskedStatusValues[op]
}

var ProviderConnectionMaskedStatusValues = []any{"AVAILABLE","DEGRADED","INELIGIBLE","ARCHIVED"}
var ValuesToProviderConnectionMaskedStatus = map[any]ProviderConnectionMaskedStatus{
  ProviderConnectionMaskedStatusValues[ProviderConnectionMaskedStatusAvailable]: ProviderConnectionMaskedStatusAvailable,
  ProviderConnectionMaskedStatusValues[ProviderConnectionMaskedStatusDegraded]: ProviderConnectionMaskedStatusDegraded,
  ProviderConnectionMaskedStatusValues[ProviderConnectionMaskedStatusIneligible]: ProviderConnectionMaskedStatusIneligible,
  ProviderConnectionMaskedStatusValues[ProviderConnectionMaskedStatusArchived]: ProviderConnectionMaskedStatusArchived,
}
