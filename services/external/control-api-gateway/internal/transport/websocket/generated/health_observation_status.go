
package generated

type HealthObservationStatus uint

const (
  HealthObservationStatusOk HealthObservationStatus = iota
  HealthObservationStatusDegraded
  HealthObservationStatusUnavailable
  HealthObservationStatusUnknown
)

// Value returns the value of the enum.
func (op HealthObservationStatus) Value() any {
	if op >= HealthObservationStatus(len(HealthObservationStatusValues)) {
		return nil
	}
	return HealthObservationStatusValues[op]
}

var HealthObservationStatusValues = []any{"OK","DEGRADED","UNAVAILABLE","UNKNOWN"}
var ValuesToHealthObservationStatus = map[any]HealthObservationStatus{
  HealthObservationStatusValues[HealthObservationStatusOk]: HealthObservationStatusOk,
  HealthObservationStatusValues[HealthObservationStatusDegraded]: HealthObservationStatusDegraded,
  HealthObservationStatusValues[HealthObservationStatusUnavailable]: HealthObservationStatusUnavailable,
  HealthObservationStatusValues[HealthObservationStatusUnknown]: HealthObservationStatusUnknown,
}
