package entity

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

var (
	imagePackageManagerPattern = regexp.MustCompile(`^(apk|apt|dnf|pip|npm)$`)
	imageInputNamePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+@/-]{0,127}$`)
	imageToolNamePattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	imageVersionPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~-]{0,127}$`)
	imageErrorCodePattern      = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	imageSignatureIdentity     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,255}$`)
)

type RoleImagePackage struct {
	Manager string `json:"manager"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

func (item RoleImagePackage) Validate() error {
	if !imagePackageManagerPattern.MatchString(item.Manager) ||
		!imageInputNamePattern.MatchString(item.Name) ||
		!imageVersionPattern.MatchString(item.Version) ||
		!validManifestDigest(item.Digest) {
		return errors.New("role image package is invalid")
	}
	return nil
}

type RoleImageTool struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	SourceRef string `json:"sourceRef"`
	SHA256    string `json:"sha256"`
}

func (item RoleImageTool) Validate() error {
	if !imageToolNamePattern.MatchString(item.Name) ||
		!imageVersionPattern.MatchString(item.Version) ||
		!validExternalRef(item.SourceRef) || !validSHA256(item.SHA256) {
		return errors.New("role image tool is invalid")
	}
	return nil
}

type RoleImagePlatform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Variant      string `json:"variant,omitempty"`
}

func (platform RoleImagePlatform) Validate() error {
	if platform.OS != "linux" ||
		(platform.Architecture != "amd64" && platform.Architecture != "arm64") ||
		(platform.Variant != "" && !imageVersionPattern.MatchString(platform.Variant)) {
		return errors.New("role image platform is invalid")
	}
	return nil
}

type RoleImageRecipeInput struct {
	BaseImageReference string              `json:"baseImageReference"`
	BaseImageDigest    string              `json:"baseImageDigest"`
	SourceRef          string              `json:"sourceRef"`
	SourceRevision     string              `json:"sourceRevision"`
	SourceSHA256       string              `json:"sourceSha256"`
	ContextRef         string              `json:"contextRef"`
	ContextSHA256      string              `json:"contextSha256"`
	BuilderSHA256      string              `json:"builderSha256"`
	FrontendSHA256     string              `json:"frontendSha256"`
	Platforms          []RoleImagePlatform `json:"platforms"`
	Packages           []RoleImagePackage  `json:"packages"`
	Tools              []RoleImageTool     `json:"tools"`
	InstallationBlock  string              `json:"installationBlock"`
	BuildSecretRefs    []string            `json:"buildSecretRefs,omitempty"`
	ToolchainSHA256    string              `json:"toolchainSha256"`
}

func (input RoleImageRecipeInput) Validate() error {
	if !validExternalRef(input.BaseImageReference) || strings.Contains(input.BaseImageReference, "@") ||
		!validManifestDigest(input.BaseImageDigest) ||
		!validExternalRef(input.SourceRef) || !imageVersionPattern.MatchString(input.SourceRevision) ||
		!validSHA256(input.SourceSHA256) || !strings.HasPrefix(input.ContextRef, "oci://") || !validExternalRef(input.ContextRef) ||
		!validSHA256(input.ContextSHA256) || !strings.HasSuffix(input.ContextRef, "@sha256:"+input.ContextSHA256) ||
		!validSHA256(input.BuilderSHA256) ||
		!validSHA256(input.FrontendSHA256) || !validSHA256(input.ToolchainSHA256) ||
		len(input.Platforms) == 0 || len(input.Platforms) > 8 ||
		len(input.Packages) > 256 || len(input.Tools) > 128 || len(input.BuildSecretRefs) > 32 ||
		!validInstallationBlock(input.InstallationBlock) {
		return errors.New("role image recipe input is invalid")
	}
	platformKeys := make([]string, 0, len(input.Platforms))
	for _, platform := range input.Platforms {
		if platform.Validate() != nil {
			return errors.New("role image recipe platform is invalid")
		}
		platformKeys = append(platformKeys, platform.OS+"/"+platform.Architecture+"/"+platform.Variant)
	}
	packageKeys := make([]string, 0, len(input.Packages))
	for _, item := range input.Packages {
		if item.Validate() != nil {
			return errors.New("role image recipe package is invalid")
		}
		packageKeys = append(packageKeys, item.Manager+"/"+item.Name)
	}
	toolKeys := make([]string, 0, len(input.Tools))
	for _, item := range input.Tools {
		if item.Validate() != nil {
			return errors.New("role image recipe tool is invalid")
		}
		toolKeys = append(toolKeys, item.Name)
	}
	if !uniqueSortedKeys(platformKeys) || !uniqueSortedKeys(packageKeys) || !uniqueSortedKeys(toolKeys) {
		return errors.New("role image recipe input contains duplicates")
	}
	secretRefs := slices.Clone(input.BuildSecretRefs)
	slices.Sort(secretRefs)
	for index, reference := range secretRefs {
		if !validImmutableSecretRef(reference) || (index > 0 && reference == secretRefs[index-1]) {
			return errors.New("role image build secret reference is invalid")
		}
	}
	return nil
}

type RoleImageRecipeSpec struct {
	Input          RoleImageRecipeInput `json:"input"`
	Generation     uint64               `json:"generation"`
	SpecSHA256     string               `json:"specSha256"`
	PolicyRevision uint64               `json:"policyRevision"`
	PolicySHA256   string               `json:"policySha256"`
}

func (RoleImageRecipeSpec) Kind() enum.Kind { return enum.KindRoleImageRecipe }

func (spec RoleImageRecipeSpec) Validate() error {
	if spec.Input.Validate() != nil || spec.Generation == 0 || !validSHA256(spec.SpecSHA256) ||
		spec.PolicyRevision == 0 || !validSHA256(spec.PolicySHA256) {
		return errors.New("role image recipe specification is invalid")
	}
	return nil
}

type ImageBuildStage string

const (
	ImageBuildStageQueued            ImageBuildStage = "QUEUED"
	ImageBuildStageContextValidation ImageBuildStage = "CONTEXT_VALIDATION"
	ImageBuildStageBasePull          ImageBuildStage = "BASE_PULL"
	ImageBuildStageSolving           ImageBuildStage = "SOLVING"
	ImageBuildStageStagingPush       ImageBuildStage = "STAGING_PUSH"
	ImageBuildStageProvenance        ImageBuildStage = "PROVENANCE"
	ImageBuildStageCompleted         ImageBuildStage = "COMPLETED"
	ImageBuildStageFailed            ImageBuildStage = "FAILED"
	ImageBuildStageCancelled         ImageBuildStage = "CANCELLED"
	ImageBuildStageExpired           ImageBuildStage = "EXPIRED"
	ImageBuildStageDeadLetter        ImageBuildStage = "DEAD_LETTER"
)

func (stage ImageBuildStage) Valid() bool {
	switch stage {
	case ImageBuildStageQueued, ImageBuildStageContextValidation, ImageBuildStageBasePull,
		ImageBuildStageSolving, ImageBuildStageStagingPush, ImageBuildStageProvenance,
		ImageBuildStageCompleted, ImageBuildStageFailed, ImageBuildStageCancelled,
		ImageBuildStageExpired, ImageBuildStageDeadLetter:
		return true
	default:
		return false
	}
}

type ImageBuildSpec struct {
	RecipeID             string          `json:"recipeId"`
	RecipeVersion        uint64          `json:"recipeVersion"`
	RecipeGeneration     uint64          `json:"recipeGeneration"`
	SpecSHA256           string          `json:"specSha256"`
	Attempt              uint32          `json:"attempt"`
	ClaimantWorkloadID   string          `json:"claimantWorkloadId,omitempty"`
	ClaimantSPIFFEID     string          `json:"claimantSpiffeId,omitempty"`
	AuthorityGeneration  uint64          `json:"authorityGeneration"`
	Fence                uint64          `json:"fence"`
	LeaseExpiresAt       time.Time       `json:"leaseExpiresAt,omitempty"`
	Stage                ImageBuildStage `json:"stage"`
	ProgressPercent      uint32          `json:"progressPercent"`
	StagingReference     string          `json:"stagingReference,omitempty"`
	ManifestDigest       string          `json:"manifestDigest,omitempty"`
	ProvenanceSHA256     string          `json:"provenanceSha256,omitempty"`
	ImmutableBuildSHA256 string          `json:"immutableBuildSha256"`
	ErrorCode            string          `json:"errorCode,omitempty"`
	AvailableAt          time.Time       `json:"availableAt"`
	MaximumAttempts      uint32          `json:"maximumAttempts"`
	LeaseTokenSHA256     string          `json:"leaseTokenSha256,omitempty"`
	ClaimJTISHA256       string          `json:"claimJtiSha256,omitempty"`
}

func (ImageBuildSpec) Kind() enum.Kind { return enum.KindImageBuild }

func (spec ImageBuildSpec) Validate() error {
	if value.ValidateID(spec.RecipeID) != nil || spec.RecipeVersion == 0 || spec.RecipeGeneration == 0 ||
		!validSHA256(spec.SpecSHA256) || spec.Attempt == 0 || spec.Attempt > 10 ||
		!spec.Stage.Valid() || spec.ProgressPercent > 100 || !validSHA256(spec.ImmutableBuildSHA256) ||
		spec.AvailableAt.IsZero() || spec.MaximumAttempts == 0 || spec.MaximumAttempts > 10 ||
		spec.Attempt > spec.MaximumAttempts {
		return errors.New("image build specification is invalid")
	}
	claimed := spec.ClaimantWorkloadID != "" || spec.ClaimantSPIFFEID != "" ||
		!spec.LeaseExpiresAt.IsZero() || spec.LeaseTokenSHA256 != "" || spec.ClaimJTISHA256 != ""
	if claimed && (!imageInputNamePattern.MatchString(spec.ClaimantWorkloadID) ||
		!strings.HasPrefix(spec.ClaimantSPIFFEID, "spiffe://mattercodex.local/") ||
		spec.AuthorityGeneration == 0 || spec.Fence == 0 || spec.LeaseExpiresAt.IsZero() ||
		!validSHA256(spec.LeaseTokenSHA256) || !validSHA256(spec.ClaimJTISHA256)) {
		return errors.New("image build claim is invalid")
	}
	if !claimed && (spec.Stage != ImageBuildStageQueued && spec.Stage != ImageBuildStageFailed &&
		spec.Stage != ImageBuildStageCancelled && spec.Stage != ImageBuildStageExpired &&
		spec.Stage != ImageBuildStageDeadLetter && spec.Stage != ImageBuildStageCompleted) {
		return errors.New("image build stage requires a claim")
	}
	completed := spec.StagingReference != "" || spec.ManifestDigest != "" || spec.ProvenanceSHA256 != ""
	if completed && (!validImageReference(spec.StagingReference) || !validManifestDigest(spec.ManifestDigest) ||
		!strings.HasSuffix(spec.StagingReference, "@"+spec.ManifestDigest) ||
		!validSHA256(spec.ProvenanceSHA256) || spec.Stage != ImageBuildStageCompleted) {
		return errors.New("completed image build evidence is invalid")
	}
	if spec.ErrorCode != "" && !imageErrorCodePattern.MatchString(spec.ErrorCode) {
		return errors.New("image build error code is invalid")
	}
	return nil
}

type ImageAdmissionVerdict string

const (
	ImageAdmissionAccepted ImageAdmissionVerdict = "ACCEPTED"
	ImageAdmissionRejected ImageAdmissionVerdict = "REJECTED"
)

type ImageArtifactSpec struct {
	RecipeID                          string                `json:"recipeId"`
	RecipeVersion                     uint64                `json:"recipeVersion"`
	RecipeGeneration                  uint64                `json:"recipeGeneration"`
	SpecSHA256                        string                `json:"specSha256"`
	BuildID                           string                `json:"buildId"`
	BuildVersion                      uint64                `json:"buildVersion"`
	BuildAttempt                      uint32                `json:"buildAttempt"`
	StagingReference                  string                `json:"stagingReference"`
	ManifestDigest                    string                `json:"manifestDigest"`
	ProvenanceSHA256                  string                `json:"provenanceSha256"`
	ImmutableBuildSHA256              string                `json:"immutableBuildSha256"`
	BaseImageDigest                   string                `json:"baseImageDigest"`
	SourceSHA256                      string                `json:"sourceSha256"`
	ContextSHA256                     string                `json:"contextSha256"`
	BuilderSHA256                     string                `json:"builderSha256"`
	FrontendSHA256                    string                `json:"frontendSha256"`
	ToolchainSHA256                   string                `json:"toolchainSha256"`
	Platforms                         []RoleImagePlatform   `json:"platforms"`
	SBOMSHA256                        string                `json:"sbomSha256,omitempty"`
	VulnerabilityEvidenceSHA256       string                `json:"vulnerabilityEvidenceSha256,omitempty"`
	PolicyRevision                    uint64                `json:"policyRevision"`
	PolicySHA256                      string                `json:"policySha256"`
	AdmissionVerdict                  ImageAdmissionVerdict `json:"admissionVerdict,omitempty"`
	SignatureIdentity                 string                `json:"signatureIdentity,omitempty"`
	SignatureSHA256                   string                `json:"signatureSha256,omitempty"`
	AdmissionRevision                 uint64                `json:"admissionRevision"`
	AdmissionReceiptSHA256            string                `json:"admissionReceiptSha256,omitempty"`
	AdmissionReceiptOCIManifestDigest string                `json:"admissionReceiptOciManifestDigest,omitempty"`
	PromotionClaimJTISHA256           string                `json:"promotionClaimJtiSha256,omitempty"`
	PromotionClaimExpiresAt           time.Time             `json:"promotionClaimExpiresAt,omitempty"`
	PromotionClaimantWorkloadID       string                `json:"promotionClaimantWorkloadId,omitempty"`
	PromotionClaimantSPIFFEID         string                `json:"promotionClaimantSpiffeId,omitempty"`
	PromotionAuthorityGeneration      uint64                `json:"promotionAuthorityGeneration"`
	PromotionFence                    uint64                `json:"promotionFence"`
	PromotedReference                 string                `json:"promotedReference,omitempty"`
	PromotionReadbackSHA256           string                `json:"promotionReadbackSha256,omitempty"`
	PromotedAt                        time.Time             `json:"promotedAt,omitempty"`
	AdmissionClaimantWorkloadID       string                `json:"admissionClaimantWorkloadId,omitempty"`
	AdmissionAuthorityGeneration      uint64                `json:"admissionAuthorityGeneration"`
	AdmissionFence                    uint64                `json:"admissionFence"`
	AdmissionClaimTokenSHA256         string                `json:"admissionClaimTokenSha256,omitempty"`
	AdmissionClaimExpiresAt           time.Time             `json:"admissionClaimExpiresAt,omitempty"`
}

func (ImageArtifactSpec) Kind() enum.Kind { return enum.KindImageArtifact }

func (spec ImageArtifactSpec) Validate() error {
	if value.ValidateID(spec.RecipeID) != nil || spec.RecipeVersion == 0 || spec.RecipeGeneration == 0 ||
		!validSHA256(spec.SpecSHA256) || value.ValidateID(spec.BuildID) != nil || spec.BuildVersion == 0 ||
		spec.BuildAttempt == 0 || !validImageReference(spec.StagingReference) ||
		!validManifestDigest(spec.ManifestDigest) || !validSHA256(spec.ProvenanceSHA256) ||
		!strings.HasSuffix(spec.StagingReference, "@"+spec.ManifestDigest) ||
		!validSHA256(spec.ImmutableBuildSHA256) || !validManifestDigest(spec.BaseImageDigest) ||
		!validSHA256(spec.SourceSHA256) || !validSHA256(spec.ContextSHA256) || !validSHA256(spec.BuilderSHA256) ||
		!validSHA256(spec.FrontendSHA256) || !validSHA256(spec.ToolchainSHA256) || len(spec.Platforms) == 0 ||
		len(spec.Platforms) > 8 || spec.PolicyRevision == 0 || !validSHA256(spec.PolicySHA256) {
		return errors.New("image artifact specification is invalid")
	}
	platformKeys := make([]string, 0, len(spec.Platforms))
	for _, platform := range spec.Platforms {
		if platform.Validate() != nil {
			return errors.New("image artifact platform is invalid")
		}
		platformKeys = append(platformKeys, platform.OS+"/"+platform.Architecture+"/"+platform.Variant)
	}
	if !uniqueSortedKeys(platformKeys) {
		return errors.New("image artifact platforms contain duplicates")
	}
	claimed := spec.AdmissionClaimantWorkloadID != "" ||
		spec.AdmissionClaimTokenSHA256 != "" || !spec.AdmissionClaimExpiresAt.IsZero()
	if claimed && (!imageInputNamePattern.MatchString(spec.AdmissionClaimantWorkloadID) ||
		spec.AdmissionAuthorityGeneration == 0 || spec.AdmissionFence == 0 ||
		!validSHA256(spec.AdmissionClaimTokenSHA256) || spec.AdmissionClaimExpiresAt.IsZero()) {
		return errors.New("image admission claim is invalid")
	}
	admitted := spec.AdmissionVerdict != "" || spec.SBOMSHA256 != "" ||
		spec.VulnerabilityEvidenceSHA256 != "" || spec.SignatureIdentity != "" ||
		spec.SignatureSHA256 != "" || spec.AdmissionRevision != 0 || spec.AdmissionReceiptSHA256 != "" ||
		spec.AdmissionReceiptOCIManifestDigest != ""
	if admitted && ((spec.AdmissionVerdict != ImageAdmissionAccepted && spec.AdmissionVerdict != ImageAdmissionRejected) ||
		!validSHA256(spec.SBOMSHA256) || !validSHA256(spec.VulnerabilityEvidenceSHA256) ||
		!imageSignatureIdentity.MatchString(spec.SignatureIdentity) || !validSHA256(spec.SignatureSHA256) ||
		spec.AdmissionRevision == 0 || !validSHA256(spec.AdmissionReceiptSHA256) ||
		!validManifestDigest(spec.AdmissionReceiptOCIManifestDigest)) {
		return errors.New("image admission evidence is invalid")
	}
	promotionClaim := spec.PromotionClaimJTISHA256 != "" || !spec.PromotionClaimExpiresAt.IsZero()
	if promotionClaim && (spec.AdmissionVerdict != ImageAdmissionAccepted ||
		!validSHA256(spec.PromotionClaimJTISHA256) || spec.PromotionClaimExpiresAt.IsZero() ||
		!imageInputNamePattern.MatchString(spec.PromotionClaimantWorkloadID) ||
		!strings.HasPrefix(spec.PromotionClaimantSPIFFEID, "spiffe://mattercodex.local/") ||
		spec.PromotionAuthorityGeneration == 0 || spec.PromotionFence == 0) {
		return errors.New("image promotion claim is invalid")
	}
	if !promotionClaim && (spec.PromotionClaimantWorkloadID != "" || spec.PromotionClaimantSPIFFEID != "") {
		return errors.New("image promotion claimant is invalid")
	}
	promoted := spec.PromotedReference != "" || spec.PromotionReadbackSHA256 != "" || !spec.PromotedAt.IsZero()
	if promoted && (spec.AdmissionVerdict != ImageAdmissionAccepted ||
		!validImageReference(spec.PromotedReference) || !strings.HasSuffix(spec.PromotedReference, "@"+spec.ManifestDigest) ||
		!validSHA256(spec.PromotionReadbackSHA256) || spec.PromotedAt.IsZero() || promotionClaim) {
		return errors.New("image promotion evidence is invalid")
	}
	return nil
}

func validManifestDigest(input string) bool {
	return strings.HasPrefix(input, "sha256:") && validSHA256(strings.TrimPrefix(input, "sha256:"))
}

func validImageReference(input string) bool {
	if !validExternalRef(input) || strings.Count(input, "@") != 1 {
		return false
	}
	parts := strings.SplitN(input, "@", 2)
	return len(parts[0]) >= 3 && !strings.ContainsAny(parts[0], "?# ") && validManifestDigest(parts[1])
}

func validInstallationBlock(input string) bool {
	if len(input) == 0 || len(input) > 32768 || !utf8.ValidString(input) || strings.TrimSpace(input) == "" {
		return false
	}
	for _, symbol := range input {
		if symbol == '\n' || symbol == '\t' {
			continue
		}
		if symbol < 0x20 || symbol == 0x7f {
			return false
		}
	}
	return true
}

func uniqueSortedKeys(values []string) bool {
	copyValues := slices.Clone(values)
	slices.Sort(copyValues)
	for index, item := range copyValues {
		if item == "" || (index > 0 && item == copyValues[index-1]) {
			return false
		}
	}
	return true
}
