package generated

type RunNodeType uint

const (
	RunNodeTypeRootProcess RunNodeType = iota
	RunNodeTypeAgentExecution
	RunNodeTypeHumanGate
	RunNodeTypeExternalAction
)

// Value returns the value of the enum.
func (op RunNodeType) Value() any {
	if op >= RunNodeType(len(RunNodeTypeValues)) {
		return nil
	}
	return RunNodeTypeValues[op]
}

var RunNodeTypeValues = []any{"ROOT_PROCESS", "AGENT_EXECUTION", "HUMAN_GATE", "EXTERNAL_ACTION"}
var ValuesToRunNodeType = map[any]RunNodeType{
	RunNodeTypeValues[RunNodeTypeRootProcess]:    RunNodeTypeRootProcess,
	RunNodeTypeValues[RunNodeTypeAgentExecution]: RunNodeTypeAgentExecution,
	RunNodeTypeValues[RunNodeTypeHumanGate]:      RunNodeTypeHumanGate,
	RunNodeTypeValues[RunNodeTypeExternalAction]: RunNodeTypeExternalAction,
}
