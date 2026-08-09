
package generated

type IncidentSeverity uint

const (
  IncidentSeverityWarning IncidentSeverity = iota
  IncidentSeverityError
  IncidentSeverityCritical
)

// Value returns the value of the enum.
func (op IncidentSeverity) Value() any {
	if op >= IncidentSeverity(len(IncidentSeverityValues)) {
		return nil
	}
	return IncidentSeverityValues[op]
}

var IncidentSeverityValues = []any{"WARNING","ERROR","CRITICAL"}
var ValuesToIncidentSeverity = map[any]IncidentSeverity{
  IncidentSeverityValues[IncidentSeverityWarning]: IncidentSeverityWarning,
  IncidentSeverityValues[IncidentSeverityError]: IncidentSeverityError,
  IncidentSeverityValues[IncidentSeverityCritical]: IncidentSeverityCritical,
}
