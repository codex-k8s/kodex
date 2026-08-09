
package generated

type WorkspaceMattermostMappingState uint

const (
  WorkspaceMattermostMappingStateBound WorkspaceMattermostMappingState = iota
  WorkspaceMattermostMappingStateUnlinked
)

// Value returns the value of the enum.
func (op WorkspaceMattermostMappingState) Value() any {
	if op >= WorkspaceMattermostMappingState(len(WorkspaceMattermostMappingStateValues)) {
		return nil
	}
	return WorkspaceMattermostMappingStateValues[op]
}

var WorkspaceMattermostMappingStateValues = []any{"BOUND","UNLINKED"}
var ValuesToWorkspaceMattermostMappingState = map[any]WorkspaceMattermostMappingState{
  WorkspaceMattermostMappingStateValues[WorkspaceMattermostMappingStateBound]: WorkspaceMattermostMappingStateBound,
  WorkspaceMattermostMappingStateValues[WorkspaceMattermostMappingStateUnlinked]: WorkspaceMattermostMappingStateUnlinked,
}
