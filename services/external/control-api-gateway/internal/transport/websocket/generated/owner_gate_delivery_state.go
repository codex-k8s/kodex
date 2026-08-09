
package generated

type OwnerGateDeliveryState uint

const (
  OwnerGateDeliveryStateAwaitingDeliveryProof OwnerGateDeliveryState = iota
  OwnerGateDeliveryStateReady
  OwnerGateDeliveryStateTerminal
  OwnerGateDeliveryStateExpired
)

// Value returns the value of the enum.
func (op OwnerGateDeliveryState) Value() any {
	if op >= OwnerGateDeliveryState(len(OwnerGateDeliveryStateValues)) {
		return nil
	}
	return OwnerGateDeliveryStateValues[op]
}

var OwnerGateDeliveryStateValues = []any{"AWAITING_DELIVERY_PROOF","READY","TERMINAL","EXPIRED"}
var ValuesToOwnerGateDeliveryState = map[any]OwnerGateDeliveryState{
  OwnerGateDeliveryStateValues[OwnerGateDeliveryStateAwaitingDeliveryProof]: OwnerGateDeliveryStateAwaitingDeliveryProof,
  OwnerGateDeliveryStateValues[OwnerGateDeliveryStateReady]: OwnerGateDeliveryStateReady,
  OwnerGateDeliveryStateValues[OwnerGateDeliveryStateTerminal]: OwnerGateDeliveryStateTerminal,
  OwnerGateDeliveryStateValues[OwnerGateDeliveryStateExpired]: OwnerGateDeliveryStateExpired,
}
