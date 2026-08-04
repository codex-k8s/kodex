package generated

import (
	"encoding/json"
)

type AnonymousSchema_9 uint

const (
	AnonymousSchema_9Runs AnonymousSchema_9 = iota
	AnonymousSchema_9Incidents
	AnonymousSchema_9Resources
	AnonymousSchema_9ConfigurationChanges
)

// Value returns the value of the enum.
func (op AnonymousSchema_9) Value() any {
	if op >= AnonymousSchema_9(len(AnonymousSchema_9Values)) {
		return nil
	}
	return AnonymousSchema_9Values[op]
}

var AnonymousSchema_9Values = []any{"RUNS", "INCIDENTS", "RESOURCES", "CONFIGURATION_CHANGES"}
var ValuesToAnonymousSchema_9 = map[any]AnonymousSchema_9{
	AnonymousSchema_9Values[AnonymousSchema_9Runs]:                 AnonymousSchema_9Runs,
	AnonymousSchema_9Values[AnonymousSchema_9Incidents]:            AnonymousSchema_9Incidents,
	AnonymousSchema_9Values[AnonymousSchema_9Resources]:            AnonymousSchema_9Resources,
	AnonymousSchema_9Values[AnonymousSchema_9ConfigurationChanges]: AnonymousSchema_9ConfigurationChanges,
}

func (op *AnonymousSchema_9) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_9[v]
	return nil
}

func (op AnonymousSchema_9) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
