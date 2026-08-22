
package generated

type IncidentState uint

const (
  IncidentStateOpen IncidentState = iota
  IncidentStateRecovering
  IncidentStateResolved
)

// Value returns the value of the enum.
func (op IncidentState) Value() any {
	if op >= IncidentState(len(IncidentStateValues)) {
		return nil
	}
	return IncidentStateValues[op]
}

var IncidentStateValues = []any{"OPEN","RECOVERING","RESOLVED"}
var ValuesToIncidentState = map[any]IncidentState{
  IncidentStateValues[IncidentStateOpen]: IncidentStateOpen,
  IncidentStateValues[IncidentStateRecovering]: IncidentStateRecovering,
  IncidentStateValues[IncidentStateResolved]: IncidentStateResolved,
}
