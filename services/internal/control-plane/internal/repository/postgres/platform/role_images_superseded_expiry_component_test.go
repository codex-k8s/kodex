package platform

import (
	"context"
	_ "embed"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/role_images_superseded_expiry_clock.sql
var querySupersededAdmissionExpiryClock string

//go:embed testdata/sql/role_images_superseded_expiry_readback.sql
var querySupersededAdmissionExpiryReadback string

// Issue #1797: отдельная disposable БД; production TTL и triggers не меняются.
// Матрица разделяет native раннее закрытие B1 при B2 и обычный exact expiry.
// PASS repository не подтверждает cleanup consumer image-admission recovery.
func TestRoleImageSupersededAdmissionExpiryComponent(t *testing.T) {
	t.Run("native-b2-early-terminal", func(t *testing.T) { testRoleImageAdmissionTerminal(t, true) })
	t.Run("ordinary-exact-expiry-replay", func(t *testing.T) { testRoleImageAdmissionTerminal(t, false) })
}

func testRoleImageAdmissionTerminal(t *testing.T, superseded bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated admission expiry fixture")
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles",
		DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.role-images.recipes.manage"}, "control-api-gateway")
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	s, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "superseded-expiry-project"}, Payload: command.ProjectInput{Name: "Superseded admission expiry", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, s, owner, project.Project.Ref, "superseded-expiry-agent", "Synthetic image role")
	_, recipeInput := promotionComponentCatalog(t)
	created, err := r.Manage(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "CREATE", ProjectRef: project.Project.Ref,
		RoleDefinitionRef: agent.RoleDefinitionRef, Name: "Superseded image", Recipe: recipeInput,
		Mutation: roleImageTestMutation("superseded-expiry-create", "CREATE", nil)})
	if err != nil || created.Build == nil {
		t.Fatalf("native recipe creation did not create B1: %v", err)
	}
	// Существующая оснастка завершает синтетическую сборку штатными owner SQL
	// без registry/network и не подделывает admission verdict.
	b1 := seedPromotionArtifact(t, ctx, r, resolved, created.Recipe, *created.Build, "PENDING")
	worker := resolved
	worker.CallerWorkload, worker.Permission = "image-admission", "platform.role-images.admission.claim"
	claim, err := r.ClaimAdmission(ctx, worker, "superseded-expiry-claim-b1")
	if err != nil || claim.Artifact.Ref != b1.Ref || claim.AdmissionAttemptRef == "" || claim.Artifact.AdmissionAttempt == nil {
		t.Fatalf("claim B1: %v", err)
	}
	var requested roleimagerepo.ManageResult
	var b2 entity.ImageArtifact
	if superseded {
		detail, err := r.Get(ctx, resolved, created.Recipe.Ref)
		if err != nil {
			t.Fatal(err)
		}
		version := int64(detail.Recipe.Version)
		requested, err = r.Manage(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "REQUEST_BUILD", ProjectRef: project.Project.Ref,
			RecipeRef: created.Recipe.Ref, Mutation: roleImageTestMutation("superseded-expiry-build-b2", "REQUEST_BUILD", &version)})
		if err != nil || requested.Build == nil || requested.Build.Ref == created.Build.Ref || requested.Recipe.Generation != created.Recipe.Generation {
			t.Fatalf("native B2 same-generation request mismatch: %v", err)
		}
		b2 = seedPromotionArtifact(t, ctx, r, resolved, requested.Recipe, *requested.Build, "PENDING")
		if b2.BuildRef != requested.Build.Ref || b2.BuildAttempt != requested.Build.Attempt {
			t.Fatal("B2 completion pins mismatch")
		}
	}
	readback := func() admissionExpiryReadback {
		t.Helper()
		var got admissionExpiryReadback
		if err := pool.QueryRow(ctx, querySupersededAdmissionExpiryReadback, b1.Ref, claim.AdmissionAttemptRef).Scan(
			&got.State, &got.Verdict, &got.Version, &got.Fence, &got.ClaimLive, &got.ClaimRevoked, &got.PromotionRevoked,
			&got.AttemptState, &got.AttemptVersion, &got.AttemptFence, &got.AttemptFinished, &got.ExactTerminalSnapshot, &got.ExactSourceSnapshot, &got.ExpiryAudits, &got.ExpiryReceipts); err != nil {
			t.Fatal("read expiry fixture state")
		}
		return got
	}
	before := readback()
	if !before.ExactSourceSnapshot {
		t.Fatal("attempt lost exact source artifact/build/spec pins")
	}
	if superseded {
		if before.State != "REJECTED" || before.Verdict != "" || before.Version != claim.Artifact.Version+1 || before.Fence != claim.Fence+1 ||
			before.ClaimLive || !before.ClaimRevoked || !before.PromotionRevoked || before.AttemptState != "CANCELLED" || !before.AttemptFinished || !before.ExactTerminalSnapshot ||
			before.AttemptVersion != claim.Artifact.AdmissionAttempt.Version+1 || before.AttemptFence != claim.Fence+1 {
			t.Fatalf("partial native B2 early-terminal: %+v", before)
		}
	} else if before.State != "CLAIMED" || !before.ClaimLive || before.ClaimRevoked || before.AttemptState != "CLAIMED" || before.AttemptFinished ||
		before.AttemptVersion != claim.Artifact.AdmissionAttempt.Version || before.AttemptFence != claim.Fence {
		t.Fatalf("normal native claim mismatch: %+v", before)
	}
	expiry := roleimagerepo.AdmissionExpiryInput{Principal: worker, IdempotencyKey: "superseded-expiry-terminal", ArtifactRef: b1.Ref,
		ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt, ExpectedVersion: claim.Artifact.Version, ExpectedFence: claim.Fence,
		ExpectedAuthorityGeneration: claim.AuthorityGeneration, ManifestDigest: b1.ManifestDigest, ImmutableBuildSHA256: b1.ImmutableBuildSHA256, ProvenanceSHA256: b1.ProvenanceSHA256,
		PolicyRevision: b1.PolicyRevision, PolicySHA256: b1.PolicySHA256, BuildRef: b1.BuildRef, ExpectedBuildAttempt: b1.BuildAttempt, RecipeGeneration: b1.RecipeGeneration, SpecSHA256: b1.SpecSHA256}
	if superseded {
		inventory, inventorySHA := imageInventoryFixture(claim.Artifact)
		record := roleimagerepo.AdmissionRecordInput{Principal: worker, IdempotencyKey: "superseded-expiry-stale-record", ArtifactRef: b1.Ref,
			ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt, ExpectedVersion: claim.Artifact.Version, ExpectedFence: claim.Fence,
			ClaimToken: claim.ClaimToken, ManifestDigest: b1.ManifestDigest, ImmutableBuildSHA256: b1.ImmutableBuildSHA256, ProvenanceSHA256: b1.ProvenanceSHA256,
			PolicyRevision: b1.PolicyRevision, PolicySHA256: b1.PolicySHA256, Verdict: "ACCEPTED", ToolInventoryJSON: inventory, ToolInventorySHA256: inventorySHA}
		_, err = r.RecordAdmission(ctx, record)
		if !errors.Is(err, errs.ErrVersionMismatch) {
			t.Fatalf("stale B1 record was not denied: %v", err)
		}
		failure := roleimagerepo.AdmissionFailureInput{Principal: worker, IdempotencyKey: "superseded-expiry-stale-fail", ArtifactRef: b1.Ref, ClaimToken: claim.ClaimToken,
			ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt, ExpectedVersion: claim.Artifact.Version, ExpectedFence: claim.Fence,
			ExpectedAuthorityGeneration: claim.AuthorityGeneration, ManifestDigest: b1.ManifestDigest, ImmutableBuildSHA256: b1.ImmutableBuildSHA256, ProvenanceSHA256: b1.ProvenanceSHA256,
			PolicyRevision: b1.PolicyRevision, PolicySHA256: b1.PolicySHA256, BuildRef: b1.BuildRef, ExpectedBuildAttempt: b1.BuildAttempt, RecipeGeneration: b1.RecipeGeneration, SpecSHA256: b1.SpecSHA256, ErrorCode: "SYNTHETIC_WORKER_FAILURE"}
		_, err = r.FailAdmission(ctx, failure)
		if !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("stale B1 failure was not denied: %v", err)
		}
	}
	if _, err := r.ExpireAdmission(ctx, expiry); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("premature expiry was not denied: %v", err)
	}
	if after := readback(); after != before {
		t.Fatal("denied callbacks changed B1 state, audit or receipt")
	}
	// Обычный expiry использует native claim; меняется лишь disposable clock.
	// Для superseded claim fixture не переписывает уже отозванную authority.
	if !superseded {
		if tag, err := pool.Exec(ctx, querySupersededAdmissionExpiryClock, b1.Ref, claim.Artifact.Version, claim.Fence); err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("advance exact isolated B1 expiry: %v", err)
		}
	}
	// Синтетический verified context чужого tenant не должен разрешить artifact.
	foreign := worker
	foreign.AuthorityTenant = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	for _, kind := range []string{"fence", "version", "tenant"} {
		wrong := expiry
		wrong.IdempotencyKey += "-wrong-" + kind
		want := errs.ErrForbidden
		switch kind {
		case "fence":
			wrong.ExpectedFence++
		case "version":
			wrong.ExpectedVersion++
			if !superseded {
				want = errs.ErrVersionMismatch
			}
		case "tenant":
			wrong.Principal = foreign
			want = errs.ErrForbidden
		}
		if _, err := r.ExpireAdmission(ctx, wrong); !errors.Is(err, want) {
			t.Fatalf("wrong %s expiry: %v", kind, err)
		}
	}
	if superseded {
		if after := readback(); after != before {
			t.Fatal("negative expiry changed native early-terminal state")
		}
		fresh, err := r.ClaimAdmission(ctx, worker, "superseded-expiry-claim-b2")
		if err != nil || fresh.Artifact.Ref != b2.Ref || fresh.Artifact.BuildRef != requested.Build.Ref || fresh.AdmissionAttemptRef == claim.AdmissionAttemptRef {
			t.Fatalf("fresh B2 claim: %v", err)
		}
		if after := readback(); after != before {
			t.Fatal("fresh B2 claim changed superseded B1 terminal snapshot")
		}
		return
	}
	result, err := r.ExpireAdmission(ctx, expiry)
	if err != nil || result.State != "FAILED" || result.ErrorCode != "ADMISSION_LEASE_EXPIRED" || result.Version != claim.Artifact.Version+1 {
		t.Fatalf("exact old B1 expiry: %v", err)
	}
	terminal := readback()
	if terminal.State != "FAILED" || terminal.Verdict != "" || terminal.ClaimLive || !terminal.ClaimRevoked || terminal.AttemptState != "FAILED" || !terminal.AttemptFinished || !terminal.ExactTerminalSnapshot || !terminal.ExactSourceSnapshot ||
		terminal.AttemptVersion != before.AttemptVersion+1 || terminal.AttemptFence != claim.Fence || terminal.ExpiryAudits != 1 || terminal.ExpiryReceipts != 1 {
		t.Fatalf("partial B1 terminal receipt: %+v", terminal)
	}
	replay, err := r.ExpireAdmission(ctx, expiry)
	if err != nil || !reflect.DeepEqual(replay, result) || readback() != terminal {
		t.Fatalf("expiry replay repeated effect: %v", err)
	}
}

type admissionExpiryReadback struct {
	State, Verdict, AttemptState                                                                           string
	Version, Fence                                                                                         uint64
	AttemptVersion, AttemptFence                                                                           uint64
	ClaimLive, ClaimRevoked, PromotionRevoked, AttemptFinished, ExactTerminalSnapshot, ExactSourceSnapshot bool
	ExpiryAudits, ExpiryReceipts                                                                           int
}
