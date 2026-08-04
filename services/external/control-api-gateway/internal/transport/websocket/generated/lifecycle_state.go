
package generated

type LifecycleState uint

const (
  LifecycleStateActive LifecycleState = iota
  LifecycleStatePaused
  LifecycleStateArchived
  LifecycleStateDeletionPending
  LifecycleStateDeleted
  LifecycleStateQueued
  LifecycleStateClaimed
  LifecycleStateRunning
  LifecycleStateWaitingOwner
  LifecycleStateWaitingExternal
  LifecycleStateSucceeded
  LifecycleStateFailed
  LifecycleStateCancelled
  LifecycleStateExpired
  LifecycleStateBlocked
)

// Value returns the value of the enum.
func (op LifecycleState) Value() any {
	if op >= LifecycleState(len(LifecycleStateValues)) {
		return nil
	}
	return LifecycleStateValues[op]
}

var LifecycleStateValues = []any{"ACTIVE","PAUSED","ARCHIVED","DELETION_PENDING","DELETED","QUEUED","CLAIMED","RUNNING","WAITING_OWNER","WAITING_EXTERNAL","SUCCEEDED","FAILED","CANCELLED","EXPIRED","BLOCKED"}
var ValuesToLifecycleState = map[any]LifecycleState{
  LifecycleStateValues[LifecycleStateActive]: LifecycleStateActive,
  LifecycleStateValues[LifecycleStatePaused]: LifecycleStatePaused,
  LifecycleStateValues[LifecycleStateArchived]: LifecycleStateArchived,
  LifecycleStateValues[LifecycleStateDeletionPending]: LifecycleStateDeletionPending,
  LifecycleStateValues[LifecycleStateDeleted]: LifecycleStateDeleted,
  LifecycleStateValues[LifecycleStateQueued]: LifecycleStateQueued,
  LifecycleStateValues[LifecycleStateClaimed]: LifecycleStateClaimed,
  LifecycleStateValues[LifecycleStateRunning]: LifecycleStateRunning,
  LifecycleStateValues[LifecycleStateWaitingOwner]: LifecycleStateWaitingOwner,
  LifecycleStateValues[LifecycleStateWaitingExternal]: LifecycleStateWaitingExternal,
  LifecycleStateValues[LifecycleStateSucceeded]: LifecycleStateSucceeded,
  LifecycleStateValues[LifecycleStateFailed]: LifecycleStateFailed,
  LifecycleStateValues[LifecycleStateCancelled]: LifecycleStateCancelled,
  LifecycleStateValues[LifecycleStateExpired]: LifecycleStateExpired,
  LifecycleStateValues[LifecycleStateBlocked]: LifecycleStateBlocked,
}
