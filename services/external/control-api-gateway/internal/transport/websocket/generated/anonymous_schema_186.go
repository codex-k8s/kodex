package generated

import (
	"encoding/json"
)

type AnonymousSchema_186 uint

const (
	AnonymousSchema_186HeartbeatMissed AnonymousSchema_186 = iota
	AnonymousSchema_186ReconcileFailed
	AnonymousSchema_186WorkloadUnavailable
)

// Value returns the value of the enum.
func (op AnonymousSchema_186) Value() any {
	if op >= AnonymousSchema_186(len(AnonymousSchema_186Values)) {
		return nil
	}
	return AnonymousSchema_186Values[op]
}

var AnonymousSchema_186Values = []any{"HEARTBEAT_MISSED", "RECONCILE_FAILED", "WORKLOAD_UNAVAILABLE"}
var ValuesToAnonymousSchema_186 = map[any]AnonymousSchema_186{
	AnonymousSchema_186Values[AnonymousSchema_186HeartbeatMissed]:     AnonymousSchema_186HeartbeatMissed,
	AnonymousSchema_186Values[AnonymousSchema_186ReconcileFailed]:     AnonymousSchema_186ReconcileFailed,
	AnonymousSchema_186Values[AnonymousSchema_186WorkloadUnavailable]: AnonymousSchema_186WorkloadUnavailable,
}

func (op *AnonymousSchema_186) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_186[v]
	return nil
}

func (op AnonymousSchema_186) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
