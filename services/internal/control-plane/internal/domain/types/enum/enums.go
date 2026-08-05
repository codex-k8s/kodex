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
	KindRoleImageRecipe     Kind = "ROLE_IMAGE_RECIPE"
	KindImageBuild          Kind = "IMAGE_BUILD"
	KindImageArtifact       Kind = "IMAGE_ARTIFACT"
)

// ProcessContinuationKind различает взаимоисключающие owner continuation.
type ProcessContinuationKind string

const (
	ProcessContinuationNone        ProcessContinuationKind = ""
	ProcessContinuationOwnerGate   ProcessContinuationKind = "OWNER_GATE"
	ProcessContinuationIntegration ProcessContinuationKind = "INTEGRATION"
)

func (kind ProcessContinuationKind) Valid() bool {
	return kind == ProcessContinuationNone || kind == ProcessContinuationOwnerGate ||
		kind == ProcessContinuationIntegration
}

// Valid сообщает принадлежность закрытому множеству.
func (kind Kind) Valid() bool {
	switch kind {
	case KindProject, KindTeam, KindChat, KindRole, KindPromptProfile,
		KindCredentialBinding, KindRepositoryWorkspace, KindIntegration,
		KindRuntimeRevision, KindSession, KindTurn, KindProcessRun,
		KindSchedule, KindOwnerGate, KindMemoryRecord, KindWorkClaim,
		KindArtifact, KindRoleImageRecipe, KindImageBuild, KindImageArtifact:
		return true
	default:
		return false
	}
}

// State — закрытый жизненный цикл агрегата.
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

// InitialState возвращает назначенное сервером начальное состояние.
func InitialState(kind Kind) State {
	switch kind {
	case KindProcessRun:
		return StateRunning
	case KindOwnerGate:
		return StateWaitingOwner
	case KindTurn:
		return StateQueued
	case KindImageBuild:
		return StateQueued
	case KindImageArtifact:
		return StateWaitingExternal
	default:
		return StateActive
	}
}

// TransitionAllowed задаёт закрытый автомат состояний для всех видов.
func TransitionAllowed(kind Kind, from, to State) bool {
	if !kind.Valid() || from == to ||
		(from.Terminal() &&
			!(kind == KindTurn && (from == StateFailed || from == StateExpired) && to == StateQueued) &&
			!(kind == KindProcessRun && (from == StateFailed || from == StateExpired) && to == StateRunning) &&
			!(kind == KindImageBuild && (from == StateFailed || from == StateExpired) && to == StateQueued) &&
			!(kind == KindSession && from == StateCancelled &&
				to == StateDeletionPending)) {
		return false
	}
	switch kind {
	case KindImageBuild:
		switch from {
		case StateQueued:
			return to == StateClaimed || to == StateCancelled || to == StateExpired
		case StateClaimed:
			return to == StateRunning || to == StateSucceeded || to == StateFailed ||
				to == StateCancelled || to == StateExpired || to == StateBlocked
		case StateRunning:
			return to == StateSucceeded || to == StateFailed || to == StateCancelled ||
				to == StateExpired || to == StateBlocked
		case StateFailed, StateExpired, StateBlocked:
			return to == StateQueued || (from == StateBlocked && to == StateCancelled)
		}
	case KindImageArtifact:
		switch from {
		case StateWaitingExternal:
			return to == StateActive || to == StateBlocked || to == StateCancelled || to == StateExpired
		case StateActive:
			return to == StateArchived || to == StateDeletionPending
		case StateBlocked:
			return to == StateArchived || to == StateDeletionPending
		case StateArchived:
			return to == StateDeletionPending
		case StateDeletionPending:
			return to == StateDeleted
		}
	case KindTurn:
		switch from {
		case StateQueued:
			return to == StateClaimed || to == StateCancelled
		case StateClaimed:
			return to == StateRunning || to == StateQueued ||
				to == StateSucceeded || to == StateFailed || to == StateCancelled ||
				to == StateExpired || to == StateWaitingExternal || to == StateWaitingOwner
		case StateRunning:
			return to == StateSucceeded || to == StateFailed ||
				to == StateCancelled || to == StateExpired || to == StateBlocked ||
				to == StateWaitingExternal || to == StateWaitingOwner
		case StateWaitingExternal, StateWaitingOwner, StateBlocked:
			return to == StateQueued || to == StateSucceeded ||
				to == StateCancelled || to == StateFailed
		case StateFailed, StateExpired:
			return to == StateQueued
		}
	case KindSession:
		switch from {
		case StateActive:
			return to == StateQueued || to == StateArchived ||
				to == StateCancelled || to == StateBlocked ||
				to == StateWaitingExternal
		case StateQueued:
			return to == StateRunning || to == StateCancelled || to == StateBlocked
		case StateRunning:
			return to == StateActive || to == StateWaitingOwner ||
				to == StateWaitingExternal || to == StateBlocked || to == StateFailed
		case StateWaitingExternal, StateWaitingOwner, StateBlocked:
			return to == StateQueued || to == StateArchived ||
				to == StateCancelled || to == StateFailed
		case StateArchived, StateCancelled:
			return to == StateDeletionPending
		case StateDeletionPending:
			return to == StateDeleted
		}
	case KindSchedule:
		return (from == StateActive && (to == StatePaused || to == StateArchived)) ||
			(from == StatePaused && (to == StateActive || to == StateArchived)) ||
			(from == StateArchived && to == StateDeletionPending) ||
			(from == StateDeletionPending && to == StateDeleted)
	case KindOwnerGate:
		return from == StateWaitingOwner &&
			(to == StateSucceeded || to == StateFailed || to == StateBlocked ||
				to == StateCancelled || to == StateExpired)
	case KindProcessRun:
		switch from {
		case StateRunning:
			return to == StateWaitingOwner || to == StateWaitingExternal ||
				to == StateSucceeded || to == StateFailed ||
				to == StateCancelled || to == StateExpired || to == StateBlocked
		case StateWaitingOwner, StateWaitingExternal, StateBlocked:
			return to == StateRunning || to == StateSucceeded ||
				to == StateFailed || to == StateCancelled || to == StateExpired
		case StateFailed, StateExpired:
			return to == StateRunning
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
