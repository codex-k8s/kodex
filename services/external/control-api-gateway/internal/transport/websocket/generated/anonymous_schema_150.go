package generated

type AnonymousSchema_150 struct {
	Scope         string `json:"scope" binding:"required"`
	RoleId        string `json:"roleId,omitempty"`
	Title         string `json:"title" binding:"required"`
	ContentSha256 string `json:"contentSha256" binding:"required"`
	Provenance    string `json:"provenance" binding:"required"`
	Importance    int    `json:"importance" binding:"required"`
}
