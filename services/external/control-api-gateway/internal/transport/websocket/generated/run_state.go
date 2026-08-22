package generated

type RunState uint

const (
	RunStateQueued RunState = iota
	RunStateRunning
	RunStateWaitingHuman
	RunStateCancelling
	RunStateSucceeded
	RunStateFailed
	RunStateCancelled
)

// Value returns the value of the enum.
func (op RunState) Value() any {
	if op >= RunState(len(RunStateValues)) {
		return nil
	}
	return RunStateValues[op]
}

var RunStateValues = []any{"QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING", "SUCCEEDED", "FAILED", "CANCELLED"}
var ValuesToRunState = map[any]RunState{
	RunStateValues[RunStateQueued]:       RunStateQueued,
	RunStateValues[RunStateRunning]:      RunStateRunning,
	RunStateValues[RunStateWaitingHuman]: RunStateWaitingHuman,
	RunStateValues[RunStateCancelling]:   RunStateCancelling,
	RunStateValues[RunStateSucceeded]:    RunStateSucceeded,
	RunStateValues[RunStateFailed]:       RunStateFailed,
	RunStateValues[RunStateCancelled]:    RunStateCancelled,
}
