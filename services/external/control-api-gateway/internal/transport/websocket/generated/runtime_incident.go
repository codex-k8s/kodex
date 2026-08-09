
package generated

type RuntimeIncident struct {
  IncidentId string
  ExecutionId string
  ExecutionFence int
  Kind *RuntimeIncidentKind
  EvidenceSha256 string
  WorkloadId string
  OccurredAt string
  Version int
  State *IncidentState
  ActionReasonCode string
  UpdatedAt string
}