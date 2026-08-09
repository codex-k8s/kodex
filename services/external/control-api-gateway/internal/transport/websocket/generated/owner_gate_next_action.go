
package generated

type OwnerGateNextAction uint

const (
  OwnerGateNextActionWaitForDelivery OwnerGateNextAction = iota
  OwnerGateNextActionResolve
  OwnerGateNextActionReadTerminal
  OwnerGateNextActionNone
)

// Value returns the value of the enum.
func (op OwnerGateNextAction) Value() any {
	if op >= OwnerGateNextAction(len(OwnerGateNextActionValues)) {
		return nil
	}
	return OwnerGateNextActionValues[op]
}

var OwnerGateNextActionValues = []any{"WAIT_FOR_DELIVERY","RESOLVE","READ_TERMINAL","NONE"}
var ValuesToOwnerGateNextAction = map[any]OwnerGateNextAction{
  OwnerGateNextActionValues[OwnerGateNextActionWaitForDelivery]: OwnerGateNextActionWaitForDelivery,
  OwnerGateNextActionValues[OwnerGateNextActionResolve]: OwnerGateNextActionResolve,
  OwnerGateNextActionValues[OwnerGateNextActionReadTerminal]: OwnerGateNextActionReadTerminal,
  OwnerGateNextActionValues[OwnerGateNextActionNone]: OwnerGateNextActionNone,
}
