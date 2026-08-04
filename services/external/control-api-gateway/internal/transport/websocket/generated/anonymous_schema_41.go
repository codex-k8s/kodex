package generated

import (
	"encoding/json"
)

type AnonymousSchema_41 uint

const (
	AnonymousSchema_41User AnonymousSchema_41 = iota
	AnonymousSchema_41Coordination
	AnonymousSchema_41WorkControl
	AnonymousSchema_41Runs
)

// Value returns the value of the enum.
func (op AnonymousSchema_41) Value() any {
	if op >= AnonymousSchema_41(len(AnonymousSchema_41Values)) {
		return nil
	}
	return AnonymousSchema_41Values[op]
}

var AnonymousSchema_41Values = []any{"USER", "COORDINATION", "WORK_CONTROL", "RUNS"}
var ValuesToAnonymousSchema_41 = map[any]AnonymousSchema_41{
	AnonymousSchema_41Values[AnonymousSchema_41User]:         AnonymousSchema_41User,
	AnonymousSchema_41Values[AnonymousSchema_41Coordination]: AnonymousSchema_41Coordination,
	AnonymousSchema_41Values[AnonymousSchema_41WorkControl]:  AnonymousSchema_41WorkControl,
	AnonymousSchema_41Values[AnonymousSchema_41Runs]:         AnonymousSchema_41Runs,
}

func (op *AnonymousSchema_41) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_41[v]
	return nil
}

func (op AnonymousSchema_41) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
