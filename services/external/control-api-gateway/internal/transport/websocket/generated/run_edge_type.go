package generated

type RunEdgeType uint

const (
	RunEdgeTypeDelegatedTo RunEdgeType = iota
	RunEdgeTypeCallbackTo
	RunEdgeTypeRetryOf
	RunEdgeTypeContinues
	RunEdgeTypeWaitingFor
)

// Value returns the value of the enum.
func (op RunEdgeType) Value() any {
	if op >= RunEdgeType(len(RunEdgeTypeValues)) {
		return nil
	}
	return RunEdgeTypeValues[op]
}

var RunEdgeTypeValues = []any{"DELEGATED_TO", "CALLBACK_TO", "RETRY_OF", "CONTINUES", "WAITING_FOR"}
var ValuesToRunEdgeType = map[any]RunEdgeType{
	RunEdgeTypeValues[RunEdgeTypeDelegatedTo]: RunEdgeTypeDelegatedTo,
	RunEdgeTypeValues[RunEdgeTypeCallbackTo]:  RunEdgeTypeCallbackTo,
	RunEdgeTypeValues[RunEdgeTypeRetryOf]:     RunEdgeTypeRetryOf,
	RunEdgeTypeValues[RunEdgeTypeContinues]:   RunEdgeTypeContinues,
	RunEdgeTypeValues[RunEdgeTypeWaitingFor]:  RunEdgeTypeWaitingFor,
}
