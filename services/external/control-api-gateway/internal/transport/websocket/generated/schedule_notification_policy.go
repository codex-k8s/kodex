
package generated

type ScheduleNotificationPolicy uint

const (
  ScheduleNotificationPolicyAlways ScheduleNotificationPolicy = iota
  ScheduleNotificationPolicyOnAction
  ScheduleNotificationPolicyOnFailure
  ScheduleNotificationPolicyOnActionOrFailure
  ScheduleNotificationPolicyAuditOnly
)

// Value returns the value of the enum.
func (op ScheduleNotificationPolicy) Value() any {
	if op >= ScheduleNotificationPolicy(len(ScheduleNotificationPolicyValues)) {
		return nil
	}
	return ScheduleNotificationPolicyValues[op]
}

var ScheduleNotificationPolicyValues = []any{"ALWAYS","ON_ACTION","ON_FAILURE","ON_ACTION_OR_FAILURE","AUDIT_ONLY"}
var ValuesToScheduleNotificationPolicy = map[any]ScheduleNotificationPolicy{
  ScheduleNotificationPolicyValues[ScheduleNotificationPolicyAlways]: ScheduleNotificationPolicyAlways,
  ScheduleNotificationPolicyValues[ScheduleNotificationPolicyOnAction]: ScheduleNotificationPolicyOnAction,
  ScheduleNotificationPolicyValues[ScheduleNotificationPolicyOnFailure]: ScheduleNotificationPolicyOnFailure,
  ScheduleNotificationPolicyValues[ScheduleNotificationPolicyOnActionOrFailure]: ScheduleNotificationPolicyOnActionOrFailure,
  ScheduleNotificationPolicyValues[ScheduleNotificationPolicyAuditOnly]: ScheduleNotificationPolicyAuditOnly,
}
