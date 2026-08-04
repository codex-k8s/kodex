
package generated

type RoleProjection struct {
  StableKey string
  Capabilities []string
  AllowedTargetRoleIds []string
  PromptProfileId string
  ProviderCredentialBindingIds []string
  RepositoryWorkspaceIds []string
  IntegrationIds []string
  ProviderAccountPool *ProviderPoolProjection
  Ownership *ConfigurationOwnershipProjection
}