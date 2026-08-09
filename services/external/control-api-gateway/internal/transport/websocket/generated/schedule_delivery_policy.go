
package generated

type ScheduleDeliveryPolicy uint

const (
  ScheduleDeliveryPolicyAtLeastOnce ScheduleDeliveryPolicy = iota
  ScheduleDeliveryPolicyExactlyOnceEffect
)

// Value returns the value of the enum.
func (op ScheduleDeliveryPolicy) Value() any {
	if op >= ScheduleDeliveryPolicy(len(ScheduleDeliveryPolicyValues)) {
		return nil
	}
	return ScheduleDeliveryPolicyValues[op]
}

var ScheduleDeliveryPolicyValues = []any{"AT_LEAST_ONCE","EXACTLY_ONCE_EFFECT"}
var ValuesToScheduleDeliveryPolicy = map[any]ScheduleDeliveryPolicy{
  ScheduleDeliveryPolicyValues[ScheduleDeliveryPolicyAtLeastOnce]: ScheduleDeliveryPolicyAtLeastOnce,
  ScheduleDeliveryPolicyValues[ScheduleDeliveryPolicyExactlyOnceEffect]: ScheduleDeliveryPolicyExactlyOnceEffect,
}
