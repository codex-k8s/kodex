
package generated

type ScheduleSessionPolicy uint

const (
  ScheduleSessionPolicyNew ScheduleSessionPolicy = iota
  ScheduleSessionPolicyPersistent
  ScheduleSessionPolicyRolling
)

// Value returns the value of the enum.
func (op ScheduleSessionPolicy) Value() any {
	if op >= ScheduleSessionPolicy(len(ScheduleSessionPolicyValues)) {
		return nil
	}
	return ScheduleSessionPolicyValues[op]
}

var ScheduleSessionPolicyValues = []any{"NEW","PERSISTENT","ROLLING"}
var ValuesToScheduleSessionPolicy = map[any]ScheduleSessionPolicy{
  ScheduleSessionPolicyValues[ScheduleSessionPolicyNew]: ScheduleSessionPolicyNew,
  ScheduleSessionPolicyValues[ScheduleSessionPolicyPersistent]: ScheduleSessionPolicyPersistent,
  ScheduleSessionPolicyValues[ScheduleSessionPolicyRolling]: ScheduleSessionPolicyRolling,
}
