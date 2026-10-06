package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
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
	liveArtifact := seedArtifact("role-image-admission-live", "Live policy claim artifact", "6")
	liveClaim, err := repository.ClaimAdmission(ctx, worker, "role-image-admission-live-claim")
	if err != nil || liveClaim.Artifact.Ref != liveArtifact.Ref || liveClaim.Artifact.AdmissionAttempt == nil {
		t.Fatalf("claim live old-policy artifact: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, `
		UPDATE control_plane.image_artifacts
		SET admission_claim_expires_at = clock_timestamp() - interval '1 second'
		WHERE ref = $1`, expiredArtifact.Ref); err != nil {
		t.Fatalf("expire old-policy admission claim: %v", err)
	}
	pendingArtifact := seedArtifact("role-image-admission-pending", "Pending policy artifact", "8")

	currentConfig := originalConfig
	// Local render сохраняет policy revision и меняет SHA при новом worker OCI.
	currentConfig.PolicySHA256 = strings.Repeat("e", 64)
	if err := repository.ConfigureRoleImages(currentConfig); err != nil {
		t.Fatalf("rotate role image admission policy: %v", err)
	}
	availability, err := repository.GetSupplyWorkAvailability(ctx, worker)
	if err != nil || !availability.AdmissionAvailable {
		t.Fatalf("stale-only admission maintenance is unreachable: available=%t err=%v", availability.AdmissionAvailable, err)
	}
	type maintenanceSnapshot struct {
		artifacts, attempts      string
		audits, receipts, events int
	}
	readSnapshot := func() maintenanceSnapshot {
		t.Helper()
		var got maintenanceSnapshot
		err := repository.pool.QueryRow(ctx, `SELECT
		 md5(COALESCE(jsonb_agg(to_jsonb(artifact) ORDER BY artifact.ref)::text,'[]')),
		 (SELECT md5(COALESCE(jsonb_agg(to_jsonb(attempt) ORDER BY attempt.ref)::text,'[]')) FROM control_plane.image_admission_attempts attempt WHERE attempt.artifact_id IN (SELECT id FROM control_plane.image_artifacts WHERE ref IN ($1,$2,$3))),
		 (SELECT count(*) FROM control_plane.audit_events),
		 (SELECT count(*) FROM control_plane.idempotency_receipts),
		 (SELECT count(*) FROM control_plane.outbox_events)
		 FROM control_plane.image_artifacts artifact WHERE artifact.ref IN ($1,$2,$3)`, expiredArtifact.Ref, pendingArtifact.Ref, liveArtifact.Ref).Scan(&got.artifacts, &got.attempts, &got.audits, &got.receipts, &got.events)
		if err != nil {
			t.Fatal("read maintenance snapshot")
		}
		return got
	}
	// Отдельный synthetic tenant существует в disposable owner DB;
	// отрицательный путь проходит настоящий resolveScope, не fake org locator.
	if _, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.organizations(ref,name)
	 VALUES ('org_maintenance_foreign','Foreign maintenance fixture')`); err != nil {
		t.Fatal("create foreign maintenance tenant fixture")
	}
	if _, err := repository.pool.Exec(ctx, `INSERT INTO control_plane.subjects(organization_id,ref,issuer,external_subject_digest,display_name)
	 SELECT id,'usr_maintenance_foreign','fixture',repeat('f',64),'Foreign maintenance actor'
	 FROM control_plane.organizations WHERE ref='org_maintenance_foreign'`); err != nil {
		t.Fatal("create foreign maintenance actor fixture")
	}
	foreign := worker
	foreign.ActorID, foreign.AuthorityTenant = "usr_maintenance_foreign", "org_maintenance_foreign"
	if _, err := repository.resolveScope(ctx, foreign); err != nil {
		t.Fatal("resolve active foreign maintenance fixture")
	}
	foreignAvailability, err := repository.GetSupplyWorkAvailability(ctx, foreign)
	if err != nil || foreignAvailability.AdmissionAvailable {
		t.Fatal("foreign tenant sees stale maintenance")
	}
	// Создание другой identity не должно смешиваться с проверкой claim effects.
	before := readSnapshot()
	if _, err := repository.ClaimAdmission(ctx, foreign, "foreign-stale-maintenance"); !errors.Is(err, domainerrs.ErrNotFound) || readSnapshot() != before {
		t.Fatal("foreign tenant changed stale owner state")
	}
	// Отказ caller до receipt/commit откатывает artifact, attempt и audit вместе.
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin maintenance rollback fixture")
	}
	rejected, err := repository.rejectStaleRoleImageAdmissions(ctx, tx, ownerScope, pgx.StrictNamedArgs{
		"organization_id": ownerScope.organizationID, "policy_revision": currentConfig.PolicyRevision, "policy_sha256": currentConfig.PolicySHA256,
	})
	rollbackErr := tx.Rollback(ctx)
	if err != nil || rollbackErr != nil || len(rejected) != 3 || readSnapshot() != before {
		t.Fatal("maintenance rollback left a partial terminal graph or audit")
	}

	if _, err := repository.ClaimAdmission(ctx, worker, "role-image-admission-empty-current-policy"); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("empty current-policy queue did not close stale artifacts: %v", err)
	}
	after := readSnapshot()
	if after.audits != before.audits+3 || after.receipts != before.receipts+1 || after.events != before.events {
		t.Fatal("stale maintenance lost atomic audits/receipt or created an undocumented event")
	}
	var responseType string
	var rawReceipt []byte
	if err := repository.pool.QueryRow(ctx, `SELECT response_type,response_payload FROM control_plane.idempotency_receipts
	 WHERE organization_id=$1::uuid AND actor_id=$2::uuid AND operation='platform.role-images.admission.claim'
	 AND idempotency_key='role-image-admission-empty-current-policy'`, ownerScope.organizationID, ownerScope.actorID).Scan(&responseType, &rawReceipt); err != nil {
		t.Fatal("read stale-only maintenance receipt")
	}
	var receipt admissionClaimReceipt
	if err := json.Unmarshal(rawReceipt, &receipt); err != nil || responseType != "IMAGE_ADMISSION_MAINTENANCE_OUTCOME" ||
		len(receipt.Rejected) != 3 || len(receipt.Expired) != 0 || receipt.Artifact.Ref != "" || receipt.AdmissionAttemptRef != "" || receipt.ClaimExpiresAt != (time.Time{}) {
		t.Fatal("stale-only maintenance did not pin a closed no-claim receipt")
	}
	expected := map[string]entity.ImageArtifact{expiredArtifact.Ref: expiredArtifact, pendingArtifact.Ref: pendingArtifact, liveArtifact.Ref: liveArtifact}
	for _, actual := range receipt.Rejected {
		original, known := expected[actual.Ref]
		if !known || actual.BuildRef != original.BuildRef || actual.BuildAttempt != original.BuildAttempt ||
			actual.RecipeVersion != original.RecipeVersion || actual.RecipeGeneration != original.RecipeGeneration ||
			actual.PolicyRevision != original.PolicyRevision || actual.PolicySHA256 != original.PolicySHA256 ||
			actual.SpecSHA256 != original.SpecSHA256 || actual.RoleRuntimeContractRevision != original.RoleRuntimeContractRevision || actual.RoleRuntimeContractSHA256 != original.RoleRuntimeContractSHA256 ||
			actual.ManifestDigest != original.ManifestDigest || actual.ImmutableBuildSHA256 != original.ImmutableBuildSHA256 ||
			actual.ProvenanceSHA256 != original.ProvenanceSHA256 || actual.AdmissionVerdict != "" || actual.PromotionState != "REJECTED" {
			t.Fatal("stale maintenance receipt lost immutable artifact pins")
		}
		if actual.Ref == liveArtifact.Ref && (actual.AdmissionAttempt == nil || actual.AdmissionAttempt.Ref != liveClaim.AdmissionAttemptRef || actual.AdmissionAttempt.State != "CANCELLED") {
			t.Fatal("stale maintenance receipt lost the original closed attempt")
		}
		delete(expected, actual.Ref)
	}
	if len(expected) != 0 {
		t.Fatal("stale maintenance receipt duplicated or omitted a terminal artifact")
	}
	if _, err := repository.ClaimAdmission(ctx, worker, "role-image-admission-empty-current-policy"); !errors.Is(err, domainerrs.ErrNotFound) || readSnapshot() != after {
		t.Fatal("stale maintenance replay repeated effects")
	}
	if _, err := repository.ClaimAdmission(ctx, worker, "role-image-admission-expired-claim"); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("stale idempotency replay returned an old-policy claim: %v", err)
	}
	if readSnapshot() != after {
		t.Fatal("old claim replay changed terminal maintenance state")
	}
	availability, err = repository.GetSupplyWorkAvailability(ctx, worker)
	if err != nil || availability.AdmissionAvailable {
		t.Fatal("terminal stale artifacts still advertise maintenance")
	}
	if _, err := repository.ClaimAdmission(ctx, worker, "role-image-admission-idle-poll"); !errors.Is(err, domainerrs.ErrNotFound) || readSnapshot() != after {
		t.Fatal("idle claim created a receipt or changed terminal state")
	}
	var attemptState string
	var attemptVersion uint64
	var pinnedTerminal bool
	if err := repository.pool.QueryRow(ctx, `SELECT state,version,finished_at IS NOT NULL AND
	 terminal_artifact_json->>'admission_state'='REJECTED' AND terminal_artifact_json->>'admission_verdict'=''
	 FROM control_plane.image_admission_attempts WHERE ref=$1`, liveClaim.Artifact.AdmissionAttempt.Ref).Scan(&attemptState, &attemptVersion, &pinnedTerminal); err != nil ||
		attemptState != "CANCELLED" || attemptVersion != liveClaim.Artifact.AdmissionAttempt.Version+1 || !pinnedTerminal {
		t.Fatalf("live stale claim attempt was not atomically closed: %v", err)
	}
	for _, artifact := range []entity.ImageArtifact{expiredArtifact, pendingArtifact, liveArtifact} {
		detail, err := repository.Get(ctx, resolvedOwner, artifact.RecipeRef)
		if err != nil || detail.PromotionCandidate == nil || detail.PromotionCandidate.Ref != artifact.Ref ||
			detail.PromotionCandidate.AdmissionVerdict != "REJECTED" || detail.AdmissionFailure != nil || detail.PromotionCandidate.PolicySHA256 != originalConfig.PolicySHA256 {
			t.Fatalf("stale terminal is not visible through existing owner GET: %v", err)
		}
		for _, action := range detail.Recipe.NextActions {
			if action == "PROMOTE" {
				t.Fatal("stale terminal is promotable")
			}
		}
	}
	currentArtifact := seedArtifact("role-image-admission-current", "Current policy artifact", "9")
	availability, err = repository.GetSupplyWorkAvailability(ctx, worker)
	if err != nil || !availability.AdmissionAvailable {
		t.Fatal("current-policy claim became unreachable")
	}
	worker.CorrelationRef = "role-image-admission-current-claim"
	currentClaim, err := repository.ClaimAdmission(ctx, worker, "role-image-admission-current-claim")
	if err != nil || currentClaim.Artifact.Ref != currentArtifact.Ref ||
		currentClaim.Artifact.PolicyRevision != currentConfig.PolicyRevision ||
		currentClaim.Artifact.PolicySHA256 != currentConfig.PolicySHA256 {
		t.Fatalf("current policy claim mismatch: claim=%#v err=%v", currentClaim, err)
	}

	for _, artifact := range []entity.ImageArtifact{expiredArtifact, pendingArtifact, liveArtifact} {
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
	detail, err := repository.Get(ctx, resolvedOwner, pendingArtifact.RecipeRef)
	if err != nil {
		t.Fatal(err)
	}
	version := int64(detail.Recipe.Version)
	requested, err := repository.Manage(ctx, roleimagerepo.ManageInput{Principal: resolvedOwner, Action: "REQUEST_BUILD",
		ProjectRef: projectResult.Project.Ref, RecipeRef: pendingArtifact.RecipeRef,
		Mutation: roleImageTestMutation("role-image-admission-policy-rebuild", "REQUEST_BUILD", &version)})
	if err != nil || requested.Build == nil || requested.Build.Ref == pendingArtifact.BuildRef ||
		requested.Recipe.PolicySHA256 != currentConfig.PolicySHA256 || requested.Recipe.Version != detail.Recipe.Version+1 {
		t.Fatalf("native rebuild did not create a fresh policy-pinned build: %v", err)
	}
	detail, err = repository.Get(ctx, resolvedOwner, pendingArtifact.RecipeRef)
	if err != nil || detail.PromotionCandidate != nil || len(detail.Builds) == 0 || detail.Builds[0].Ref != requested.Build.Ref {
		t.Fatal("rebuild exposed stale terminal artifact as the new build candidate")
	}
	// Следующий сценарий использует прежнюю policy: закрываем оставленный claim
	// штатной stale maintenance, а не оставляем ему глобальную очередь fixture.
	if err := repository.ConfigureRoleImages(originalConfig); err != nil {
		t.Fatal("restore role image policy before fixture cleanup")
	}
	if _, err := repository.ClaimAdmission(ctx, worker, "role-image-admission-policy-cleanup"); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatal("stale current-policy fixture claim did not close through maintenance")
	}
	var currentClosed bool
	if err := repository.pool.QueryRow(ctx, `SELECT
		attempt.state='CANCELLED' AND attempt.finished_at IS NOT NULL
		AND artifact.admission_state='REJECTED' AND artifact.admission_verdict=''
		AND artifact.admission_claimant_workload IS NULL
		AND artifact.admission_authority_generation=0
		AND artifact.admission_claim_token_sha256 IS NULL
		AND artifact.admission_claim_expires_at IS NULL
		FROM control_plane.image_admission_attempts attempt
		JOIN control_plane.image_artifacts artifact ON artifact.id=attempt.artifact_id
		WHERE attempt.ref=$1`, currentClaim.AdmissionAttemptRef).Scan(&currentClosed); err != nil || !currentClosed {
		t.Fatalf("fixture cleanup retained an active admission attempt or authority: closed=%t err=%v", currentClosed, err)
	}
	availability, err = repository.GetSupplyWorkAvailability(ctx, worker)
	if err != nil || availability.AdmissionAvailable {
		t.Fatal("fixture cleanup left stale admission maintenance available")
	}
}
