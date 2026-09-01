package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testRoleImageAdmissionPolicyRotation(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	originalConfig := repository.roleImages
	defer func() {
		if err := repository.ConfigureRoleImages(originalConfig); err != nil {
			t.Errorf("restore role image configuration: %v", err)
		}
	}()

	ownerInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Role image admission owner", CallerWorkload: "control-api-gateway",
		Operation: "platform.role-images.recipes.manage",
	}
	owner := resolvedTestPrincipal(t, ctx, repository, ownerInput, "control-api-gateway")
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve role image admission owner: %v", err)
	}
	ownerScope, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatalf("resolve role image admission owner scope: %v", err)
	}
	platform, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct role image admission platform service: %v", err)
	}
	projectResult, err := platform.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "role-image-admission-policy-project"},
		Payload:  command.ProjectInput{Name: "Role image admission policy rotation", Language: "en"},
	})
	if err != nil || projectResult.Project == nil {
		t.Fatalf("create role image admission project: project=%#v err=%v", projectResult.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, platform, owner, projectResult.Project.Ref,
		"role-image-admission-policy-agent", "Role image admission policy specialist")
	_, recipeInput := promotionComponentCatalog(t)

	seedArtifact := func(key, name, digestCharacter string) entity.ImageArtifact {
		t.Helper()
		created, createErr := repository.Manage(ctx, roleimagerepo.ManageInput{
			Principal: resolvedOwner, Action: "CREATE", ProjectRef: projectResult.Project.Ref,
			RoleDefinitionRef: agent.RoleDefinitionRef, Name: name, Recipe: recipeInput,
			Mutation: roleImageTestMutation(key+"-recipe", "CREATE", nil),
		})
		if createErr != nil || created.Build == nil {
			t.Fatalf("create %s role image recipe: result=%#v err=%v", key, created, createErr)
		}
		lockedBuild, lockErr := scanLockedBuild(repository.pool.QueryRow(ctx, queryRoleImagesLockBuild,
			ownerScope.organizationID, created.Build.Ref))
		if lockErr != nil {
			t.Fatalf("lock %s role image build: %v", key, lockErr)
		}
		manifestDigest := "sha256:" + strings.Repeat(digestCharacter, 64)
		provenanceSHA256 := strings.Repeat(digestCharacter, 64)
		stagingReference := repository.roleImages.StagingRepository + "@" + manifestDigest
		if err := repository.pool.QueryRow(ctx, queryRoleImagesCompleteBuild,
			ownerScope.organizationID, lockedBuild.ID, lockedBuild.Build.Version,
			stagingReference, manifestDigest, provenanceSHA256,
			lockedBuild.Build.ImmutableBuildSHA256).Scan(
			&lockedBuild.Build.Version, &lockedBuild.Build.Stage, &lockedBuild.Build.ProgressPercent,
			&lockedBuild.Build.StagingReference, &lockedBuild.Build.ManifestDigest,
			&lockedBuild.Build.ProvenanceSHA256, &lockedBuild.Build.ImmutableBuildSHA256,
			&lockedBuild.Build.UpdatedAt); err != nil {
			t.Fatalf("complete %s role image build: %v", key, err)
		}
		artifactRef, refErr := newRef("imgart")
		if refErr != nil {
			t.Fatalf("create %s role image artifact ref: %v", key, refErr)
		}
		var artifactID string
		if err := repository.pool.QueryRow(ctx, queryRoleImagesInsertArtifact, artifactRef,
			ownerScope.organizationID, lockedBuild.ProjectID, lockedBuild.RecipeID,
			lockedBuild.Build.RecipeVersion, lockedBuild.Build.RecipeGeneration,
			lockedBuild.Build.SpecSHA256, lockedBuild.ID, lockedBuild.Build.Version,
			lockedBuild.Build.Attempt, asJSON(lockedBuild.Specification), lockedBuild.PolicyRevision,
			lockedBuild.PolicySHA256, lockedBuild.ContractRevision, lockedBuild.ContractSHA256,
			stagingReference, manifestDigest, lockedBuild.Build.ImmutableBuildSHA256,
			provenanceSHA256).Scan(&artifactID); err != nil {
			t.Fatalf("insert %s role image artifact: %v", key, err)
		}
		artifact, readErr := scanRoleImageArtifact(repository.pool.QueryRow(ctx,
			queryRoleImagesGetActiveArtifact, ownerScope.organizationID, artifactRef))
		if readErr != nil {
			t.Fatalf("read %s role image artifact: %v", key, readErr)
		}
		return artifact
	}

	expiredArtifact := seedArtifact("role-image-admission-expired", "Expired policy artifact", "7")
	worker := resolvedOwner
	worker.CallerWorkload = "image-admission"
	worker.Permission = "platform.role-images.admission.claim"
	worker.CorrelationRef = "role-image-admission-expired-claim"
	expiredClaim, err := repository.ClaimAdmission(ctx, worker, "role-image-admission-expired-claim")
	if err != nil || expiredClaim.Artifact.Ref != expiredArtifact.Ref || expiredClaim.ClaimToken == "" {
		t.Fatalf("claim old-policy artifact: claim=%#v err=%v", expiredClaim, err)
	}
	if _, err := repository.pool.Exec(ctx, `
		UPDATE control_plane.image_artifacts
		SET admission_claim_expires_at = clock_timestamp() - interval '1 second'
		WHERE ref = $1`, expiredArtifact.Ref); err != nil {
		t.Fatalf("expire old-policy admission claim: %v", err)
	}
	pendingArtifact := seedArtifact("role-image-admission-pending", "Pending policy artifact", "8")

	currentConfig := originalConfig
	currentConfig.PolicyRevision++
	currentConfig.PolicySHA256 = strings.Repeat("e", 64)
	if err := repository.ConfigureRoleImages(currentConfig); err != nil {
		t.Fatalf("rotate role image admission policy: %v", err)
	}

	if _, err := repository.ClaimAdmission(ctx, worker, "role-image-admission-empty-current-policy"); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("empty current-policy queue did not close stale artifacts: %v", err)
	}
	if _, err := repository.ClaimAdmission(ctx, worker, "role-image-admission-expired-claim"); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("stale idempotency replay returned an old-policy claim: %v", err)
	}
	currentArtifact := seedArtifact("role-image-admission-current", "Current policy artifact", "9")
	worker.CorrelationRef = "role-image-admission-current-claim"
	currentClaim, err := repository.ClaimAdmission(ctx, worker, "role-image-admission-current-claim")
	if err != nil || currentClaim.Artifact.Ref != currentArtifact.Ref ||
		currentClaim.Artifact.PolicyRevision != currentConfig.PolicyRevision ||
		currentClaim.Artifact.PolicySHA256 != currentConfig.PolicySHA256 {
		t.Fatalf("current policy claim mismatch: claim=%#v err=%v", currentClaim, err)
	}

	for _, artifact := range []entity.ImageArtifact{expiredArtifact, pendingArtifact} {
		var admissionState, admissionVerdict, promotionState string
		var admissionCredentialsRevoked, promotionCredentialsRevoked bool
		if err := repository.pool.QueryRow(ctx, `
			SELECT admission_state, admission_verdict, promotion_state,
			       admission_claimant_workload IS NULL
			         AND admission_authority_generation = 0
			         AND admission_claim_token_sha256 IS NULL
			         AND admission_claim_expires_at IS NULL,
			       promotion_claimant_workload IS NULL
			         AND promotion_authority_generation = 0
			         AND promotion_claim_token_sha256 IS NULL
			         AND promotion_claim_expires_at IS NULL
			         AND promotion_authorization_token_sha256 IS NULL
			         AND promotion_authorization_expires_at IS NULL
			FROM control_plane.image_artifacts
			WHERE ref = $1`, artifact.Ref).Scan(
			&admissionState, &admissionVerdict, &promotionState,
			&admissionCredentialsRevoked, &promotionCredentialsRevoked); err != nil {
			t.Fatalf("read terminalized stale artifact %s: %v", artifact.Ref, err)
		}
		if admissionState != "REJECTED" || admissionVerdict != "" || promotionState != "REJECTED" ||
			!admissionCredentialsRevoked || !promotionCredentialsRevoked {
			t.Fatalf("stale artifact was not closed without a synthetic verdict: ref=%s admission=%s verdict=%s promotion=%s admissionRevoked=%t promotionRevoked=%t",
				artifact.Ref, admissionState, admissionVerdict, promotionState,
				admissionCredentialsRevoked, promotionCredentialsRevoked)
		}
	}
}
