
package generated

type IncidentProjection struct {
  IncidentRef string
  Version int
  Kind *RuntimeIncidentKind
  State *IncidentState
  Severity *IncidentSeverity
  Impact string
  Workspace *OwnerDisplayValue
  Run *OwnerDisplayValue
  SafeCorrelation string
  DiagnosticSummary string
  RunbookUrl string
  OccurredAt string
  UpdatedAt string
  NextActions []IncidentNextAction
  ExecutionFence int
}