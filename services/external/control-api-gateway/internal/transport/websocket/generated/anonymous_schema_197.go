package generated

import (
	"encoding/json"
)

type AnonymousSchema_197 uint

const (
	AnonymousSchema_197Succeeded AnonymousSchema_197 = iota
)

// Value returns the value of the enum.
func (op AnonymousSchema_197) Value() any {
	if op >= AnonymousSchema_197(len(AnonymousSchema_197Values)) {
		return nil
	}
	return AnonymousSchema_197Values[op]
}

var AnonymousSchema_197Values = []any{"succeeded"}
var ValuesToAnonymousSchema_197 = map[any]AnonymousSchema_197{
	AnonymousSchema_197Values[AnonymousSchema_197Succeeded]: AnonymousSchema_197Succeeded,
}

func (op *AnonymousSchema_197) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_197[v]
	return nil
}

func (op AnonymousSchema_197) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
