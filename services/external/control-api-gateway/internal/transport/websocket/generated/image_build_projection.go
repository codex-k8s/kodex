
package generated

type ImageBuildProjection struct {
  RecipeId string
  RecipeVersion int
  RecipeGeneration int
  SpecSha256 string
  Attempt int
  Stage *ImageBuildStage
  ProgressPercent int
  StagingReference string
  ManifestDigest string
  ProvenanceSha256 string
  ImmutableBuildSha256 string
  ErrorCode string
  AvailableAt string
  MaximumAttempts int
}