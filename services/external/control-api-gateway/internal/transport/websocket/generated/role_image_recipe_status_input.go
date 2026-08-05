
package generated

// Безопасная read/status проекция без installation block и build secret references.
type RoleImageRecipeStatusInput struct {
  BaseImageReference string
  BaseImageDigest string
  SourceRef string
  SourceRevision string
  SourceSha256 string
  ContextRef string
  ContextSha256 string
  BuilderSha256 string
  FrontendSha256 string
  Platforms []RoleImagePlatform
  Packages []RoleImagePackage
  Tools []RoleImageTool
  ToolchainSha256 string
}