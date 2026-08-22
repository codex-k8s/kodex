
package generated

type ResyncReason uint

const (
  ResyncReasonRetentionExpired ResyncReason = iota
  ResyncReasonGapDetected
  ResyncReasonProjectionRecovered
)

// Value returns the value of the enum.
func (op ResyncReason) Value() any {
	if op >= ResyncReason(len(ResyncReasonValues)) {
		return nil
	}
	return ResyncReasonValues[op]
}

var ResyncReasonValues = []any{"RETENTION_EXPIRED","GAP_DETECTED","PROJECTION_RECOVERED"}
var ValuesToResyncReason = map[any]ResyncReason{
  ResyncReasonValues[ResyncReasonRetentionExpired]: ResyncReasonRetentionExpired,
  ResyncReasonValues[ResyncReasonGapDetected]: ResyncReasonGapDetected,
  ResyncReasonValues[ResyncReasonProjectionRecovered]: ResyncReasonProjectionRecovered,
}
