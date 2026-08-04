
package generated

type OwnerGateDecision uint

const (
  OwnerGateDecisionPending OwnerGateDecision = iota
  OwnerGateDecisionApproved
  OwnerGateDecisionRejected
  OwnerGateDecisionChangesRequested
  OwnerGateDecisionCancelled
)

// Value returns the value of the enum.
func (op OwnerGateDecision) Value() any {
	if op >= OwnerGateDecision(len(OwnerGateDecisionValues)) {
		return nil
	}
	return OwnerGateDecisionValues[op]
}

var OwnerGateDecisionValues = []any{"PENDING","APPROVED","REJECTED","CHANGES_REQUESTED","CANCELLED"}
var ValuesToOwnerGateDecision = map[any]OwnerGateDecision{
  OwnerGateDecisionValues[OwnerGateDecisionPending]: OwnerGateDecisionPending,
  OwnerGateDecisionValues[OwnerGateDecisionApproved]: OwnerGateDecisionApproved,
  OwnerGateDecisionValues[OwnerGateDecisionRejected]: OwnerGateDecisionRejected,
  OwnerGateDecisionValues[OwnerGateDecisionChangesRequested]: OwnerGateDecisionChangesRequested,
  OwnerGateDecisionValues[OwnerGateDecisionCancelled]: OwnerGateDecisionCancelled,
}
