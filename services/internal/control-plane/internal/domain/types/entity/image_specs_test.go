package entity

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestImageBuildSpecBindsStagingReferenceToManifestDigest(t *testing.T) {
	digest := strings.Repeat("a", 64)
	spec := ImageBuildSpec{
		RecipeID: uuid.NewString(), RecipeVersion: 1, RecipeGeneration: 1,
		SpecSHA256: strings.Repeat("b", 64), Attempt: 1,
		Stage: ImageBuildStageCompleted, ProgressPercent: 100,
		StagingReference: "registry.example.test/staging/role@sha256:" + digest,
		ManifestDigest:   "sha256:" + digest, ProvenanceSHA256: strings.Repeat("c", 64),
		ImmutableBuildSHA256: strings.Repeat("d", 64), AvailableAt: time.Now().UTC(), MaximumAttempts: 3,
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("valid completed build rejected: %v", err)
	}
	spec.StagingReference = "registry.example.test/staging/role@sha256:" + strings.Repeat("e", 64)
	if err := spec.Validate(); err == nil {
		t.Fatal("staging reference with another manifest digest accepted")
	}
}

func TestImageArtifactSpecRequiresCompleteExactPromotionEvidence(t *testing.T) {
	digest := strings.Repeat("a", 64)
	now := time.Now().UTC()
	spec := ImageArtifactSpec{
		RecipeID: uuid.NewString(), RecipeVersion: 1, RecipeGeneration: 1,
		SpecSHA256: strings.Repeat("b", 64), BuildID: uuid.NewString(), BuildVersion: 2, BuildAttempt: 1,
		StagingReference: "registry.example.test/staging/role@sha256:" + digest,
		ManifestDigest:   "sha256:" + digest, ProvenanceSHA256: strings.Repeat("c", 64),
		ImmutableBuildSHA256: strings.Repeat("d", 64), BaseImageDigest: "sha256:" + strings.Repeat("e", 64),
		SourceSHA256: strings.Repeat("f", 64), ContextSHA256: strings.Repeat("1", 64),
		BuilderSHA256: strings.Repeat("2", 64), FrontendSHA256: strings.Repeat("3", 64),
		ToolchainSHA256: strings.Repeat("4", 64), Platforms: []RoleImagePlatform{{OS: "linux", Architecture: "amd64"}},
		SBOMSHA256: strings.Repeat("5", 64), VulnerabilityEvidenceSHA256: strings.Repeat("6", 64),
		PolicyRevision: 7, PolicySHA256: strings.Repeat("7", 64), AdmissionVerdict: ImageAdmissionAccepted,
		SignatureIdentity: "sha256:" + strings.Repeat("8", 64), SignatureSHA256: strings.Repeat("9", 64),
		AdmissionRevision: 1, AdmissionReceiptSHA256: strings.Repeat("a", 64),
		AdmissionReceiptOCIManifestDigest: "sha256:" + strings.Repeat("c", 64),
		RoleRuntimeContractRevision:       1, RoleRuntimeContractSHA256: strings.Repeat("d", 64),
		PromotedReference:       "registry.example.test/promoted/role@sha256:" + digest,
		PromotionReadbackSHA256: strings.Repeat("b", 64), PromotedAt: now,
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("valid promoted artifact rejected: %v", err)
	}

	tests := map[string]func(*ImageArtifactSpec){
		"signature missing": func(value *ImageArtifactSpec) { value.SignatureSHA256 = "" },
		"receipt OCI manifest missing": func(value *ImageArtifactSpec) {
			value.AdmissionReceiptOCIManifestDigest = ""
		},
		"staging digest mismatch": func(value *ImageArtifactSpec) {
			value.StagingReference = "registry.example.test/staging/role@sha256:" + strings.Repeat("c", 64)
		},
		"promotion digest mismatch": func(value *ImageArtifactSpec) {
			value.PromotedReference = "registry.example.test/promoted/role@sha256:" + strings.Repeat("d", 64)
		},
		"readback missing": func(value *ImageArtifactSpec) { value.PromotionReadbackSHA256 = "" },
		"live claim retained after promotion": func(value *ImageArtifactSpec) {
			value.PromotionClaimJTISHA256 = strings.Repeat("e", 64)
			value.PromotionClaimExpiresAt = now.Add(time.Minute)
			value.PromotionClaimantWorkloadID = "image-promotion"
			value.PromotionClaimantSPIFFEID = "spiffe://mattercodex.local/ns/mattercodex-system/sa/image-promotion"
			value.PromotionAuthorityGeneration, value.PromotionFence = 1, 1
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := spec
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("incomplete or mismatched evidence accepted")
			}
		})
	}
}
