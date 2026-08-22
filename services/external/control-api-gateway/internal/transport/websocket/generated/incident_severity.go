
package generated

type IncidentSeverity uint

const (
  IncidentSeverityInfo IncidentSeverity = iota
  IncidentSeverityWarning
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

var IncidentSeverityValues = []any{"INFO","WARNING","ERROR","CRITICAL"}
var ValuesToIncidentSeverity = map[any]IncidentSeverity{
  IncidentSeverityValues[IncidentSeverityInfo]: IncidentSeverityInfo,
  IncidentSeverityValues[IncidentSeverityWarning]: IncidentSeverityWarning,
  IncidentSeverityValues[IncidentSeverityError]: IncidentSeverityError,
  IncidentSeverityValues[IncidentSeverityCritical]: IncidentSeverityCritical,
}
