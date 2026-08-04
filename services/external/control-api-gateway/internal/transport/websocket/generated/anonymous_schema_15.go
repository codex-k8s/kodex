package generated

type AnonymousSchema_15 struct {
	Id        string              `json:"id" binding:"required"`
	Kind      *AnonymousSchema_17 `json:"kind" binding:"required"`
	Name      string              `json:"name" binding:"required"`
	State     *AnonymousSchema_19 `json:"state" binding:"required"`
	Version   int                 `json:"version" binding:"required"`
	ProjectId string              `json:"projectId,omitempty"`
	ParentId  string              `json:"parentId,omitempty"`
	Spec      *AnonymousSchema_23 `json:"spec" binding:"required"`
	CreatedAt string              `json:"createdAt" binding:"required"`
	UpdatedAt string              `json:"updatedAt" binding:"required"`
}
