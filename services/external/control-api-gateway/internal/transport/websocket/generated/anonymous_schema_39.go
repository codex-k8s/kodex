package generated

type AnonymousSchema_39 struct {
	StableKey          string              `json:"stableKey" binding:"required"`
	RoomType           *AnonymousSchema_41 `json:"roomType" binding:"required"`
	DefaultAgentId     string              `json:"defaultAgentId,omitempty"`
	ExternalChannelRef string              `json:"externalChannelRef" binding:"required"`
	WorkPolicy         string              `json:"workPolicy" binding:"required"`
	Ownership          *AnonymousSchema_28 `json:"ownership" binding:"required"`
}
