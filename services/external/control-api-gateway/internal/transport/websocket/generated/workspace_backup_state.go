
package generated

type WorkspaceBackupState uint

const (
  WorkspaceBackupStateVerifying WorkspaceBackupState = iota
  WorkspaceBackupStateAvailable
  WorkspaceBackupStateFailed
  WorkspaceBackupStateCancelled
  WorkspaceBackupStateExpired
)

// Value returns the value of the enum.
func (op WorkspaceBackupState) Value() any {
	if op >= WorkspaceBackupState(len(WorkspaceBackupStateValues)) {
		return nil
	}
	return WorkspaceBackupStateValues[op]
}

var WorkspaceBackupStateValues = []any{"VERIFYING","AVAILABLE","FAILED","CANCELLED","EXPIRED"}
var ValuesToWorkspaceBackupState = map[any]WorkspaceBackupState{
  WorkspaceBackupStateValues[WorkspaceBackupStateVerifying]: WorkspaceBackupStateVerifying,
  WorkspaceBackupStateValues[WorkspaceBackupStateAvailable]: WorkspaceBackupStateAvailable,
  WorkspaceBackupStateValues[WorkspaceBackupStateFailed]: WorkspaceBackupStateFailed,
  WorkspaceBackupStateValues[WorkspaceBackupStateCancelled]: WorkspaceBackupStateCancelled,
  WorkspaceBackupStateValues[WorkspaceBackupStateExpired]: WorkspaceBackupStateExpired,
}
