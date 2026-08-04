package generated

type AnonymousSchema_134 struct {
	TargetResourceId string              `json:"targetResourceId" binding:"required"`
	TargetKind       *AnonymousSchema_17 `json:"targetKind" binding:"required"`
	TargetVersion    int                 `json:"targetVersion" binding:"required"`
	Cron             string              `json:"cron,omitempty"`
	Timezone         string              `json:"timezone" binding:"required"`
	NextRunAt        string              `json:"nextRunAt" binding:"required"`
	MaximumAttempts  int                 `json:"maximumAttempts" binding:"required"`
	Ownership        *AnonymousSchema_28 `json:"ownership" binding:"required"`
}
