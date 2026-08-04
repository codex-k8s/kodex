package generated

type AnonymousSchema_141 struct {
	ProcessRunId         string               `json:"processRunId" binding:"required"`
	ResultSha256         string               `json:"resultSha256" binding:"required"`
	ExpiresAt            string               `json:"expiresAt" binding:"required"`
	Decision             *AnonymousSchema_145 `json:"decision" binding:"required"`
	SessionId            string               `json:"sessionId" binding:"required"`
	TurnId               string               `json:"turnId" binding:"required"`
	Attempt              int                  `json:"attempt" binding:"required"`
	ImmutableInputSha256 string               `json:"immutableInputSha256" binding:"required"`
}
