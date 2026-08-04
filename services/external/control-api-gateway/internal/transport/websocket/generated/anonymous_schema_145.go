package generated

import (
	"encoding/json"
)

type AnonymousSchema_145 uint

const (
	AnonymousSchema_145Pending AnonymousSchema_145 = iota
	AnonymousSchema_145Approved
	AnonymousSchema_145Rejected
	AnonymousSchema_145ChangesRequested
	AnonymousSchema_145Cancelled
)

// Value returns the value of the enum.
func (op AnonymousSchema_145) Value() any {
	if op >= AnonymousSchema_145(len(AnonymousSchema_145Values)) {
		return nil
	}
	return AnonymousSchema_145Values[op]
}

var AnonymousSchema_145Values = []any{"PENDING", "APPROVED", "REJECTED", "CHANGES_REQUESTED", "CANCELLED"}
var ValuesToAnonymousSchema_145 = map[any]AnonymousSchema_145{
	AnonymousSchema_145Values[AnonymousSchema_145Pending]:          AnonymousSchema_145Pending,
	AnonymousSchema_145Values[AnonymousSchema_145Approved]:         AnonymousSchema_145Approved,
	AnonymousSchema_145Values[AnonymousSchema_145Rejected]:         AnonymousSchema_145Rejected,
	AnonymousSchema_145Values[AnonymousSchema_145ChangesRequested]: AnonymousSchema_145ChangesRequested,
	AnonymousSchema_145Values[AnonymousSchema_145Cancelled]:        AnonymousSchema_145Cancelled,
}

func (op *AnonymousSchema_145) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_145[v]
	return nil
}

func (op AnonymousSchema_145) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
