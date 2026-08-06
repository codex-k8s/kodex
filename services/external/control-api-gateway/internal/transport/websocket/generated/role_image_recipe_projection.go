
package generated

type RoleImageRecipeProjection struct {
  // Безопасная read/status проекция без installation block и build secret references.
  Input *RoleImageRecipeStatusInput
  Generation int
  SpecSha256 string
  PolicyRevision int
  PolicySha256 string
  RoleRuntimeContractRevision int
  RoleRuntimeContractSha256 string
}