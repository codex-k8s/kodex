package generated

type AnonymousSchema_32 struct {
	StableKey       string              `json:"stableKey" binding:"required"`
	ExternalTeamRef string              `json:"externalTeamRef" binding:"required"`
	MemberActorIds  []string            `json:"memberActorIds" binding:"required"`
	RoleIds         []string            `json:"roleIds" binding:"required"`
	Ownership       *AnonymousSchema_28 `json:"ownership" binding:"required"`
}
