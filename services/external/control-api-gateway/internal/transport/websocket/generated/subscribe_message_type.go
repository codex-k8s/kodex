
package generated

type SubscribeMessageType uint

const (
  SubscribeMessageTypeSubscribe SubscribeMessageType = iota
)

// Value returns the value of the enum.
func (op SubscribeMessageType) Value() any {
	if op >= SubscribeMessageType(len(SubscribeMessageTypeValues)) {
		return nil
	}
	return SubscribeMessageTypeValues[op]
}

var SubscribeMessageTypeValues = []any{"SUBSCRIBE"}
var ValuesToSubscribeMessageType = map[any]SubscribeMessageType{
  SubscribeMessageTypeValues[SubscribeMessageTypeSubscribe]: SubscribeMessageTypeSubscribe,
}
