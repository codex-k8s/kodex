
package generated

type RunNextAction uint

const (
  RunNextActionCancel RunNextAction = iota
  RunNextActionRetry
)

// Value returns the value of the enum.
func (op RunNextAction) Value() any {
	if op >= RunNextAction(len(RunNextActionValues)) {
		return nil
	}
	return RunNextActionValues[op]
}

var RunNextActionValues = []any{"CANCEL","RETRY"}
var ValuesToRunNextAction = map[any]RunNextAction{
  RunNextActionValues[RunNextActionCancel]: RunNextActionCancel,
  RunNextActionValues[RunNextActionRetry]: RunNextActionRetry,
}
