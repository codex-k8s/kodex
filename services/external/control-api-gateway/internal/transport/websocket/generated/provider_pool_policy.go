
package generated

type ProviderPoolPolicy uint

const (
  ProviderPoolPolicyLeastUsed ProviderPoolPolicy = iota
  ProviderPoolPolicyWeighted
)

// Value returns the value of the enum.
func (op ProviderPoolPolicy) Value() any {
	if op >= ProviderPoolPolicy(len(ProviderPoolPolicyValues)) {
		return nil
	}
	return ProviderPoolPolicyValues[op]
}

var ProviderPoolPolicyValues = []any{"least_used","weighted"}
var ValuesToProviderPoolPolicy = map[any]ProviderPoolPolicy{
  ProviderPoolPolicyValues[ProviderPoolPolicyLeastUsed]: ProviderPoolPolicyLeastUsed,
  ProviderPoolPolicyValues[ProviderPoolPolicyWeighted]: ProviderPoolPolicyWeighted,
}
