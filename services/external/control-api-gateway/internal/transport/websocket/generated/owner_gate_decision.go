
package generated

type OwnerGateDecision uint

const (
  OwnerGateDecisionApprove OwnerGateDecision = iota
  OwnerGateDecisionReject
  OwnerGateDecisionRequestChanges
  OwnerGateDecisionCancel
)

// Value returns the value of the enum.
func (op OwnerGateDecision) Value() any {
	if op >= OwnerGateDecision(len(OwnerGateDecisionValues)) {
		return nil
	}
	return OwnerGateDecisionValues[op]
}

var OwnerGateDecisionValues = []any{"APPROVE","REJECT","REQUEST_CHANGES","CANCEL"}
var ValuesToOwnerGateDecision = map[any]OwnerGateDecision{
  OwnerGateDecisionValues[OwnerGateDecisionApprove]: OwnerGateDecisionApprove,
  OwnerGateDecisionValues[OwnerGateDecisionReject]: OwnerGateDecisionReject,
  OwnerGateDecisionValues[OwnerGateDecisionRequestChanges]: OwnerGateDecisionRequestChanges,
  OwnerGateDecisionValues[OwnerGateDecisionCancel]: OwnerGateDecisionCancel,
}
