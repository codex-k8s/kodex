
package generated

type ImageAdmissionVerdict uint

const (
  ImageAdmissionVerdictAccepted ImageAdmissionVerdict = iota
  ImageAdmissionVerdictRejected
)

// Value returns the value of the enum.
func (op ImageAdmissionVerdict) Value() any {
	if op >= ImageAdmissionVerdict(len(ImageAdmissionVerdictValues)) {
		return nil
	}
	return ImageAdmissionVerdictValues[op]
}

var ImageAdmissionVerdictValues = []any{"ACCEPTED","REJECTED"}
var ValuesToImageAdmissionVerdict = map[any]ImageAdmissionVerdict{
  ImageAdmissionVerdictValues[ImageAdmissionVerdictAccepted]: ImageAdmissionVerdictAccepted,
  ImageAdmissionVerdictValues[ImageAdmissionVerdictRejected]: ImageAdmissionVerdictRejected,
}
