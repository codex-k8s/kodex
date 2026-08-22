
package generated

type RunEventType uint

const (
  RunEventTypeRunCreated RunEventType = iota
  RunEventTypeRunStateChanged
  RunEventTypeNodeAdded
  RunEventTypeNodeStateChanged
  RunEventTypeEdgeAdded
  RunEventTypeTurnQueued
  RunEventTypeTurnStarted
  RunEventTypeTurnProgress
  RunEventTypeTurnCompleted
  RunEventTypeDelegationCreated
  RunEventTypeCallbackDelivered
  RunEventTypeOwnerGateOpened
  RunEventTypeOwnerGateResolved
  RunEventTypeArtifactAvailable
  RunEventTypeIncidentLinked
)

// Value returns the value of the enum.
func (op RunEventType) Value() any {
	if op >= RunEventType(len(RunEventTypeValues)) {
		return nil
	}
	return RunEventTypeValues[op]
}

var RunEventTypeValues = []any{"RUN_CREATED","RUN_STATE_CHANGED","NODE_ADDED","NODE_STATE_CHANGED","EDGE_ADDED","TURN_QUEUED","TURN_STARTED","TURN_PROGRESS","TURN_COMPLETED","DELEGATION_CREATED","CALLBACK_DELIVERED","OWNER_GATE_OPENED","OWNER_GATE_RESOLVED","ARTIFACT_AVAILABLE","INCIDENT_LINKED"}
var ValuesToRunEventType = map[any]RunEventType{
  RunEventTypeValues[RunEventTypeRunCreated]: RunEventTypeRunCreated,
  RunEventTypeValues[RunEventTypeRunStateChanged]: RunEventTypeRunStateChanged,
  RunEventTypeValues[RunEventTypeNodeAdded]: RunEventTypeNodeAdded,
  RunEventTypeValues[RunEventTypeNodeStateChanged]: RunEventTypeNodeStateChanged,
  RunEventTypeValues[RunEventTypeEdgeAdded]: RunEventTypeEdgeAdded,
  RunEventTypeValues[RunEventTypeTurnQueued]: RunEventTypeTurnQueued,
  RunEventTypeValues[RunEventTypeTurnStarted]: RunEventTypeTurnStarted,
  RunEventTypeValues[RunEventTypeTurnProgress]: RunEventTypeTurnProgress,
  RunEventTypeValues[RunEventTypeTurnCompleted]: RunEventTypeTurnCompleted,
  RunEventTypeValues[RunEventTypeDelegationCreated]: RunEventTypeDelegationCreated,
  RunEventTypeValues[RunEventTypeCallbackDelivered]: RunEventTypeCallbackDelivered,
  RunEventTypeValues[RunEventTypeOwnerGateOpened]: RunEventTypeOwnerGateOpened,
  RunEventTypeValues[RunEventTypeOwnerGateResolved]: RunEventTypeOwnerGateResolved,
  RunEventTypeValues[RunEventTypeArtifactAvailable]: RunEventTypeArtifactAvailable,
  RunEventTypeValues[RunEventTypeIncidentLinked]: RunEventTypeIncidentLinked,
}
