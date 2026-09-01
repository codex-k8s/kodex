package entity

import "time"

// RoleImagePlatform задаёт одну точную целевую платформу образа роли.
type RoleImagePlatform struct {
	OS, Architecture, Variant string
}

// RoleImagePackage описывает типизированный пакет, устанавливаемый builder.
type RoleImagePackage struct {
	Manager, Name, Version, Digest, SourceRef string
}

// RoleImageTool описывает immutable OCI-вход отдельного инструмента.
type RoleImageTool struct {
	Name, Version, SourceRef, SHA256 string
}

// RoleImageRecipeInput является полной канонической спецификацией среды роли.
// Secret values в неё не входят.
type RoleImageRecipeInput struct {
	BaseImageReference, BaseImageDigest, SourceRef, SourceRevision, SourceSHA256 string
	ContextRef, ContextSHA256, BuilderSHA256, FrontendSHA256, InstallationBlock  string
	ToolchainSHA256                                                              string
	EnvironmentKey                                                               string
	PackageKeys, ToolKeys                                                        []string
	Platforms                                                                    []RoleImagePlatform
	Packages                                                                     []RoleImagePackage
	Tools                                                                        []RoleImageTool
	Dockerfile                                                                   string
}

// RoleEnvironmentSelection содержит только owner-facing выбор из
// авторитетного каталога. Supply-chain locators назначает control-plane.
type RoleEnvironmentSelection struct {
	EnvironmentKey        string
	PackageKeys, ToolKeys []string
	InstallationBlock     string
	Dockerfile            string
}

type RoleImageRecipe struct {
	Ref, ProjectRef, RoleDefinitionRef, Name, State                  string
	SpecSHA256, PolicySHA256, RoleRuntimeContractSHA256              string
	ActiveImageArtifactRef, PromotedImageReference                   string
	Version, Generation, PolicyRevision, RoleRuntimeContractRevision uint64
	Input                                                            RoleImageRecipeInput
	CreatedAt, UpdatedAt                                             time.Time
	NextActions                                                      []string
}

type ImageBuild struct {
	Ref, RecipeRef, SpecSHA256, Stage, StagingReference, ManifestDigest   string
	ProvenanceSHA256, ImmutableBuildSHA256, SafeErrorCode, DiagnosticCode string
	DiagnosticSummary, LeaseTokenSHA256, ClaimantWorkload                 string
	Version, RecipeVersion, RecipeGeneration, Fence, AuthorityGeneration  uint64
	Attempt, ProgressPercent                                              uint32
	LeaseExpiresAt                                                        *time.Time
	CreatedAt, UpdatedAt                                                  time.Time
	Dockerfile                                                            string
}

type ImageArtifact struct {
	Ref, RecipeRef, SpecSHA256, BuildRef, StagingReference, ManifestDigest        string
	ImmutableBuildSHA256, ProvenanceSHA256, BaseImageDigest, SourceSHA256         string
	ContextSHA256, BuilderSHA256, FrontendSHA256, ToolchainSHA256                 string
	PolicySHA256, SBOMSHA256, VulnerabilityEvidenceSHA256, AdmissionVerdict       string
	SignatureIdentity, SignatureSHA256, AdmissionReceiptSHA256                    string
	AdmissionReceiptOCIManifestDigest, PromotedReference, PromotionReadbackSHA256 string
	RoleRuntimeContractSHA256                                                     string
	Version, RecipeVersion, RecipeGeneration, BuildVersion, PolicyRevision        uint64
	AdmissionRevision, RoleRuntimeContractRevision                                uint64
	BuildAttempt                                                                  uint32
	Platforms                                                                     []RoleImagePlatform
	Tools                                                                         []RoleImageTool
	PromotedAt                                                                    *time.Time
	CreatedAt, UpdatedAt                                                          time.Time
}

type RoleImageBuildInput struct {
	RecipeRef, SpecSHA256, BaseImageReference, BaseImageDigest                   string
	SourceRef, SourceRevision, SourceSHA256, ContextRef, ContextSHA256           string
	BuilderSHA256, FrontendSHA256, InstallationBlock, ToolchainSHA256            string
	PolicySHA256, ImmutableBuildSHA256, RoleRuntimeContractSHA256                string
	RecipeVersion, RecipeGeneration, PolicyRevision, RoleRuntimeContractRevision uint64
	Platforms                                                                    []RoleImagePlatform
	Packages                                                                     []RoleImagePackage
	Tools                                                                        []RoleImageTool
	Dockerfile                                                                   string
}

type ImageBuildClaim struct {
	Build               ImageBuild
	Input               RoleImageBuildInput
	LeaseToken          string
	Fence               uint64
	AuthorityGeneration uint64
	LeaseExpiresAt      time.Time
}

type ImageAdmissionClaim struct {
	Artifact            ImageArtifact
	ClaimToken          string
	Fence               uint64
	AuthorityGeneration uint64
	ClaimExpiresAt      time.Time
}

type ImagePromotionClaim struct {
	Artifact            ImageArtifact
	PromotionClaim      string
	Fence               uint64
	AuthorityGeneration uint64
	ClaimExpiresAt      time.Time
}

type ImagePromotionAuthorization struct {
	Artifact               ImageArtifact
	AuthorizationToken     string
	AuthorizationExpiresAt time.Time
}

type RoleImageRecipeRevision struct {
	Ref, RecipeRef, SpecSHA256, ProvenanceSHA256, SourceSHA256 string
	ImmutableBuildSHA256, ImageArtifactRef, ManifestDigest     string
	PromotedReference, PromotionReceiptSHA256                  string
	Revision, RecipeVersion, RecipeGeneration                  uint64
	CreatedAt                                                  time.Time
}

type RoleImagePromotionReceipt struct {
	Ref, RecipeRef, ImageArtifactRef, ProvenanceSHA256, ManifestDigest string
	ReceiptSHA256, State                                               string
	CreatedAt                                                          time.Time
}
