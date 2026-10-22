package entity

// RunSessionReadiness reports only session-related gates in the current read snapshot, including
// storage state and the latest archive task. Absence of these blockers does not prove readiness of the
// complete runtime claim chain.
// RunSessionReadiness сообщает только ограничения сессии в текущем снимке чтения, включая состояние
// хранения и последнюю задачу архива. Отсутствие этих препятствий не доказывает готовность всей
// цепочки захвата исполнения.
type RunSessionReadiness struct {
	SessionRef, StorageState, Reason string
	LatestArchiveTask                *RunSessionArchiveTask
}

// RunSessionArchiveTask presents archive task identity, kind, state, bounded failure code, and current
// and maximum attempts. It is read-only diagnostic metadata, not a lease or permission to retry or
// restore the session.
// RunSessionArchiveTask представляет личность, вид, состояние, ограниченный код ошибки, текущую и
// максимальную попытки задачи архива. Это диагностические данные чтения, а не аренда или разрешение
// повторить задачу либо восстановить сессию.
type RunSessionArchiveTask struct {
	Ref, Kind, State, SafeErrorCode string
	Attempt, MaximumAttempts        int32
}
