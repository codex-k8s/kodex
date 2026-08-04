package generated

type AnonymousSchema_182 struct {
	IncidentId     string               `json:"incidentId" binding:"required"`
	ExecutionId    string               `json:"executionId" binding:"required"`
	ExecutionFence int                  `json:"executionFence" binding:"required"`
	Kind           *AnonymousSchema_186 `json:"kind" binding:"required"`
	EvidenceSha256 string               `json:"evidenceSha256" binding:"required"`
	WorkloadId     string               `json:"workloadId" binding:"required"`
	OccurredAt     string               `json:"occurredAt" binding:"required"`
}
