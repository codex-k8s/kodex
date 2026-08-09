
package generated

type WorkspaceBackupScope uint

const (
  WorkspaceBackupScopeWorkspace WorkspaceBackupScope = iota
  WorkspaceBackupScopeAllWorkspaces
)

// Value returns the value of the enum.
func (op WorkspaceBackupScope) Value() any {
	if op >= WorkspaceBackupScope(len(WorkspaceBackupScopeValues)) {
		return nil
	}
	return WorkspaceBackupScopeValues[op]
}

var WorkspaceBackupScopeValues = []any{"WORKSPACE","ALL_WORKSPACES"}
var ValuesToWorkspaceBackupScope = map[any]WorkspaceBackupScope{
  WorkspaceBackupScopeValues[WorkspaceBackupScopeWorkspace]: WorkspaceBackupScopeWorkspace,
  WorkspaceBackupScopeValues[WorkspaceBackupScopeAllWorkspaces]: WorkspaceBackupScopeAllWorkspaces,
}
