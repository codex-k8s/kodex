package generated

import (
	"encoding/json"
)

type AnonymousSchema_4 uint

const (
	AnonymousSchema_4Runs AnonymousSchema_4 = iota
	AnonymousSchema_4Incidents
	AnonymousSchema_4Resources
	AnonymousSchema_4ConfigurationChanges
)

// Value returns the value of the enum.
func (op AnonymousSchema_4) Value() any {
	if op >= AnonymousSchema_4(len(AnonymousSchema_4Values)) {
		return nil
	}
	return AnonymousSchema_4Values[op]
}

var AnonymousSchema_4Values = []any{"RUNS", "INCIDENTS", "RESOURCES", "CONFIGURATION_CHANGES"}
var ValuesToAnonymousSchema_4 = map[any]AnonymousSchema_4{
	AnonymousSchema_4Values[AnonymousSchema_4Runs]:                 AnonymousSchema_4Runs,
	AnonymousSchema_4Values[AnonymousSchema_4Incidents]:            AnonymousSchema_4Incidents,
	AnonymousSchema_4Values[AnonymousSchema_4Resources]:            AnonymousSchema_4Resources,
	AnonymousSchema_4Values[AnonymousSchema_4ConfigurationChanges]: AnonymousSchema_4ConfigurationChanges,
}

func (op *AnonymousSchema_4) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_4[v]
	return nil
}

func (op AnonymousSchema_4) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
