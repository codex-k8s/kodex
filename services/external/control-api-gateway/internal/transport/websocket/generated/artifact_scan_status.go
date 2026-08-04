
package generated

type ArtifactScanStatus uint

const (
  ArtifactScanStatusPending ArtifactScanStatus = iota
  ArtifactScanStatusScanning
  ArtifactScanStatusClean
  ArtifactScanStatusQuarantined
  ArtifactScanStatusFailed
)

// Value returns the value of the enum.
func (op ArtifactScanStatus) Value() any {
	if op >= ArtifactScanStatus(len(ArtifactScanStatusValues)) {
		return nil
	}
	return ArtifactScanStatusValues[op]
}

var ArtifactScanStatusValues = []any{"PENDING","SCANNING","CLEAN","QUARANTINED","FAILED"}
var ValuesToArtifactScanStatus = map[any]ArtifactScanStatus{
  ArtifactScanStatusValues[ArtifactScanStatusPending]: ArtifactScanStatusPending,
  ArtifactScanStatusValues[ArtifactScanStatusScanning]: ArtifactScanStatusScanning,
  ArtifactScanStatusValues[ArtifactScanStatusClean]: ArtifactScanStatusClean,
  ArtifactScanStatusValues[ArtifactScanStatusQuarantined]: ArtifactScanStatusQuarantined,
  ArtifactScanStatusValues[ArtifactScanStatusFailed]: ArtifactScanStatusFailed,
}
