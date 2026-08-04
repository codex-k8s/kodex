package generated

type Subscribe struct {
	ReservedType  string              `json:"type" binding:"required"`
	RequestId     string              `json:"requestId" binding:"required"`
	Channels      []AnonymousSchema_4 `json:"channels" binding:"required"`
	ResourceKinds []AnonymousSchema_6 `json:"resourceKinds,omitempty"`
}
