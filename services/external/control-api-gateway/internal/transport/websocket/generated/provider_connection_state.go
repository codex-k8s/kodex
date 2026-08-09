
package generated

type ProviderConnectionState uint

const (
  ProviderConnectionStatePending ProviderConnectionState = iota
  ProviderConnectionStateValid
  ProviderConnectionStateInvalid
  ProviderConnectionStateRevoked
)

// Value returns the value of the enum.
func (op ProviderConnectionState) Value() any {
	if op >= ProviderConnectionState(len(ProviderConnectionStateValues)) {
		return nil
	}
	return ProviderConnectionStateValues[op]
}

var ProviderConnectionStateValues = []any{"PENDING","VALID","INVALID","REVOKED"}
var ValuesToProviderConnectionState = map[any]ProviderConnectionState{
  ProviderConnectionStateValues[ProviderConnectionStatePending]: ProviderConnectionStatePending,
  ProviderConnectionStateValues[ProviderConnectionStateValid]: ProviderConnectionStateValid,
  ProviderConnectionStateValues[ProviderConnectionStateInvalid]: ProviderConnectionStateInvalid,
  ProviderConnectionStateValues[ProviderConnectionStateRevoked]: ProviderConnectionStateRevoked,
}
