// Package roleimage определяет transport-neutral порт supply-chain образов ролей.
package roleimage

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

type Filter struct {
	ProjectRef, RoleDefinitionRef string
	Page                          query.Page
}

type ManageInput struct {
	Principal               value.Principal
	Mutation                value.Mutation
	Action                  string
	RecipeRef, ProjectRef   string
	RoleDefinitionRef, Name string
	Recipe                  entity.RoleImageRecipeInput
}

type ManageResult struct {
	Recipe   entity.RoleImageRecipe
	Build    *entity.ImageBuild
	Artifact *entity.ImageArtifact
	Reused   bool
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
	ExpectedVersion, ExpectedFence, PolicyRevision             uint64
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

type Repository interface {
	ResolvePrincipal(context.Context, value.Principal) (value.Principal, error)
	List(context.Context, value.Principal, Filter) ([]entity.RoleImageRecipe, string, error)
	Get(context.Context, value.Principal, string) (entity.RoleImageRecipe, []entity.ImageBuild, *entity.ImageArtifact, error)
	Manage(context.Context, ManageInput) (ManageResult, error)
	ClaimBuild(context.Context, value.Principal, string) (entity.ImageBuildClaim, error)
	RenewBuild(context.Context, BuildLeaseInput) (entity.ImageBuildClaim, error)
	ReportBuildProgress(context.Context, BuildProgressInput) (entity.ImageBuild, error)
	CompleteBuild(context.Context, BuildCompletionInput) (entity.ImageBuild, entity.ImageArtifact, error)
	FailBuild(context.Context, BuildFailureInput) (entity.ImageBuild, error)
	ClaimAdmission(context.Context, value.Principal, string) (entity.ImageAdmissionClaim, error)
	RecordAdmission(context.Context, AdmissionRecordInput) (entity.ImageArtifact, error)
	ClaimPromotion(context.Context, value.Principal, string) (entity.ImagePromotionClaim, error)
	AuthorizePromotion(context.Context, PromotionAuthorizeInput) (entity.ImagePromotionAuthorization, error)
	CompletePromotion(context.Context, PromotionCompleteInput) (entity.ImageArtifact, error)
}
