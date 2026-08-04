package generated

type AnonymousSchema_157 struct {
	ProcessRunId        string   `json:"processRunId" binding:"required"`
	TurnId              string   `json:"turnId" binding:"required"`
	Domains             []string `json:"domains" binding:"required"`
	ResourceKeys        []string `json:"resourceKeys" binding:"required"`
	WorkloadId          string   `json:"workloadId" binding:"required"`
	SessionId           string   `json:"sessionId" binding:"required"`
	Attempt             int      `json:"attempt" binding:"required"`
	ExpiresAt           string   `json:"expiresAt" binding:"required"`
	AuthorityGeneration int      `json:"authorityGeneration" binding:"required"`
}
