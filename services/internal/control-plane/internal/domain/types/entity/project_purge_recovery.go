package entity

// ProjectPurgeRecovery exposes logical deletion and cleanup uncertainty without object storage keys,
// pinned by recovery version and snapshot digest. ProjectPresent, pending counts, and CanResolve
// describe the observed recovery state, not proof of physical byte deletion or permission to resolve
// it.
// ProjectPurgeRecovery раскрывает логическое удаление и неопределённость очистки без ключей объектного
// хранилища с привязками версии восстановления и дайджеста снимка. ProjectPresent, счётчики ожидания и
// CanResolve описывают наблюдаемое состояние, а не доказывают удаление байтов и не разрешают завершить
// восстановление.
type ProjectPurgeRecovery struct {
	ProjectRef          string `json:"projectRef"`
	Version             int64  `json:"version"`
	SnapshotDigest      string `json:"snapshotDigest"`
	State               string `json:"state"`
	UnknownContentCount int32  `json:"unknownContentCount"`
	CleanupPendingCount int32  `json:"cleanupPendingCount"`
	ProjectPresent      bool   `json:"projectPresent"`
	CanResolve          bool   `json:"canResolve"`
	ResolutionRef       string `json:"resolutionRef"`
	ProjectVersion      int64  `json:"projectVersion"`
}
