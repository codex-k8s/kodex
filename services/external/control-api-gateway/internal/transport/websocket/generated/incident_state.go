
package generated

type IncidentState uint

const (
  IncidentStateOpen IncidentState = iota
  IncidentStateAcknowledged
  IncidentStateRetrying
  IncidentStateReleased
  IncidentStateClosed
)

// Value returns the value of the enum.
func (op IncidentState) Value() any {
	if op >= IncidentState(len(IncidentStateValues)) {
		return nil
	}
	return IncidentStateValues[op]
}

var IncidentStateValues = []any{"OPEN","ACKNOWLEDGED","RETRYING","RELEASED","CLOSED"}
var ValuesToIncidentState = map[any]IncidentState{
  IncidentStateValues[IncidentStateOpen]: IncidentStateOpen,
  IncidentStateValues[IncidentStateAcknowledged]: IncidentStateAcknowledged,
  IncidentStateValues[IncidentStateRetrying]: IncidentStateRetrying,
  IncidentStateValues[IncidentStateReleased]: IncidentStateReleased,
  IncidentStateValues[IncidentStateClosed]: IncidentStateClosed,
}
