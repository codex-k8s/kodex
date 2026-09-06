package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testRoleImagePromotionLifecycle(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	ownerInput := platformrepo.ProofPrincipalInput{
		ExternalActorID:     "20000000-0000-4000-8000-000000000001",
		ExternalTenantID:    "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Role image promotion owner", CallerWorkload: "control-api-gateway",
		Operation: "platform.command.role-images.promote",
	}
	owner := resolvedTestPrincipal(t, ctx, repository, ownerInput, "control-api-gateway")
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve role image promotion owner: %v", err)
	}
	platform, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct platform service: %v", err)
	}
	projectResult, err := platform.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "role-image-promotion-project"},
		Payload:  command.ProjectInput{Name: "Role image promotion", Language: "en"},
	})
	if err != nil || projectResult.Project == nil {
		t.Fatalf("create promotion project: project=%#v err=%v", projectResult.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, platform, owner, projectResult.Project.Ref,
		"role-image-promotion-agent", "Role image promotion specialist")
	catalog, recipeInput := promotionComponentCatalog(t)
	created, err := repository.Manage(ctx, roleimagerepo.ManageInput{
		Principal: resolvedOwner, Action: "CREATE", ProjectRef: projectResult.Project.Ref,
		RoleDefinitionRef: agent.RoleDefinitionRef, Name: "Promotion image", Recipe: recipeInput,
		Mutation: roleImageTestMutation("role-image-promotion-create", "CREATE", nil),
	})
	if err != nil || created.Build == nil {
		t.Fatalf("create promotion recipe: result=%#v err=%v", created, err)
	}
	assertManagedRoleImageBuild(t, ctx, repository, created.Recipe, *created.Build)
	testManagedRoleImageDraftFence(t, ctx, repository, owner, resolvedOwner, created.Recipe, *created.Build)
	artifact := seedAdmittedPromotionArtifact(t, ctx, repository, resolvedOwner,
		created.Recipe, *created.Build)
	admittedDetail, err := repository.Get(ctx, resolvedOwner, created.Recipe.Ref)
	if err != nil || admittedDetail.PromotionCandidate == nil ||
		admittedDetail.PromotionCandidate.Ref != artifact.Ref ||
		!containsString(admittedDetail.Recipe.NextActions, "PROMOTE") {
		t.Fatalf("admitted promotion candidate readback mismatch: detail=%#v err=%v", admittedDetail, err)
	}
	roleImages, err := roleimageservice.New(repository, catalog)
	if err != nil {
		t.Fatalf("construct role image service: %v", err)
	}

	promotionWorker := owner
	promotionWorker.CallerWorkload = "image-promotion"
	promotionWorker.Permission = "platform.role-images.promotion.claim"
	promotionWorker.CorrelationRef = "role-image-promotion-unrequested"
	if _, err := roleImages.ClaimPromotion(ctx, promotionWorker, "promotion-before-request"); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("promotion worker claimed an unrequested artifact: %v", err)
	}

	expectedVersion := int64(created.Recipe.Version)
	staleVersion := expectedVersion + 1
	if _, err := roleImages.Promote(ctx, roleimagerepo.PromotionRequestInput{
		Principal: owner,
		Mutation:  value.Mutation{IdempotencyKey: "role-image-promotion-stale", ExpectedVersion: &staleVersion},
		RecipeRef: created.Recipe.Ref, ArtifactRef: artifact.Ref,
		ExpectedProvenanceSHA256: artifact.ProvenanceSHA256,
	}); !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("stale promotion OCC was accepted: %v", err)
	}
	if _, err := roleImages.Promote(ctx, roleimagerepo.PromotionRequestInput{
		Principal: owner,
		Mutation:  value.Mutation{IdempotencyKey: "role-image-promotion-wrong-provenance", ExpectedVersion: &expectedVersion},
		RecipeRef: created.Recipe.Ref, ArtifactRef: artifact.Ref,
		ExpectedProvenanceSHA256: strings.Repeat("0", 64),
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("mismatched admitted provenance was accepted: %v", err)
	}

	request := roleimagerepo.PromotionRequestInput{
		Principal: owner,
		Mutation:  value.Mutation{IdempotencyKey: "role-image-promotion-request", ExpectedVersion: &expectedVersion},
		RecipeRef: created.Recipe.Ref, ArtifactRef: artifact.Ref,
		ExpectedProvenanceSHA256: artifact.ProvenanceSHA256,
	}
	receipt, err := roleImages.Promote(ctx, request)
	if err != nil || receipt.State != "QUEUED" || receipt.Ref == "" || receipt.ReceiptSHA256 == "" ||
		receipt.ImageArtifactRef != artifact.Ref || receipt.ProvenanceSHA256 != artifact.ProvenanceSHA256 {
		t.Fatalf("request exact promotion: receipt=%#v err=%v", receipt, err)
	}
	queuedDetail, err := repository.Get(ctx, resolvedOwner, created.Recipe.Ref)
	if err != nil || queuedDetail.PromotionCandidate == nil ||
		queuedDetail.PromotionCandidate.Ref != artifact.Ref ||
		containsString(queuedDetail.Recipe.NextActions, "PROMOTE") {
		t.Fatalf("queued promotion candidate readback mismatch: detail=%#v err=%v", queuedDetail, err)
	}
	replayed, err := roleImages.Promote(ctx, request)
	if err != nil || !reflect.DeepEqual(replayed, receipt) {
		t.Fatalf("promotion request replay mismatch: replay=%#v receipt=%#v err=%v", replayed, receipt, err)
	}
	reusedKey := request
	reusedKey.ExpectedProvenanceSHA256 = strings.Repeat("1", 64)
	if _, err := roleImages.Promote(ctx, reusedKey); !errors.Is(err, domainerrs.ErrIdempotencyReuse) {
		t.Fatalf("promotion idempotency key reuse was accepted: %v", err)
	}

	promotionWorker.CorrelationRef = "role-image-promotion-claim"
	type claimResult struct {
		claim entity.ImagePromotionClaim
		err   error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			claim, claimErr := roleImages.ClaimPromotion(ctx, promotionWorker, "promotion-worker-claim")
			results <- claimResult{claim: claim, err: claimErr}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	claims := make([]entity.ImagePromotionClaim, 0, 2)
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent idempotent claim failed: %v", result.err)
		}
		claims = append(claims, result.claim)
	}
	claim := claims[0]
	if len(claims) != 2 || !reflect.DeepEqual(claims[0], claims[1]) ||
		claim.Artifact.Ref != artifact.Ref || claim.PromotionClaim == "" {
		t.Fatalf("promotion claim one-winner replay mismatch: claims=%#v", claims)
	}
	if _, err := roleImages.ClaimPromotion(ctx, promotionWorker, "promotion-worker-other-claim"); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("second worker claimed an active promotion: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, `
UPDATE control_plane.image_artifacts
SET promotion_claim_expires_at = clock_timestamp() - interval '1 second'
WHERE ref = $1`, artifact.Ref); err != nil {
		t.Fatalf("expire promotion claim fixture: %v", err)
	}
	retriedClaim, err := roleImages.ClaimPromotion(ctx, promotionWorker, "promotion-worker-retry-claim")
	if err != nil || retriedClaim.Fence <= claim.Fence || retriedClaim.PromotionClaim == claim.PromotionClaim {
		t.Fatalf("expired promotion claim retry mismatch: first=%#v retry=%#v err=%v", claim, retriedClaim, err)
	}
	promotionWorker.Permission = "platform.role-images.promotion.authorize"
	promotionWorker.CorrelationRef = "role-image-promotion-authorize"
	if _, err := roleImages.AuthorizePromotion(ctx, roleimagerepo.PromotionAuthorizeInput{
		Principal: promotionWorker, IdempotencyKey: "promotion-worker-stale-authorize",
		ArtifactRef: artifact.Ref, PromotionClaim: claim.PromotionClaim,
		ManifestDigest: artifact.ManifestDigest, ExpectedVersion: retriedClaim.Artifact.Version,
	}); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("stale promotion claim authorized after retry: %v", err)
	}
	authorization, err := roleImages.AuthorizePromotion(ctx, roleimagerepo.PromotionAuthorizeInput{
		Principal: promotionWorker, IdempotencyKey: "promotion-worker-authorize",
		ArtifactRef: artifact.Ref, PromotionClaim: retriedClaim.PromotionClaim,
		ManifestDigest: artifact.ManifestDigest, ExpectedVersion: retriedClaim.Artifact.Version,
	})
	if err != nil || authorization.AuthorizationToken == "" {
		t.Fatalf("authorize requested promotion: authorization=%#v err=%v", authorization, err)
	}
	promotionWorker.Permission = "platform.role-images.promotion.complete"
	promotionWorker.CorrelationRef = "role-image-promotion-complete"
	promotedReference := repository.roleImages.PromotedRepository + "@" + artifact.ManifestDigest
	completion := roleimagerepo.PromotionCompleteInput{
		Principal: promotionWorker, IdempotencyKey: "promotion-worker-complete",
		ArtifactRef: artifact.Ref, AuthorizationToken: authorization.AuthorizationToken,
		PromotedReference: promotedReference, ManifestDigest: artifact.ManifestDigest,
		PromotionReadbackSHA256: strings.Repeat("9", 64),
		ExpectedVersion:         authorization.Artifact.Version,
	}
	promoted, err := roleImages.CompletePromotion(ctx, completion)
	if err != nil || promoted.PromotedReference != promotedReference {
		t.Fatalf("complete requested promotion: artifact=%#v err=%v", promoted, err)
	}
	replayedPromotion, err := roleImages.CompletePromotion(ctx, completion)
	if err != nil || !reflect.DeepEqual(replayedPromotion, promoted) {
		t.Fatalf("completion replay mismatch: replay=%#v promoted=%#v err=%v", replayedPromotion, promoted, err)
	}
	reusedCompletion := completion
	reusedCompletion.PromotionReadbackSHA256 = strings.Repeat("8", 64)
	if _, err := roleImages.CompletePromotion(ctx, reusedCompletion); !errors.Is(err, domainerrs.ErrIdempotencyReuse) {
		t.Fatalf("completion idempotency key reuse was accepted: %v", err)
	}

	detail, err := repository.Get(ctx, resolvedOwner, created.Recipe.Ref)
	readback, activeArtifact := detail.Recipe, detail.ActiveArtifact
	if err != nil || activeArtifact == nil || detail.PromotionCandidate != nil ||
		readback.ActiveImageArtifactRef != artifact.Ref || readback.PromotedImageReference != promotedReference {
		t.Fatalf("promoted active image readback mismatch: recipe=%#v artifact=%#v err=%v",
			readback, activeArtifact, err)
	}
	var requestState, requestReceipt, revisionRef, revisionReceipt, revisionArtifact string
	var revisionSpec, revisionProvenance, revisionSource, revisionBuild, revisionManifest, revisionPromoted string
	var requestCount, auditCount, promotedEventCount int
	var promotedEventVersion uint64
	if err := repository.pool.QueryRow(ctx, `
SELECT request.state, request.receipt_sha256, revision.ref, revision.promotion_receipt_sha256, artifact.ref,
	   revision.spec_sha256, revision.provenance_sha256, revision.source_sha256,
	   revision.immutable_build_sha256, revision.manifest_digest, revision.promoted_reference,
       count(*) OVER (),
       (SELECT count(*) FROM control_plane.audit_events audit
        WHERE audit.organization_id = request.organization_id
          AND audit.action IN ('controlplane.promote_role_image', 'platform.role-images.promotion.complete')
	      AND audit.resource_ref = $2),
	   (SELECT count(*) FROM control_plane.outbox_events event
	    WHERE convert_from(event.payload, 'UTF8')::jsonb->>'eventName' = 'ROLE_IMAGE_PROMOTED'
	      AND convert_from(event.payload, 'UTF8')::jsonb->>'aggregateRef' = $2),
	   (SELECT max((convert_from(event.payload, 'UTF8')::jsonb->>'aggregateVersion')::bigint)
	    FROM control_plane.outbox_events event
	    WHERE convert_from(event.payload, 'UTF8')::jsonb->>'eventName' = 'ROLE_IMAGE_PROMOTED'
	      AND convert_from(event.payload, 'UTF8')::jsonb->>'aggregateRef' = $2)
FROM control_plane.role_image_promotion_requests request
JOIN control_plane.image_artifacts artifact ON artifact.id = request.image_artifact_id
JOIN control_plane.role_image_recipe_revisions revision ON revision.image_artifact_id = artifact.id
WHERE request.ref = $1 AND request.recipe_id = artifact.recipe_id`, receipt.Ref, created.Recipe.Ref).Scan(
		&requestState, &requestReceipt, &revisionRef, &revisionReceipt, &revisionArtifact,
		&revisionSpec, &revisionProvenance, &revisionSource, &revisionBuild,
		&revisionManifest, &revisionPromoted, &requestCount, &auditCount,
		&promotedEventCount, &promotedEventVersion); err != nil {
		t.Fatalf("read promotion receipt linkage: %v", err)
	}
	if requestState != "PROMOTED" || requestReceipt != receipt.ReceiptSHA256 ||
		revisionReceipt != completion.PromotionReadbackSHA256 ||
		revisionArtifact != artifact.Ref || revisionSpec != artifact.SpecSHA256 ||
		revisionProvenance != artifact.ProvenanceSHA256 || revisionSource != artifact.SourceSHA256 ||
		revisionBuild != artifact.ImmutableBuildSHA256 || revisionManifest != artifact.ManifestDigest ||
		revisionPromoted != promotedReference || requestCount != 1 || auditCount != 2 ||
		promotedEventCount != 1 || promotedEventVersion != readback.Version {
		t.Fatalf("promotion linkage mismatch: state=%s receipt=%s artifact=%s requests=%d audits=%d events=%d eventVersion=%d recipeVersion=%d",
			requestState, revisionReceipt, revisionArtifact, requestCount, auditCount,
			promotedEventCount, promotedEventVersion, readback.Version)
	}
	if _, err := repository.pool.Exec(ctx, `
UPDATE control_plane.role_image_recipe_revisions
SET source_sha256 = $2
WHERE ref = $1`, revisionRef, strings.Repeat("0", 64)); err == nil {
		t.Fatal("immutable promoted role image revision was mutable")
	}
	testRoleImageImpactLifecycle(t, ctx, repository, platform, roleImages, owner, resolvedOwner, readback, *activeArtifact, agent)
}

func promotionComponentCatalog(t *testing.T) (*roleimageservice.Catalog, entity.RoleImageRecipeInput) {
	t.Helper()
	digestA := strings.Repeat("a", 64)
	digestB := strings.Repeat("b", 64)
	catalog, err := roleimageservice.NewCatalog([]roleimageservice.Environment{{
		Key: "promotion", NameMessageKey: "role-environments.promotion.name",
		DescriptionMessageKey: "role-environments.promotion.description",
		Recommended:           true, Available: true,
		Input: entity.RoleImageRecipeInput{
			BaseImageReference: "registry.internal/role-base-promotion",
			BaseImageDigest:    "sha256:" + digestA, SourceRef: "https://source.invalid/kodex",
			SourceRevision: "revision-1", SourceSHA256: digestA,
			ContextRef:    "oci://registry.internal/role-input@sha256:" + digestB,
			ContextSHA256: digestB, BuilderSHA256: digestA, FrontendSHA256: digestA,
			ToolchainSHA256: digestA,
			Platforms:       []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}},
		},
	}})
	if err != nil {
		t.Fatalf("construct promotion catalog: %v", err)
	}
	input, err := catalog.Resolve(entity.RoleEnvironmentSelection{EnvironmentKey: "promotion"})
	if err != nil {
		t.Fatalf("resolve promotion recipe input: %v", err)
	}
	return catalog, input
}

func seedAdmittedPromotionArtifact(t *testing.T, ctx context.Context, repository *Repository,
	principal value.Principal, recipe entity.RoleImageRecipe, build entity.ImageBuild) entity.ImageArtifact {
	t.Helper()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		t.Fatalf("resolve promotion fixture scope: %v", err)
	}
	lockedBuild, err := scanLockedBuild(repository.pool.QueryRow(ctx, queryRoleImagesLockBuild,
		current.organizationID, build.Ref))
	if err != nil {
		t.Fatalf("lock promotion fixture build: %v", err)
	}
	manifestDigest := "sha256:" + strings.Repeat("e", 64)
	provenanceSHA256 := strings.Repeat("f", 64)
	stagingReference := repository.roleImages.StagingRepository + "@" + manifestDigest
	if err := repository.pool.QueryRow(ctx, queryRoleImagesCompleteBuild, current.organizationID,
		lockedBuild.ID, lockedBuild.Build.Version, stagingReference, manifestDigest,
		provenanceSHA256, lockedBuild.Build.ImmutableBuildSHA256).Scan(
		&lockedBuild.Build.Version, &lockedBuild.Build.Stage, &lockedBuild.Build.ProgressPercent,
		&lockedBuild.Build.StagingReference, &lockedBuild.Build.ManifestDigest,
		&lockedBuild.Build.ProvenanceSHA256, &lockedBuild.Build.ImmutableBuildSHA256,
		&lockedBuild.Build.UpdatedAt); err != nil {
		t.Fatalf("complete promotion fixture build: %v", err)
	}
	artifactRef, err := newRef("imgart")
	if err != nil {
		t.Fatalf("create promotion fixture artifact ref: %v", err)
	}
	var artifactID string
	if err := repository.pool.QueryRow(ctx, queryRoleImagesInsertArtifact, artifactRef,
		current.organizationID, lockedBuild.ProjectID, lockedBuild.RecipeID,
		lockedBuild.Build.RecipeVersion, lockedBuild.Build.RecipeGeneration,
		lockedBuild.Build.SpecSHA256, lockedBuild.ID, lockedBuild.Build.Version,
		lockedBuild.Build.Attempt, asJSON(lockedBuild.Specification), lockedBuild.PolicyRevision,
		lockedBuild.PolicySHA256, lockedBuild.ContractRevision, lockedBuild.ContractSHA256,
		stagingReference, manifestDigest, lockedBuild.Build.ImmutableBuildSHA256,
		provenanceSHA256).Scan(&artifactID); err != nil {
		t.Fatalf("insert promotion fixture artifact: %v", err)
	}
	var version, admissionRevision uint64
	var verdict string
	var updatedAt time.Time
	if err := repository.pool.QueryRow(ctx, queryRoleImagesRecordAdmission,
		current.organizationID, artifactID, uint64(1), "ACCEPTED",
		strings.Repeat("2", 64), strings.Repeat("3", 64),
		"spiffe://kodex.local/ns/kodex-system/sa/image-admission",
		strings.Repeat("4", 64), strings.Repeat("5", 64),
		"sha256:"+strings.Repeat("6", 64)).Scan(
		&version, &verdict, &admissionRevision, &updatedAt); err != nil {
		t.Fatalf("admit promotion fixture artifact: %v", err)
	}
	artifact, err := scanRoleImageArtifact(repository.pool.QueryRow(ctx,
		queryRoleImagesGetActiveArtifact, current.organizationID, artifactRef))
	if err != nil {
		t.Fatalf("read admitted promotion fixture: %v", err)
	}
	if artifact.RecipeRef != recipe.Ref || artifact.Version != version || verdict != "ACCEPTED" || admissionRevision == 0 {
		t.Fatalf("invalid admitted promotion fixture: artifact=%#v verdict=%s revision=%d", artifact, verdict, admissionRevision)
	}
	return artifact
}
