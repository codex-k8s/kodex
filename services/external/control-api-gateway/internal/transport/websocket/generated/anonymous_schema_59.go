package generated

import (
	"encoding/json"
)

type AnonymousSchema_59 uint

const (
	AnonymousSchema_59LeastUsed AnonymousSchema_59 = iota
	AnonymousSchema_59Weighted
)

// Value returns the value of the enum.
func (op AnonymousSchema_59) Value() any {
	if op >= AnonymousSchema_59(len(AnonymousSchema_59Values)) {
		return nil
	}
	return AnonymousSchema_59Values[op]
}

var AnonymousSchema_59Values = []any{"least_used", "weighted"}
var ValuesToAnonymousSchema_59 = map[any]AnonymousSchema_59{
	AnonymousSchema_59Values[AnonymousSchema_59LeastUsed]: AnonymousSchema_59LeastUsed,
	AnonymousSchema_59Values[AnonymousSchema_59Weighted]:  AnonymousSchema_59Weighted,
}

func (op *AnonymousSchema_59) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_59[v]
	return nil
}

func (op AnonymousSchema_59) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
