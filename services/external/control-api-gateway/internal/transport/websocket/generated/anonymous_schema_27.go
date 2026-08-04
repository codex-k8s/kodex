package generated

import (
	"encoding/json"
)

type AnonymousSchema_27 uint

const (
	AnonymousSchema_27Ru AnonymousSchema_27 = iota
	AnonymousSchema_27En
)

// Value returns the value of the enum.
func (op AnonymousSchema_27) Value() any {
	if op >= AnonymousSchema_27(len(AnonymousSchema_27Values)) {
		return nil
	}
	return AnonymousSchema_27Values[op]
}

var AnonymousSchema_27Values = []any{"ru", "en"}
var ValuesToAnonymousSchema_27 = map[any]AnonymousSchema_27{
	AnonymousSchema_27Values[AnonymousSchema_27Ru]: AnonymousSchema_27Ru,
	AnonymousSchema_27Values[AnonymousSchema_27En]: AnonymousSchema_27En,
}

func (op *AnonymousSchema_27) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_27[v]
	return nil
}

func (op AnonymousSchema_27) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
