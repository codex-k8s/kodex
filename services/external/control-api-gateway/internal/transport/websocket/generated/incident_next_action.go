
package generated

type IncidentNextAction uint

const (
  IncidentNextActionAcknowledge IncidentNextAction = iota
  IncidentNextActionRetry
  IncidentNextActionRelease
  IncidentNextActionClose
)

// Value returns the value of the enum.
func (op IncidentNextAction) Value() any {
	if op >= IncidentNextAction(len(IncidentNextActionValues)) {
		return nil
	}
	return IncidentNextActionValues[op]
}

var IncidentNextActionValues = []any{"ACKNOWLEDGE","RETRY","RELEASE","CLOSE"}
var ValuesToIncidentNextAction = map[any]IncidentNextAction{
  IncidentNextActionValues[IncidentNextActionAcknowledge]: IncidentNextActionAcknowledge,
  IncidentNextActionValues[IncidentNextActionRetry]: IncidentNextActionRetry,
  IncidentNextActionValues[IncidentNextActionRelease]: IncidentNextActionRelease,
  IncidentNextActionValues[IncidentNextActionClose]: IncidentNextActionClose,
}
