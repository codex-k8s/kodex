package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// queryImageAdmissionFailureExpiry expires only the claimed artifact selected by exact
// organization/ref for owner-side failure recovery tests.
// queryImageAdmissionFailureExpiry делает просроченным только захваченный артефакт точной
// организации и ref для тестов восстановления ошибки владельцем.
//
//go:embed testdata/sql/image_admission_failure_expiry.sql
var queryImageAdmissionFailureExpiry string

// queryImageAdmissionFailureReadback reads the terminal artifact and latest admission attempt,
// exact terminal snapshot and absence of open attempts or retained claim evidence.
// queryImageAdmissionFailureReadback читает окончательный артефакт и последнюю попытку допуска,
// точный окончательный снимок и отсутствие открытых попыток и сохранённых данных claim.
//
//go:embed testdata/sql/image_admission_failure_readback.sql
var queryImageAdmissionFailureReadback string

// queryImageAdmissionFailureEventCount counts exact organization/recipe/state outbox facts to
// enforce one terminal admission event.
// queryImageAdmissionFailureEventCount считает точные факты outbox организации, рецепта и
// состояния для требования одного окончательного события допуска.
//
//go:embed testdata/sql/image_admission_failure_event_count.sql
var queryImageAdmissionFailureEventCount string

// TestImageAdmissionFailureComponent checks fenced/OCC technical failure, exact fresh-proof
// replay, worker expiry and owner/hook recovery, superseded artifact denial and one-winner
// record/failure races without fabricated admission evidence; all builds and claims are synthetic
// disposable fixtures.
// TestImageAdmissionFailureComponent проверяет fencing и OCC технического отказа, точный повтор со
// свежим proof, истечение worker и восстановление владельцем и hook, запрет вытесненного артефакта
// и единственного победителя гонки записи и отказа без выдуманного доказательства допуска; все
// сборки и claims искусственные в одноразовой fixture.
func TestImageAdmissionFailureComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	var err error
	defer pool.Close()
	repository := newNamespacedRoleImageComponentRepository(t, ctx, pool)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.organization.role-images.recipes.manage"}, "control-api-gateway")
	owner, err = repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	worker := owner
	worker.CallerWorkload = "image-admission"
	worker.Permission = "platform.role-images.admission.claim"
	worker.CredentialRevision = 1
	create := func(key string) roleimagerepo.ManageResult {
		t.Helper()
		_, recipe := promotionComponentCatalog(t)
		result, err := repository.ManageOrganization(ctx, roleimagerepo.ManageInput{Principal: owner, Action: "CREATE", Name: "Failure fixture " + key, Recipe: recipe, Mutation: roleImageTestMutation("failure-create-"+key, "CREATE", nil)})
		if err != nil {
			t.Fatal(err)
		}
		seedPromotionArtifact(t, ctx, repository, owner, result.Recipe, *result.Build, "PENDING")
		return result
	}
	inputFor := func(claim entity.ImageAdmissionClaim, key string) roleimagerepo.AdmissionFailureInput {
		a := claim.Artifact
		return roleimagerepo.AdmissionFailureInput{ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt, Principal: worker, IdempotencyKey: key, ArtifactRef: a.Ref, ExpectedVersion: a.Version, ExpectedFence: claim.Fence, ExpectedAuthorityGeneration: claim.AuthorityGeneration, ClaimToken: claim.ClaimToken, ManifestDigest: a.ManifestDigest, ImmutableBuildSHA256: a.ImmutableBuildSHA256, ProvenanceSHA256: a.ProvenanceSHA256, PolicyRevision: a.PolicyRevision, PolicySHA256: a.PolicySHA256, BuildRef: a.BuildRef, ExpectedBuildAttempt: a.BuildAttempt, RecipeGeneration: a.RecipeGeneration, SpecSHA256: a.SpecSHA256, ErrorCode: "ADMISSION_WORKER_FAILED"}
	}
	assertFailure := func(claim entity.ImageAdmissionClaim, expected string) {
		t.Helper()
		scope, err := repository.resolveScope(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		var state, verdict, code, sbom string
		var tokenGone, leaseGone bool
		var generation, revision uint64
		var attemptRef, attemptState string
		var attemptNumber uint32
		var attemptFence uint64
		var exactTerminalSnapshot, noOpenAttempt bool
		if err := pool.QueryRow(ctx, queryImageAdmissionFailureReadback, scope.organizationID, claim.Artifact.Ref).Scan(&state, &verdict, &code, &tokenGone, &leaseGone, &generation, &sbom, &revision,
			&attemptRef, &attemptNumber, &attemptState, &attemptFence, &exactTerminalSnapshot, &noOpenAttempt); err != nil {
			t.Fatal(err)
		}
		if state != "FAILED" || verdict != "" || code != expected || !tokenGone || !leaseGone || generation != 0 || sbom != "" || revision != 0 {
			t.Fatal("technical failure fabricated evidence or retained a claim")
		}
		if attemptRef != claim.AdmissionAttemptRef || attemptNumber != claim.AdmissionAttempt || attemptState != "FAILED" ||
			attemptFence != claim.Fence || !exactTerminalSnapshot || !noOpenAttempt {
			t.Fatal("technical failure did not atomically close the exact admission attempt")
		}
		var count int
		if err := pool.QueryRow(ctx, queryImageAdmissionFailureEventCount, claim.Artifact.RecipeRef, claim.Artifact.OrganizationRef, expected).Scan(&count); err != nil || count != 1 {
			t.Fatalf("terminal event cardinality: count=%d err=%v", count, err)
		}
	}
	created := create("worker")
	claim, err := repository.ClaimAdmission(ctx, worker, "failure-claim-worker")
	if err != nil {
		t.Fatal(err)
	}
	input := inputFor(claim, "failure-worker")
	bad := input
	bad.ExpectedFence++
	if _, err := repository.FailAdmission(ctx, bad); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("wrong fence accepted")
	}
	bad = input
	bad.ExpectedVersion++
	if _, err := repository.FailAdmission(ctx, bad); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("wrong version accepted")
	}
	failure, err := repository.FailAdmission(ctx, input)
	if err != nil || failure.Version != claim.Artifact.Version+1 || failure.ErrorCode != "ADMISSION_WORKER_FAILED" {
		t.Fatalf("worker failure: %v", err)
	}
	replay, err := repository.FailAdmission(ctx, input)
	if err != nil || replay != failure {
		t.Fatalf("exact failure replay: %v", err)
	}
	fresh := input
	fresh.Principal.CorrelationRef = "cor_freshfailure"
	fresh.Principal.CredentialRevision++
	if replay, err := repository.FailAdmission(ctx, fresh); err != nil || replay != failure {
		t.Fatalf("fresh proof failure replay: %v", err)
	}
	if _, err := repository.ClaimAdmission(ctx, worker, "failure-claim-worker"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("terminal claim replay returned revoked token")
	}
	bad = input
	bad.ErrorCode = "ADMISSION_EVIDENCE_EXCEEDS_BOUND"
	if _, err := repository.FailAdmission(ctx, bad); err == nil {
		t.Fatal("changed intent replay accepted")
	}
	assertFailure(claim, "ADMISSION_WORKER_FAILED")
	detail, err := repository.GetOrganization(ctx, owner, created.Recipe.Ref)
	if err != nil || detail.AdmissionFailure == nil || *detail.AdmissionFailure != failure || detail.PromotionCandidate != nil {
		t.Fatalf("owner technical failure read: %v", err)
	}
	if _, err := repository.ClaimAdmission(ctx, worker, "failure-no-reclaim"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("failed artifact reclaimed: %v", err)
	}
	created = create("expired")
	claim, err = repository.ClaimAdmission(ctx, worker, "failure-claim-expired")
	if err != nil {
		t.Fatal(err)
	}
	input = inputFor(claim, "failure-expired-worker")
	scope, err := repository.resolveScope(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, queryImageAdmissionFailureExpiry, scope.organizationID, claim.Artifact.Ref); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.FailAdmission(ctx, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("expired worker token accepted: %v", err)
	}
	expiry := roleimagerepo.AdmissionExpiryInput{ExpectedAdmissionAttemptRef: input.ExpectedAdmissionAttemptRef, ExpectedAdmissionAttempt: input.ExpectedAdmissionAttempt, Principal: worker, IdempotencyKey: "failure-expired-owner", ArtifactRef: input.ArtifactRef, ExpectedVersion: input.ExpectedVersion, ExpectedFence: input.ExpectedFence, ExpectedAuthorityGeneration: input.ExpectedAuthorityGeneration, ManifestDigest: input.ManifestDigest, ImmutableBuildSHA256: input.ImmutableBuildSHA256, ProvenanceSHA256: input.ProvenanceSHA256, PolicyRevision: input.PolicyRevision, PolicySHA256: input.PolicySHA256, BuildRef: input.BuildRef, ExpectedBuildAttempt: input.ExpectedBuildAttempt, RecipeGeneration: input.RecipeGeneration, SpecSHA256: input.SpecSHA256}
	failure, err = repository.ExpireAdmission(ctx, expiry)
	if err != nil || failure.ErrorCode != "ADMISSION_LEASE_EXPIRED" {
		t.Fatalf("owner expiry: %v", err)
	}
	replay, err = repository.ExpireAdmission(ctx, expiry)
	if err != nil || replay != failure {
		t.Fatalf("owner expiry replay: %v", err)
	}
	assertFailure(claim, "ADMISSION_LEASE_EXPIRED")
	create("hook")
	claim, err = repository.ClaimAdmission(ctx, worker, "failure-claim-hook")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, queryImageAdmissionFailureExpiry, scope.organizationID, claim.Artifact.Ref); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := repository.ClaimAdmission(ctx, worker, "failure-expiry-hook"); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("expiry hook: %v", err)
		}
	}
	assertFailure(claim, "ADMISSION_LEASE_EXPIRED")
	availability, err := repository.GetSupplyWorkAvailability(ctx, worker)
	if err != nil || availability.AdmissionAvailable {
		t.Fatalf("terminal availability: %v", err)
	}
	hookInput := inputFor(claim, "hook-recovery")
	expiry = roleimagerepo.AdmissionExpiryInput{ExpectedAdmissionAttemptRef: hookInput.ExpectedAdmissionAttemptRef, ExpectedAdmissionAttempt: hookInput.ExpectedAdmissionAttempt, Principal: worker, IdempotencyKey: "hook-recovery-expire", ArtifactRef: hookInput.ArtifactRef, ExpectedVersion: hookInput.ExpectedVersion, ExpectedFence: hookInput.ExpectedFence, ExpectedAuthorityGeneration: hookInput.ExpectedAuthorityGeneration, ManifestDigest: hookInput.ManifestDigest, ImmutableBuildSHA256: hookInput.ImmutableBuildSHA256, ProvenanceSHA256: hookInput.ProvenanceSHA256, PolicyRevision: hookInput.PolicyRevision, PolicySHA256: hookInput.PolicySHA256, BuildRef: hookInput.BuildRef, ExpectedBuildAttempt: hookInput.ExpectedBuildAttempt, RecipeGeneration: hookInput.RecipeGeneration, SpecSHA256: hookInput.SpecSHA256}
	if receipt, err := repository.ExpireAdmission(ctx, expiry); err != nil || receipt.ErrorCode != "ADMISSION_LEASE_EXPIRED" {
		t.Fatalf("hook expiry exact receipt recovery: %v", err)
	}
	expiry.ExpectedFence++
	expiry.IdempotencyKey = "hook-recovery-foreign"
	if _, err := repository.ExpireAdmission(ctx, expiry); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("stale expiry tuple returned another claim receipt")
	}
	assertFailure(claim, "ADMISSION_LEASE_EXPIRED")
	superseded := create("superseded")
	version := int64(superseded.Recipe.Version)
	replacement, err := repository.ManageOrganization(ctx, roleimagerepo.ManageInput{Principal: owner, Action: "REQUEST_BUILD", RecipeRef: superseded.Recipe.Ref, Mutation: roleImageTestMutation("failure-replacement-build", "REQUEST_BUILD", &version)})
	if err != nil || replacement.Build == nil {
		t.Fatalf("new owner build: %v", err)
	}
	availability, err = repository.GetSupplyWorkAvailability(ctx, worker)
	if err != nil || availability.AdmissionAvailable {
		t.Fatal("older PENDING artifact blocked latest queued build")
	}
	if _, err := repository.ClaimAdmission(ctx, worker, "failure-superseded-no-claim"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("old PENDING artifact was admitted after replacement build")
	}
	newest := seedPromotionArtifact(t, ctx, repository, owner, replacement.Recipe, *replacement.Build, "PENDING")
	claim, err = repository.ClaimAdmission(ctx, worker, "failure-newest-claim")
	if err != nil || claim.Artifact.Ref != newest.Ref {
		t.Fatalf("newest artifact claim: %v", err)
	}
	input = inputFor(claim, "failure-newest-result")
	if _, err := repository.FailAdmission(ctx, input); err != nil {
		t.Fatal(err)
	}
	created = create("race")
	claim, err = repository.ClaimAdmission(ctx, worker, "failure-race-claim")
	if err != nil {
		t.Fatal(err)
	}
	input = inputFor(claim, "failure-race-result")
	record := roleimagerepo.AdmissionRecordInput{ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt, Principal: worker, IdempotencyKey: "failure-race-record", ArtifactRef: claim.Artifact.Ref, ExpectedVersion: claim.Artifact.Version, ExpectedFence: claim.Fence, ClaimToken: claim.ClaimToken, ManifestDigest: claim.Artifact.ManifestDigest, ImmutableBuildSHA256: claim.Artifact.ImmutableBuildSHA256, ProvenanceSHA256: claim.Artifact.ProvenanceSHA256, PolicyRevision: claim.Artifact.PolicyRevision, PolicySHA256: claim.Artifact.PolicySHA256, Verdict: "ACCEPTED", SBOMSHA256: strings.Repeat("1", 64), VulnerabilityEvidenceSHA256: strings.Repeat("2", 64), SignatureIdentity: "synthetic-owner", SignatureSHA256: strings.Repeat("3", 64), AdmissionReceiptSHA256: strings.Repeat("4", 64), AdmissionReceiptOCIManifestDigest: "sha256:" + strings.Repeat("5", 64)}
	record.ToolInventoryJSON, record.ToolInventorySHA256 = imageInventoryFixture(claim.Artifact)
	record.VulnerabilityReportJSON, record.VulnerabilityReportProjectionSHA256, record.VulnerabilityEvidenceSHA256 = imageRiskReportFixture(t, claim.Artifact, record.SBOMSHA256, false)
	outcomes := make(chan error, 2)
	go func() { _, err := repository.FailAdmission(ctx, input); outcomes <- err }()
	go func() { _, err := repository.RecordAdmission(ctx, record); outcomes <- err }()
	winners := 0
	for i := 0; i < 2; i++ {
		err := <-outcomes
		if err == nil {
			winners++
		} else if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrVersionMismatch) {
			t.Fatalf("unexpected race result: %v", err)
		}
	}
	if winners != 1 {
		t.Fatal("record/failure race did not elect exactly one owner winner")
	}
	var terminalEvents int
	for _, state := range []string{"ADMISSION_WORKER_FAILED", "ACCEPTED"} {
		var count int
		if err := pool.QueryRow(ctx, queryImageAdmissionFailureEventCount, created.Recipe.Ref, created.Recipe.OrganizationRef, state).Scan(&count); err != nil {
			t.Fatal(err)
		}
		terminalEvents += count
	}
	if terminalEvents != 1 {
		t.Fatal("record/failure race emitted partial or duplicate terminal events")
	}
}
