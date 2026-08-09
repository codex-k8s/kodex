
package generated

type WorkspaceRestoreState uint

const (
  WorkspaceRestoreStateQueued WorkspaceRestoreState = iota
  WorkspaceRestoreStateRunning
  WorkspaceRestoreStateSucceeded
  WorkspaceRestoreStateFailed
  WorkspaceRestoreStateCancelled
  WorkspaceRestoreStateExpired
)

// Value returns the value of the enum.
func (op WorkspaceRestoreState) Value() any {
	if op >= WorkspaceRestoreState(len(WorkspaceRestoreStateValues)) {
		return nil
	}
	return WorkspaceRestoreStateValues[op]
}

var WorkspaceRestoreStateValues = []any{"QUEUED","RUNNING","SUCCEEDED","FAILED","CANCELLED","EXPIRED"}
var ValuesToWorkspaceRestoreState = map[any]WorkspaceRestoreState{
  WorkspaceRestoreStateValues[WorkspaceRestoreStateQueued]: WorkspaceRestoreStateQueued,
  WorkspaceRestoreStateValues[WorkspaceRestoreStateRunning]: WorkspaceRestoreStateRunning,
  WorkspaceRestoreStateValues[WorkspaceRestoreStateSucceeded]: WorkspaceRestoreStateSucceeded,
  WorkspaceRestoreStateValues[WorkspaceRestoreStateFailed]: WorkspaceRestoreStateFailed,
  WorkspaceRestoreStateValues[WorkspaceRestoreStateCancelled]: WorkspaceRestoreStateCancelled,
  WorkspaceRestoreStateValues[WorkspaceRestoreStateExpired]: WorkspaceRestoreStateExpired,
}
