package generated

type RunNodeState uint

const (
	RunNodeStateQueued RunNodeState = iota
	RunNodeStateRunning
	RunNodeStateWaiting
	RunNodeStateSucceeded
	RunNodeStateFailed
	RunNodeStateCancelled
	RunNodeStateSkipped
)

// Value returns the value of the enum.
func (op RunNodeState) Value() any {
	if op >= RunNodeState(len(RunNodeStateValues)) {
		return nil
	}
	return RunNodeStateValues[op]
}

var RunNodeStateValues = []any{"QUEUED", "RUNNING", "WAITING", "SUCCEEDED", "FAILED", "CANCELLED", "SKIPPED"}
var ValuesToRunNodeState = map[any]RunNodeState{
	RunNodeStateValues[RunNodeStateQueued]:    RunNodeStateQueued,
	RunNodeStateValues[RunNodeStateRunning]:   RunNodeStateRunning,
	RunNodeStateValues[RunNodeStateWaiting]:   RunNodeStateWaiting,
	RunNodeStateValues[RunNodeStateSucceeded]: RunNodeStateSucceeded,
	RunNodeStateValues[RunNodeStateFailed]:    RunNodeStateFailed,
	RunNodeStateValues[RunNodeStateCancelled]: RunNodeStateCancelled,
	RunNodeStateValues[RunNodeStateSkipped]:   RunNodeStateSkipped,
}
