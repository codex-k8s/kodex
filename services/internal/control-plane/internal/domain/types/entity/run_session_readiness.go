package entity

// RunSessionReadiness описывает только session-gates в текущем read snapshot.
// Отсутствие этих блокеров не доказывает готовность всей runtime claim цепочки.
type RunSessionReadiness struct {
	SessionRef, StorageState, Reason string
	LatestArchiveTask                *RunSessionArchiveTask
}

type RunSessionArchiveTask struct {
	Ref, Kind, State, SafeErrorCode string
	Attempt, MaximumAttempts        int32
}

