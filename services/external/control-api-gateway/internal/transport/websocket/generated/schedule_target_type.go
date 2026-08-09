
package generated

type ScheduleTargetType uint

const (
  ScheduleTargetTypeAgent ScheduleTargetType = iota
  ScheduleTargetTypePlaybook
)

// Value returns the value of the enum.
func (op ScheduleTargetType) Value() any {
	if op >= ScheduleTargetType(len(ScheduleTargetTypeValues)) {
		return nil
	}
	return ScheduleTargetTypeValues[op]
}

var ScheduleTargetTypeValues = []any{"AGENT","PLAYBOOK"}
var ValuesToScheduleTargetType = map[any]ScheduleTargetType{
  ScheduleTargetTypeValues[ScheduleTargetTypeAgent]: ScheduleTargetTypeAgent,
  ScheduleTargetTypeValues[ScheduleTargetTypePlaybook]: ScheduleTargetTypePlaybook,
}
