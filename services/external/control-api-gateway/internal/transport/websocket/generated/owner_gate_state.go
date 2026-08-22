
package generated

type OwnerGateState uint

const (
  OwnerGateStateOpen OwnerGateState = iota
  OwnerGateStateApproved
  OwnerGateStateRejected
  OwnerGateStateChangesRequested
  OwnerGateStateCancelled
  OwnerGateStateExpired
)

// Value returns the value of the enum.
func (op OwnerGateState) Value() any {
	if op >= OwnerGateState(len(OwnerGateStateValues)) {
		return nil
	}
	return OwnerGateStateValues[op]
}

var OwnerGateStateValues = []any{"OPEN","APPROVED","REJECTED","CHANGES_REQUESTED","CANCELLED","EXPIRED"}
var ValuesToOwnerGateState = map[any]OwnerGateState{
  OwnerGateStateValues[OwnerGateStateOpen]: OwnerGateStateOpen,
  OwnerGateStateValues[OwnerGateStateApproved]: OwnerGateStateApproved,
  OwnerGateStateValues[OwnerGateStateRejected]: OwnerGateStateRejected,
  OwnerGateStateValues[OwnerGateStateChangesRequested]: OwnerGateStateChangesRequested,
  OwnerGateStateValues[OwnerGateStateCancelled]: OwnerGateStateCancelled,
  OwnerGateStateValues[OwnerGateStateExpired]: OwnerGateStateExpired,
}
