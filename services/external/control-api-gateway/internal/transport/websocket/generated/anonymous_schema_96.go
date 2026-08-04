package generated

type AnonymousSchema_96 struct {
	ManifestSha256          string `json:"manifestSha256" binding:"required"`
	ImageDigest             string `json:"imageDigest" binding:"required"`
	PromptProfileId         string `json:"promptProfileId" binding:"required"`
	PromptRevision          int    `json:"promptRevision" binding:"required"`
	SessionId               string `json:"sessionId" binding:"required"`
	RoleId                  string `json:"roleId" binding:"required"`
	ChatId                  string `json:"chatId,omitempty"`
	EffectiveRuntimeSha256  string `json:"effectiveRuntimeSha256" binding:"required"`
	AuthorityPolicyRevision int    `json:"authorityPolicyRevision" binding:"required"`
}
