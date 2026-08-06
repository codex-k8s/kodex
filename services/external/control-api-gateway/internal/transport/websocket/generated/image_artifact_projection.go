
package generated

type ImageArtifactProjection struct {
  RecipeId string
  RecipeVersion int
  RecipeGeneration int
  SpecSha256 string
  BuildId string
  BuildVersion int
  BuildAttempt int
  StagingReference string
  ManifestDigest string
  ProvenanceSha256 string
  ImmutableBuildSha256 string
  BaseImageDigest string
  SourceSha256 string
  ContextSha256 string
  BuilderSha256 string
  FrontendSha256 string
  ToolchainSha256 string
  RoleRuntimeContractRevision int
  RoleRuntimeContractSha256 string
  Platforms []RoleImagePlatform
  SbomSha256 string
  VulnerabilityEvidenceSha256 string
  PolicyRevision int
  PolicySha256 string
  AdmissionVerdict *ImageAdmissionVerdict
  SignatureIdentity string
  SignatureSha256 string
  AdmissionRevision int
  AdmissionReceiptSha256 string
  AdmissionReceiptOciManifestDigest string
  PromotedReference string
  PromotionReadbackSha256 string
  PromotedAt string
}