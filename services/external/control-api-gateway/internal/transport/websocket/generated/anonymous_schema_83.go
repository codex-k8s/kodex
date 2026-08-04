package generated

type AnonymousSchema_83 struct {
	RepositoryRef       string              `json:"repositoryRef" binding:"required"`
	WorkspaceMode       string              `json:"workspaceMode" binding:"required"`
	DefaultBranch       string              `json:"defaultBranch" binding:"required"`
	CredentialBindingId string              `json:"credentialBindingId,omitempty"`
	Ownership           *AnonymousSchema_28 `json:"ownership" binding:"required"`
}
