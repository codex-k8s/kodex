
package generated

type ScheduleMisfirePolicy uint

const (
  ScheduleMisfirePolicySkip ScheduleMisfirePolicy = iota
  ScheduleMisfirePolicyRunOnce
  ScheduleMisfirePolicyCatchUp
  ScheduleMisfirePolicyWithinGrace
)

// Value returns the value of the enum.
func (op ScheduleMisfirePolicy) Value() any {
	if op >= ScheduleMisfirePolicy(len(ScheduleMisfirePolicyValues)) {
		return nil
	}
	return ScheduleMisfirePolicyValues[op]
}

var ScheduleMisfirePolicyValues = []any{"SKIP","RUN_ONCE","CATCH_UP","WITHIN_GRACE"}
var ValuesToScheduleMisfirePolicy = map[any]ScheduleMisfirePolicy{
  ScheduleMisfirePolicyValues[ScheduleMisfirePolicySkip]: ScheduleMisfirePolicySkip,
  ScheduleMisfirePolicyValues[ScheduleMisfirePolicyRunOnce]: ScheduleMisfirePolicyRunOnce,
  ScheduleMisfirePolicyValues[ScheduleMisfirePolicyCatchUp]: ScheduleMisfirePolicyCatchUp,
  ScheduleMisfirePolicyValues[ScheduleMisfirePolicyWithinGrace]: ScheduleMisfirePolicyWithinGrace,
}
