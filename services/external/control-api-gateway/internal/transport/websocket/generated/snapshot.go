package generated

type Snapshot struct {
	ReservedType string             `json:"type" binding:"required"`
	RequestId    string             `json:"requestId" binding:"required"`
	Channel      *AnonymousSchema_9 `json:"channel" binding:"required"`
	Sequence     int                `json:"sequence" binding:"required"`
	SnapshotId   string             `json:"snapshotId" binding:"required"`
	Complete     bool               `json:"complete" binding:"required"`
	ServerTime   string             `json:"serverTime" binding:"required"`
	Items        *SnapshotItems     `json:"items" binding:"required"`
}
