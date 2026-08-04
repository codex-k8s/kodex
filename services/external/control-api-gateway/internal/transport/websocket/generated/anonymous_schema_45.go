package generated

type AnonymousSchema_45 struct {
	StableKey                    string              `json:"stableKey" binding:"required"`
	Capabilities                 []string            `json:"capabilities" binding:"required"`
	AllowedTargetRoleIds         []string            `json:"allowedTargetRoleIds" binding:"required"`
	PromptProfileId              string              `json:"promptProfileId" binding:"required"`
	ProviderCredentialBindingIds []string            `json:"providerCredentialBindingIds" binding:"required"`
	RepositoryWorkspaceIds       []string            `json:"repositoryWorkspaceIds" binding:"required"`
	IntegrationIds               []string            `json:"integrationIds" binding:"required"`
	ProviderAccountPool          *AnonymousSchema_58 `json:"providerAccountPool" binding:"required"`
	Ownership                    *AnonymousSchema_28 `json:"ownership" binding:"required"`
}
