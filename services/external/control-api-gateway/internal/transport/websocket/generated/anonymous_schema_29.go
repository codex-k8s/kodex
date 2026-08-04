package generated

import (
	"encoding/json"
)

type AnonymousSchema_29 uint

const (
	AnonymousSchema_29Ui AnonymousSchema_29 = iota
	AnonymousSchema_29Git
)

// Value returns the value of the enum.
func (op AnonymousSchema_29) Value() any {
	if op >= AnonymousSchema_29(len(AnonymousSchema_29Values)) {
		return nil
	}
	return AnonymousSchema_29Values[op]
}

var AnonymousSchema_29Values = []any{"ui", "git"}
var ValuesToAnonymousSchema_29 = map[any]AnonymousSchema_29{
	AnonymousSchema_29Values[AnonymousSchema_29Ui]:  AnonymousSchema_29Ui,
	AnonymousSchema_29Values[AnonymousSchema_29Git]: AnonymousSchema_29Git,
}

func (op *AnonymousSchema_29) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_29[v]
	return nil
}

func (op AnonymousSchema_29) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
