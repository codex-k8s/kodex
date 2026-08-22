
package generated

type ArtifactScanState uint

const (
  ArtifactScanStatePending ArtifactScanState = iota
  ArtifactScanStateScanning
  ArtifactScanStateClean
  ArtifactScanStateQuarantined
  ArtifactScanStateFailed
)

// Value returns the value of the enum.
func (op ArtifactScanState) Value() any {
	if op >= ArtifactScanState(len(ArtifactScanStateValues)) {
		return nil
	}
	return ArtifactScanStateValues[op]
}

var ArtifactScanStateValues = []any{"PENDING","SCANNING","CLEAN","QUARANTINED","FAILED"}
var ValuesToArtifactScanState = map[any]ArtifactScanState{
  ArtifactScanStateValues[ArtifactScanStatePending]: ArtifactScanStatePending,
  ArtifactScanStateValues[ArtifactScanStateScanning]: ArtifactScanStateScanning,
  ArtifactScanStateValues[ArtifactScanStateClean]: ArtifactScanStateClean,
  ArtifactScanStateValues[ArtifactScanStateQuarantined]: ArtifactScanStateQuarantined,
  ArtifactScanStateValues[ArtifactScanStateFailed]: ArtifactScanStateFailed,
}
