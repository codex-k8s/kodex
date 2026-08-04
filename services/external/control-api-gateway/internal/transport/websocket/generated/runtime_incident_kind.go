
package generated

type RuntimeIncidentKind uint

const (
  RuntimeIncidentKindHeartbeatMissed RuntimeIncidentKind = iota
  RuntimeIncidentKindReconcileFailed
  RuntimeIncidentKindWorkloadUnavailable
)

// Value returns the value of the enum.
func (op RuntimeIncidentKind) Value() any {
	if op >= RuntimeIncidentKind(len(RuntimeIncidentKindValues)) {
		return nil
	}
	return RuntimeIncidentKindValues[op]
}

var RuntimeIncidentKindValues = []any{"HEARTBEAT_MISSED","RECONCILE_FAILED","WORKLOAD_UNAVAILABLE"}
var ValuesToRuntimeIncidentKind = map[any]RuntimeIncidentKind{
  RuntimeIncidentKindValues[RuntimeIncidentKindHeartbeatMissed]: RuntimeIncidentKindHeartbeatMissed,
  RuntimeIncidentKindValues[RuntimeIncidentKindReconcileFailed]: RuntimeIncidentKindReconcileFailed,
  RuntimeIncidentKindValues[RuntimeIncidentKindWorkloadUnavailable]: RuntimeIncidentKindWorkloadUnavailable,
}
