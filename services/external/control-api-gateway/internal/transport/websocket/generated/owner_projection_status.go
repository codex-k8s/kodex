
package generated

type OwnerProjectionStatus uint

const (
  OwnerProjectionStatusPresent OwnerProjectionStatus = iota
  OwnerProjectionStatusUnavailable
  OwnerProjectionStatusStale
  OwnerProjectionStatusIneligible
)

// Value returns the value of the enum.
func (op OwnerProjectionStatus) Value() any {
	if op >= OwnerProjectionStatus(len(OwnerProjectionStatusValues)) {
		return nil
	}
	return OwnerProjectionStatusValues[op]
}

var OwnerProjectionStatusValues = []any{"PRESENT","UNAVAILABLE","STALE","INELIGIBLE"}
var ValuesToOwnerProjectionStatus = map[any]OwnerProjectionStatus{
  OwnerProjectionStatusValues[OwnerProjectionStatusPresent]: OwnerProjectionStatusPresent,
  OwnerProjectionStatusValues[OwnerProjectionStatusUnavailable]: OwnerProjectionStatusUnavailable,
  OwnerProjectionStatusValues[OwnerProjectionStatusStale]: OwnerProjectionStatusStale,
  OwnerProjectionStatusValues[OwnerProjectionStatusIneligible]: OwnerProjectionStatusIneligible,
}
