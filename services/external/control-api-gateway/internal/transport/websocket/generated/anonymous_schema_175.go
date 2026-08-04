package generated

import (
	"encoding/json"
)

type AnonymousSchema_175 uint

const (
	AnonymousSchema_175Pending AnonymousSchema_175 = iota
	AnonymousSchema_175Scanning
	AnonymousSchema_175Clean
	AnonymousSchema_175Quarantined
	AnonymousSchema_175Failed
)

// Value returns the value of the enum.
func (op AnonymousSchema_175) Value() any {
	if op >= AnonymousSchema_175(len(AnonymousSchema_175Values)) {
		return nil
	}
	return AnonymousSchema_175Values[op]
}

var AnonymousSchema_175Values = []any{"PENDING", "SCANNING", "CLEAN", "QUARANTINED", "FAILED"}
var ValuesToAnonymousSchema_175 = map[any]AnonymousSchema_175{
	AnonymousSchema_175Values[AnonymousSchema_175Pending]:     AnonymousSchema_175Pending,
	AnonymousSchema_175Values[AnonymousSchema_175Scanning]:    AnonymousSchema_175Scanning,
	AnonymousSchema_175Values[AnonymousSchema_175Clean]:       AnonymousSchema_175Clean,
	AnonymousSchema_175Values[AnonymousSchema_175Quarantined]: AnonymousSchema_175Quarantined,
	AnonymousSchema_175Values[AnonymousSchema_175Failed]:      AnonymousSchema_175Failed,
}

func (op *AnonymousSchema_175) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_175[v]
	return nil
}

func (op AnonymousSchema_175) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
