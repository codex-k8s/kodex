
package generated

type Incident struct {
  Ref string
  ProjectRef string
  RunRef string
  Category string
  Severity *IncidentSeverity
  State *IncidentState
  SafeSummary string
  SafeNextStep string
  CoreAffected bool
  CreatedAt string
}