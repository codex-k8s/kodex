// Package enum содержит закрытые множества control-plane.
package enum

// Kind — авторитетный тип агрегата.
type Kind string

const (
	KindProject             Kind = "PROJECT"
	KindTeam                Kind = "TEAM"
	KindChat                Kind = "CHAT"
	KindRole                Kind = "ROLE"
	KindPromptProfile       Kind = "PROMPT_PROFILE"
	KindCredentialBinding   Kind = "CREDENTIAL_BINDING"
	KindRepositoryWorkspace Kind = "REPOSITORY_WORKSPACE"
	KindIntegration         Kind = "INTEGRATION"
	KindRuntimeRevision     Kind = "RUNTIME_REVISION"
	KindSession             Kind = "SESSION"
	KindTurn                Kind = "TURN"
	KindProcessRun          Kind = "PROCESS_RUN"
	KindSchedule            Kind = "SCHEDULE"
	KindOwnerGate           Kind = "OWNER_GATE"
	KindMemoryRecord        Kind = "MEMORY_RECORD"
	KindWorkClaim           Kind = "WORK_CLAIM"
	KindArtifact            Kind = "ARTIFACT"
)

// Valid сообщает принадлежность закрытому множеству.
func (kind Kind) Valid() bool {
	switch kind {
	case KindProject, KindTeam, KindChat, KindRole, KindPromptProfile,
		KindCredentialBinding, KindRepositoryWorkspace, KindIntegration,
		KindRuntimeRevision, KindSession, KindTurn, KindProcessRun,
		KindSchedule, KindOwnerGate, KindMemoryRecord, KindWorkClaim,
		KindArtifact:
		return true
	default:
		return false
	}
}

// State — закрытый lifecycle агрегата.
type State string

const (
	StateActive          State = "ACTIVE"
	StatePaused          State = "PAUSED"
	StateArchived        State = "ARCHIVED"
	StateDeletionPending State = "DELETION_PENDING"
	StateDeleted         State = "DELETED"
	StateQueued          State = "QUEUED"
	StateClaimed         State = "CLAIMED"
	StateRunning         State = "RUNNING"
	StateWaitingOwner    State = "WAITING_OWNER"
	StateWaitingExternal State = "WAITING_EXTERNAL"
	StateSucceeded       State = "SUCCEEDED"
	StateFailed          State = "FAILED"
	StateCancelled       State = "CANCELLED"
	StateExpired         State = "EXPIRED"
	StateBlocked         State = "BLOCKED"
)

// Terminal фиксирует состояния без штатного обратного перехода.
func (state State) Terminal() bool {
	switch state {
	case StateDeleted, StateSucceeded, StateFailed, StateCancelled, StateExpired:
		return true
	default:
		return false
	}
}

// InitialState возвращает server-owned начальное состояние.
func InitialState(kind Kind) State {
	switch kind {
	case KindProcessRun:
		return StateRunning
	case KindOwnerGate:
		return StateWaitingOwner
	case KindTurn:
		return StateQueued
	default:
		return StateActive
	}
}

// TransitionAllowed задаёт fail-closed state machine системно для всех видов.
func TransitionAllowed(kind Kind, from, to State) bool {
	if !kind.Valid() || from == to ||
		(from.Terminal() &&
			!(kind == KindTurn && from == StateFailed && to == StateQueued)) {
		return false
	}
	switch kind {
	case KindTurn:
		switch from {
		case StateQueued:
			return to == StateClaimed || to == StateCancelled
		case StateClaimed:
			return to == StateRunning || to == StateQueued ||
				to == StateSucceeded || to == StateFailed || to == StateCancelled
		case StateRunning:
			return to == StateSucceeded || to == StateFailed ||
				to == StateCancelled || to == StateBlocked ||
				to == StateWaitingExternal || to == StateWaitingOwner
		case StateWaitingExternal, StateWaitingOwner, StateBlocked:
			return to == StateQueued || to == StateCancelled || to == StateFailed
		case StateFailed:
			return to == StateQueued
		}
	case KindSession:
		switch from {
		case StateActive:
			return to == StateQueued || to == StateArchived || to == StateBlocked
		case StateQueued:
			return to == StateRunning || to == StateCancelled || to == StateBlocked
		case StateRunning:
			return to == StateActive || to == StateWaitingOwner ||
				to == StateWaitingExternal || to == StateBlocked || to == StateFailed
		case StateWaitingExternal, StateWaitingOwner, StateBlocked:
			return to == StateQueued || to == StateArchived || to == StateFailed
		}
	case KindSchedule:
		return (from == StateActive && (to == StatePaused || to == StateArchived)) ||
			(from == StatePaused && (to == StateActive || to == StateArchived))
	case KindOwnerGate:
		return from == StateWaitingOwner &&
			(to == StateSucceeded || to == StateFailed || to == StateBlocked ||
				to == StateCancelled || to == StateExpired)
	case KindProcessRun:
		switch from {
		case StateRunning:
			return to == StateWaitingOwner || to == StateWaitingExternal ||
				to == StateSucceeded || to == StateFailed ||
				to == StateCancelled || to == StateBlocked
		case StateWaitingOwner, StateWaitingExternal, StateBlocked:
			return to == StateRunning || to == StateSucceeded ||
				to == StateFailed || to == StateCancelled
		}
	case KindWorkClaim:
		return from == StateActive &&
			(to == StateCancelled || to == StateExpired ||
				to == StateArchived || to == StateDeletionPending)
	default:
		switch from {
		case StateActive:
			return to == StatePaused || to == StateArchived ||
				to == StateDeletionPending
		case StatePaused:
			return to == StateActive || to == StateArchived ||
				to == StateDeletionPending
		case StateArchived:
			return to == StateActive || to == StateDeletionPending
		case StateDeletionPending:
			return to == StateActive || to == StateDeleted
		}
	}
	return false
}
