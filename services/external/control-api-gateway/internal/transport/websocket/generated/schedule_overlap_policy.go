
package generated

type ScheduleOverlapPolicy uint

const (
  ScheduleOverlapPolicyForbid ScheduleOverlapPolicy = iota
  ScheduleOverlapPolicySkip
  ScheduleOverlapPolicyQueue
)

// Value returns the value of the enum.
func (op ScheduleOverlapPolicy) Value() any {
	if op >= ScheduleOverlapPolicy(len(ScheduleOverlapPolicyValues)) {
		return nil
	}
	return ScheduleOverlapPolicyValues[op]
}

var ScheduleOverlapPolicyValues = []any{"FORBID","SKIP","QUEUE"}
var ValuesToScheduleOverlapPolicy = map[any]ScheduleOverlapPolicy{
  ScheduleOverlapPolicyValues[ScheduleOverlapPolicyForbid]: ScheduleOverlapPolicyForbid,
  ScheduleOverlapPolicyValues[ScheduleOverlapPolicySkip]: ScheduleOverlapPolicySkip,
  ScheduleOverlapPolicyValues[ScheduleOverlapPolicyQueue]: ScheduleOverlapPolicyQueue,
}
