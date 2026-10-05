// Package roleimage определяет transport-neutral порт supply-chain образов ролей.
package roleimage

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type Filter struct {
	ScopeKind                     string
	ProjectRef, RoleDefinitionRef string
	Query, State                  string
	Page                          query.Page
}

type ManageInput struct {
	ScopeKind               string
	Principal               value.Principal
	Mutation                value.Mutation
	Action                  string
	RecipeRef, ProjectRef   string
	BuildRef                string
	RoleDefinitionRef, Name string
	Recipe                  entity.RoleImageRecipeInput
	Environment             entity.RoleEnvironmentSelection
}

type ManageResult struct {
	Recipe   entity.RoleImageRecipe
	Build    *entity.ImageBuild
	Artifact *entity.ImageArtifact
	Reused   bool
}

type Detail struct {
	Recipe             entity.RoleImageRecipe
	Builds             []entity.ImageBuild
	ActiveArtifact     *entity.ImageArtifact
	PromotionCandidate *entity.ImageArtifact
	AdmissionFailure   *entity.RoleImageAdmissionFailure
}

type BuildLeaseInput struct {
	Principal                            value.Principal
	IdempotencyKey, BuildRef, LeaseToken string
	ExpectedVersion, ExpectedFence       uint64
	ExpectedAttempt                      uint32
}

type BuildProgressInput struct {
	BuildLeaseInput
	Stage           string
	ProgressPercent uint32
}

type BuildCompletionInput struct {
	BuildLeaseInput
	StagingReference, ManifestDigest, ProvenanceSHA256, ImmutableBuildSHA256 string
}

type BuildFailureInput struct {
	BuildLeaseInput
	ErrorCode, DiagnosticCode, DiagnosticSummary string
}

type AdmissionRecordInput struct {
	Principal                                                  value.Principal
	IdempotencyKey, ArtifactRef, ClaimToken, ManifestDigest    string
	ImmutableBuildSHA256, ProvenanceSHA256, SBOMSHA256         string
	VulnerabilityEvidenceSHA256, PolicySHA256, Verdict         string
	SignatureIdentity, SignatureSHA256, AdmissionReceiptSHA256 string
	AdmissionReceiptOCIManifestDigest                          string
	ToolInventoryJSON, ToolInventorySHA256                     string
	ExpectedVersion, ExpectedFence, PolicyRevision             uint64
}

type AdmissionFailureInput struct {
	Principal                                                                                     value.Principal
	IdempotencyKey, ArtifactRef, ClaimToken                                                       string
	ManifestDigest, ImmutableBuildSHA256, ProvenanceSHA256, PolicySHA256                          string
	BuildRef, SpecSHA256, ErrorCode                                                               string
	ExpectedVersion, ExpectedFence, ExpectedAuthorityGeneration, PolicyRevision, RecipeGeneration uint64
	ExpectedBuildAttempt                                                                          uint32
}

// Expiry не принимает worker token или назначаемую caller причину.
type AdmissionExpiryInput struct {
	Principal                                                                                     value.Principal
	IdempotencyKey, ArtifactRef, ManifestDigest, ImmutableBuildSHA256, ProvenanceSHA256           string
	PolicySHA256, BuildRef, SpecSHA256                                                            string
	ExpectedVersion, ExpectedFence, ExpectedAuthorityGeneration, PolicyRevision, RecipeGeneration uint64
	ExpectedBuildAttempt                                                                          uint32
}

type PromotionAuthorizeInput struct {
	Principal                                                   value.Principal
	IdempotencyKey, ArtifactRef, PromotionClaim, ManifestDigest string
	ExpectedVersion                                             uint64
}

type PromotionCompleteInput struct {
	Principal                                                  value.Principal
	IdempotencyKey, ArtifactRef, AuthorizationToken            string
	PromotedReference, ManifestDigest, PromotionReadbackSHA256 string
	ExpectedVersion                                            uint64
}

type PromotionRequestInput struct {
	ScopeKind                                        string
	Principal                                        value.Principal
	Mutation                                         value.Mutation
	RecipeRef, ArtifactRef, ExpectedProvenanceSHA256 string
}

type SupplyWorkAvailability struct {
	AdmissionAvailable bool
	PromotionAvailable bool
}

type Repository interface {
	ListOrganizationRevisions(context.Context, value.Principal, string, query.Page) ([]entity.RoleImageRecipeRevision, string, error)
	ResolvePrincipal(context.Context, value.Principal) (value.Principal, error)
	List(context.Context, value.Principal, Filter) ([]entity.RoleImageRecipe, string, int64, error)
	Get(context.Context, value.Principal, string) (Detail, error)
	Manage(context.Context, ManageInput) (ManageResult, error)
	ListOrganization(context.Context, value.Principal, Filter) ([]entity.RoleImageRecipe, string, int64, error)
	GetOrganization(context.Context, value.Principal, string) (Detail, error)
	ManageOrganization(context.Context, ManageInput) (ManageResult, error)
	RequestOrganizationPromotion(context.Context, PromotionRequestInput) (entity.RoleImagePromotionReceipt, error)
	ClaimBuild(context.Context, value.Principal, string) (entity.ImageBuildClaim, error)
	RenewBuild(context.Context, BuildLeaseInput) (entity.ImageBuildClaim, error)
	ReportBuildProgress(context.Context, BuildProgressInput) (entity.ImageBuild, error)
	CompleteBuild(context.Context, BuildCompletionInput) (entity.ImageBuild, entity.ImageArtifact, error)
	FailBuild(context.Context, BuildFailureInput) (entity.ImageBuild, error)
	GetSupplyWorkAvailability(context.Context, value.Principal) (SupplyWorkAvailability, error)
	ClaimAdmission(context.Context, value.Principal, string) (entity.ImageAdmissionClaim, error)
	RecordAdmission(context.Context, AdmissionRecordInput) (entity.ImageArtifact, error)
	FailAdmission(context.Context, AdmissionFailureInput) (entity.RoleImageAdmissionFailure, error)
	ExpireAdmission(context.Context, AdmissionExpiryInput) (entity.RoleImageAdmissionFailure, error)
	ClaimPromotion(context.Context, value.Principal, string) (entity.ImagePromotionClaim, error)
	RequestPromotion(context.Context, PromotionRequestInput) (entity.RoleImagePromotionReceipt, error)
	AuthorizePromotion(context.Context, PromotionAuthorizeInput) (entity.ImagePromotionAuthorization, error)
	CompletePromotion(context.Context, PromotionCompleteInput) (entity.ImageArtifact, error)
}
