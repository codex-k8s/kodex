package entity

// ProjectPurgeRecovery не раскрывает object keys и описывает логический отказ
// отдельно от подтверждённого удаления физических байтов.
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
