package resource

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
)

func TestPromotedImageReuseDoesNotCrossOwnerBoundary(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	var artifact entity.Resource
	for _, candidate := range fixture.tx.resources {
		if candidate.Kind == enum.KindImageArtifact {
			artifact = candidate
			break
		}
	}
	if artifact.ID == "" {
		t.Fatal("image artifact fixture is absent")
	}
	spec := artifact.Spec.(entity.ImageArtifactSpec)
	_, err := fixture.tx.PromotedImageArtifactBySpec(t.Context(), artifact.OrganizationID,
		artifact.ProjectID, "00000000-0000-0000-0000-000000000001", spec.SpecSHA256,
		spec.PolicyRevision, spec.PolicySHA256)
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign owner reused promoted image artifact: %v", err)
	}
}

func TestRoleImageCanonicalHashCoversInstallationAndNormalizesSets(t *testing.T) {
	digest := strings.Repeat("a", 64)
	service := &Service{imagePolicyRevision: 7, imagePolicySHA256: strings.Repeat("b", 64),
		trustedRoleBaseRepository: "registry.example.test/base/runtime", trustedRoleBaseDigest: "sha256:" + digest,
		roleImageInputRepository:    "example.test/inputs",
		roleRuntimeContractRevision: 1, roleRuntimeContractSHA256: strings.Repeat("c", 64)}
	input := entity.RoleImageRecipeInput{
		BaseImageReference: "registry.example.test/base/runtime", BaseImageDigest: "sha256:" + digest,
		SourceRef: "git://example.test/runtime", SourceRevision: "v1", SourceSHA256: digest,
		ContextRef: "oci://example.test/inputs@sha256:" + digest, ContextSHA256: digest,
		BuilderSHA256: digest, FrontendSHA256: digest, ToolchainSHA256: digest,
		Platforms:         []entity.RoleImagePlatform{{OS: "linux", Architecture: "arm64"}, {OS: "linux", Architecture: "amd64"}},
		Packages:          []entity.RoleImagePackage{{Manager: "apt", Name: "jq", Version: "1.7", Digest: "sha256:" + digest, SourceRef: "oci://example.test/inputs@sha256:" + digest}},
		Tools:             []entity.RoleImageTool{{Name: "cosign", Version: "3.0.2", SourceRef: "oci://example.test/inputs@sha256:" + digest, SHA256: digest}},
		InstallationBlock: "printf 'first\\n'", BuildSecretRefs: []string{"vault-versioned://builder/token/v1"},
	}
	first, err := service.newRoleImageRecipeSpec(canonicalRoleImageInput(input), 1)
	if err != nil {
		t.Fatalf("first specification rejected: %v", err)
	}
	reordered := input
	reordered.Platforms = []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}, {OS: "linux", Architecture: "arm64"}}
	second, err := service.newRoleImageRecipeSpec(canonicalRoleImageInput(reordered), 2)
	if err != nil {
		t.Fatalf("reordered specification rejected: %v", err)
	}
	if first.SpecSHA256 != second.SpecSHA256 {
		t.Fatal("canonical set ordering changed the specification hash")
	}
	reordered.InstallationBlock = "printf 'second\\n'"
	changed, err := service.newRoleImageRecipeSpec(canonicalRoleImageInput(reordered), 3)
	if err != nil {
		t.Fatalf("changed specification rejected: %v", err)
	}
	if changed.SpecSHA256 == first.SpecSHA256 {
		t.Fatal("installation block did not change the specification hash")
	}
	reordered.InstallationBlock = ""
	empty, err := service.newRoleImageRecipeSpec(canonicalRoleImageInput(reordered), 4)
	if err != nil {
		t.Fatalf("canonical empty installation block rejected: %v", err)
	}
	secondEmpty, err := service.newRoleImageRecipeSpec(canonicalRoleImageInput(reordered), 5)
	if err != nil || secondEmpty.SpecSHA256 != empty.SpecSHA256 {
		t.Fatal("canonical empty installation block hash is unstable")
	}
}

func TestRecipeCancellationRevokesAdmissionAndPromotionClaims(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	var artifact entity.Resource
	for _, candidate := range fixture.tx.resources {
		if candidate.Kind == enum.KindImageArtifact {
			artifact = candidate
			break
		}
	}
	if artifact.ID == "" {
		t.Fatal("image artifact fixture is absent")
	}
	spec := artifact.Spec.(entity.ImageArtifactSpec)
	spec.PromotedReference, spec.PromotionReadbackSHA256 = "", ""
	spec.PromotedAt = time.Time{}
	spec.AdmissionClaimantWorkloadID = "image-admission"
	spec.AdmissionAuthorityGeneration, spec.AdmissionFence = 1, 1
	spec.AdmissionClaimTokenSHA256 = strings.Repeat("a", 64)
	spec.AdmissionClaimExpiresAt = fixture.tx.now.Add(time.Minute)
	spec.PromotionClaimJTISHA256 = strings.Repeat("b", 64)
	spec.PromotionClaimExpiresAt = fixture.tx.now.Add(time.Minute)
	spec.PromotionClaimantWorkloadID = "image-promotion"
	spec.PromotionClaimantSPIFFEID = "spiffe://mattercodex.local/ns/mattercodex-system/sa/image-promotion"
	spec.PromotionAuthorityGeneration, spec.PromotionFence = 1, 1
	artifact.Spec, artifact.State = spec, enum.StateWaitingExternal
	fixture.tx.resources[artifact.ID] = artifact

	principal := fixture.principal(permissionManageRoleImageRecipe, controlAPIGatewayWorkload, controlAPIGatewaySPIFFEID)
	if err := fixture.service.cancelRecipeWork(t.Context(), fixture.tx, fixture.tx,
		principal, spec.RecipeID, fixture.tx.now); err != nil {
		t.Fatalf("cancel recipe image work: %v", err)
	}
	updated := fixture.tx.resources[artifact.ID]
	updatedSpec := updated.Spec.(entity.ImageArtifactSpec)
	if updated.State != enum.StateCancelled || updatedSpec.AdmissionClaimTokenSHA256 != "" ||
		updatedSpec.AdmissionClaimantWorkloadID != "" || !updatedSpec.AdmissionClaimExpiresAt.IsZero() ||
		updatedSpec.PromotionClaimJTISHA256 != "" || updatedSpec.PromotionClaimantWorkloadID != "" ||
		updatedSpec.PromotionClaimantSPIFFEID != "" || !updatedSpec.PromotionClaimExpiresAt.IsZero() {
		t.Fatal("recipe cancellation left an admission or promotion claim reachable")
	}
}

func TestImageBuildIdempotencyBindsFailurePayload(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	var imageBuild entity.Resource
	for _, candidate := range fixture.tx.resources {
		if candidate.Kind == enum.KindImageBuild {
			imageBuild = candidate
			break
		}
	}
	if imageBuild.ID == "" {
		t.Fatal("image build fixture is absent")
	}
	leaseToken := "bounded-test-lease-token"
	spec := imageBuild.Spec.(entity.ImageBuildSpec)
	spec.Stage, spec.ProgressPercent = entity.ImageBuildStageContextValidation, 15
	spec.StagingReference, spec.ManifestDigest, spec.ProvenanceSHA256 = "", "", ""
	spec.ClaimantWorkloadID = "role-image-builder"
	spec.ClaimantSPIFFEID = "spiffe://mattercodex.local/ns/mattercodex-system/sa/role-image-builder"
	spec.AuthorityGeneration, spec.Fence = 1, 1
	spec.LeaseExpiresAt = fixture.tx.now.Add(time.Minute)
	spec.LeaseTokenSHA256 = hashString(leaseToken)
	spec.ClaimJTISHA256 = strings.Repeat("c", 64)
	imageBuild.Spec, imageBuild.State = spec, enum.StateClaimed
	fixture.tx.resources[imageBuild.ID] = imageBuild

	principal := fixture.principal(permissionFailImageBuild, "role-image-builder",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/role-image-builder")
	principal.AuthoritySource = "IMAGE_BUILD"
	base := ImageBuildLeaseInput{Principal: principal, IdempotencyKey: "fail-build-once-idempotently",
		ImageBuildID: imageBuild.ID, ExpectedVersion: imageBuild.Version,
		ExpectedAttempt: spec.Attempt, ExpectedFence: spec.Fence, LeaseToken: leaseToken}
	if _, err := fixture.service.FailImageBuild(t.Context(), FailImageBuildInput{
		ImageBuildLeaseInput: base, ErrorCode: "CONTEXT_INVALID",
	}); err != nil {
		t.Fatalf("fail image build: %v", err)
	}
	if _, err := fixture.service.FailImageBuild(t.Context(), FailImageBuildInput{
		ImageBuildLeaseInput: base, ErrorCode: "SOLVE_FAILED",
	}); !errors.Is(err, errs.ErrIdempotencyConflict) {
		t.Fatalf("changed failure payload reused semantic receipt: %v", err)
	}
}

func TestExpiredPromotionClaimIsFencedBeforeExternalCompletion(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	var artifact entity.Resource
	for _, candidate := range fixture.tx.resources {
		if candidate.Kind == enum.KindImageArtifact {
			artifact = candidate
			break
		}
	}
	if artifact.ID == "" {
		t.Fatal("image artifact fixture is absent")
	}
	spec := artifact.Spec.(entity.ImageArtifactSpec)
	spec.PromotedReference, spec.PromotionReadbackSHA256 = "", ""
	spec.PromotedAt = time.Time{}
	artifact.Spec, artifact.State = spec, enum.StateWaitingExternal
	fixture.tx.resources[artifact.ID] = artifact

	principal := fixture.principal(permissionClaimImagePromotion, "image-promotion",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/image-promotion")
	principal.AuthoritySource = "IMAGE_PROMOTION_CLAIM"
	first, err := fixture.service.ClaimImagePromotion(t.Context(), ClaimImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-claim-first",
	})
	if err != nil {
		t.Fatalf("claim promotion: %v", err)
	}
	if _, err := fixture.service.ClaimImagePromotion(t.Context(), ClaimImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-claim-concurrent",
	}); err == nil {
		t.Fatal("concurrent promotion claim replaced a live claim")
	}

	fixture.tx.now = first.ClaimExpiresAt.Add(time.Microsecond)
	second, err := fixture.service.ClaimImagePromotion(t.Context(), ClaimImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-claim-after-expiry",
	})
	if err != nil {
		t.Fatalf("replace expired promotion claim: %v", err)
	}
	if second.Fence <= first.Fence || second.AuthorityGeneration <= first.AuthorityGeneration ||
		second.ImageArtifact.Version <= first.ImageArtifact.Version {
		t.Fatal("replacement promotion claim did not advance fence, generation, and resource version")
	}

	authorizePrincipal := principal
	authorizePrincipal.Permission = permissionAuthorizeImagePromotion
	digest := spec.ManifestDigest
	if _, err := fixture.service.AuthorizeImagePromotion(t.Context(), AuthorizeImagePromotionInput{
		Principal: authorizePrincipal, IdempotencyKey: "authorize-expired-promotion",
		ImageArtifactID: artifact.ID, ExpectedVersion: first.ImageArtifact.Version,
		PromotionClaim: first.PromotionClaim, ManifestDigest: digest,
	}); err == nil {
		t.Fatal("expired superseded promotion claim authorized a side effect")
	}
}

func TestPromotionClaimIsConsumedBeforeRegistrySideEffect(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	var artifact entity.Resource
	for _, candidate := range fixture.tx.resources {
		if candidate.Kind == enum.KindImageArtifact {
			artifact = candidate
			break
		}
	}
	spec := artifact.Spec.(entity.ImageArtifactSpec)
	spec.PromotedReference, spec.PromotionReadbackSHA256 = "", ""
	spec.PromotedAt = time.Time{}
	artifact.Spec, artifact.State = spec, enum.StateWaitingExternal
	fixture.tx.resources[artifact.ID] = artifact

	principal := fixture.principal(permissionClaimImagePromotion, "image-promotion",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/image-promotion")
	principal.AuthoritySource = "IMAGE_PROMOTION_CLAIM"
	claim, err := fixture.service.ClaimImagePromotion(t.Context(), ClaimImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-claim-consume",
	})
	if err != nil {
		t.Fatalf("claim promotion: %v", err)
	}
	principal.Permission = permissionAuthorizeImagePromotion
	authorized, err := fixture.service.AuthorizeImagePromotion(t.Context(), AuthorizeImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-authorize-consume", ImageArtifactID: artifact.ID,
		ExpectedVersion: claim.ImageArtifact.Version, PromotionClaim: claim.PromotionClaim, ManifestDigest: spec.ManifestDigest,
	})
	if err != nil {
		t.Fatalf("authorize promotion: %v", err)
	}
	if authorized.AuthorizationToken == "" || !fixture.tx.now.Before(authorized.AuthorizationExpiresAt) {
		t.Fatal("promotion authorization is not bounded")
	}
	principal.Permission = permissionClaimImagePromotion
	claimReplay, err := fixture.service.ClaimImagePromotion(t.Context(), ClaimImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-claim-consume",
	})
	if err != nil || claimReplay.PromotionClaim != claim.PromotionClaim {
		t.Fatalf("durable promotion claim replay failed after authorization: %v", err)
	}
	principal.Permission = permissionAuthorizeImagePromotion
	authorizedReplay, err := fixture.service.AuthorizeImagePromotion(t.Context(), AuthorizeImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-authorize-consume", ImageArtifactID: artifact.ID,
		ExpectedVersion: claimReplay.ImageArtifact.Version, PromotionClaim: claimReplay.PromotionClaim,
		ManifestDigest: spec.ManifestDigest,
	})
	if err != nil || authorizedReplay.AuthorizationToken != authorized.AuthorizationToken {
		t.Fatalf("durable promotion authorization replay failed: %v", err)
	}
	if _, err := fixture.service.AuthorizeImagePromotion(t.Context(), AuthorizeImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-authorize-replay", ImageArtifactID: artifact.ID,
		ExpectedVersion: claim.ImageArtifact.Version, PromotionClaim: claim.PromotionClaim, ManifestDigest: spec.ManifestDigest,
	}); err == nil {
		t.Fatal("consumed promotion claim authorized a second side effect")
	}
	principal.Permission = permissionCompleteImagePromotion
	if _, err := fixture.service.CompleteImagePromotion(t.Context(), CompleteImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-complete-after-authorize",
		ImageArtifactID: artifact.ID, ExpectedVersion: authorized.ImageArtifact.Version,
		AuthorizationToken: authorized.AuthorizationToken,
		PromotedReference:  fixture.service.promotedImageRepository + "@" + spec.ManifestDigest,
		ManifestDigest:     spec.ManifestDigest, PromotionReadbackSHA256: strings.Repeat("f", 64),
	}); err != nil {
		t.Fatalf("complete authorized promotion: %v", err)
	}
	principal.Permission = permissionAuthorizeImagePromotion
	if _, err := fixture.service.AuthorizeImagePromotion(t.Context(), AuthorizeImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-authorize-consume", ImageArtifactID: artifact.ID,
		ExpectedVersion: claim.ImageArtifact.Version, PromotionClaim: claim.PromotionClaim, ManifestDigest: spec.ManifestDigest,
	}); err == nil {
		t.Fatal("completed promotion replay reissued external side-effect authority")
	}
}

func TestExpiredPromotionAuthorizationIsReclaimedWithHigherFence(t *testing.T) {
	fixture := newCurrentTupleFixture(t)
	var artifact entity.Resource
	for _, candidate := range fixture.tx.resources {
		if candidate.Kind == enum.KindImageArtifact {
			artifact = candidate
			break
		}
	}
	spec := artifact.Spec.(entity.ImageArtifactSpec)
	spec.PromotedReference, spec.PromotionReadbackSHA256 = "", ""
	spec.PromotedAt = time.Time{}
	artifact.Spec, artifact.State = spec, enum.StateWaitingExternal
	fixture.tx.resources[artifact.ID] = artifact

	principal := fixture.principal(permissionClaimImagePromotion, "image-promotion",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/image-promotion")
	principal.AuthoritySource = "IMAGE_PROMOTION_CLAIM"
	claim, err := fixture.service.ClaimImagePromotion(t.Context(), ClaimImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-claim-before-crash",
	})
	if err != nil {
		t.Fatalf("claim promotion: %v", err)
	}
	principal.Permission = permissionAuthorizeImagePromotion
	authorized, err := fixture.service.AuthorizeImagePromotion(t.Context(), AuthorizeImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-authorize-before-crash", ImageArtifactID: artifact.ID,
		ExpectedVersion: claim.ImageArtifact.Version, PromotionClaim: claim.PromotionClaim, ManifestDigest: spec.ManifestDigest,
	})
	if err != nil {
		t.Fatalf("authorize promotion: %v", err)
	}
	fixture.tx.now = authorized.AuthorizationExpiresAt.Add(time.Microsecond)
	principal.Permission = permissionClaimImagePromotion
	reclaimed, err := fixture.service.ClaimImagePromotion(t.Context(), ClaimImagePromotionInput{
		Principal: principal, IdempotencyKey: "promotion-claim-after-authorization-expiry",
	})
	if err != nil {
		t.Fatalf("reclaim expired authorization: %v", err)
	}
	if reclaimed.Fence <= claim.Fence || reclaimed.AuthorityGeneration <= claim.AuthorityGeneration ||
		reclaimed.ImageArtifact.Version <= authorized.ImageArtifact.Version {
		t.Fatal("expired promotion authorization did not advance fence, generation, and version")
	}
}
