
package generated

type RoleDefinitionProjection struct {
  StableKey string
  Description string
  Capabilities []string
  AllowedTargetRoleDefinitionRefs []string
  RoleImageRecipeRef string
  RoleImageRecipeVersion int
  RoleImageRecipeSha256 string
  Ownership *ConfigurationOwnershipProjection
}