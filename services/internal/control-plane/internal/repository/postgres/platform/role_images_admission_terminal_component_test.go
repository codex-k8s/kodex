package platform

import (
	"context"
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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Issue #1797: native PROJECT B1→B2 ранний terminal; proof только читает
// exact original claim receipt/attempt и не возобновляет отозванный grant.
func TestRoleImageAdmissionTerminalComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated admission terminal fixture")
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute,
		AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging",
		PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64),
		LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001",
		ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.role-images.recipes.manage"}, "control-api-gateway")
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	s, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "terminal-proof-project"},
		Payload: command.ProjectInput{Name: "Exact terminal proof", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create terminal proof project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, s, owner, project.Project.Ref, "terminal-proof-agent", "Synthetic admission role")
	_, recipe := promotionComponentCatalog(t)
	created, err := r.Manage(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "CREATE", ProjectRef: project.Project.Ref, RoleDefinitionRef: agent.RoleDefinitionRef,
		Name: "Terminal proof image", Recipe: recipe, Mutation: roleImageTestMutation("terminal-proof-create", "CREATE", nil)})
	if err != nil || created.Build == nil {
		t.Fatalf("native automatic B1: %v", err)
	}
	b1 := seedPromotionArtifact(t, ctx, r, resolved, created.Recipe, *created.Build, "PENDING")
	worker := resolved
	worker.CallerWorkload, worker.Permission = "image-admission", "platform.role-images.admission.claim"
	const b1Key = "terminal-proof-claim-b1"
	claim, err := r.ClaimAdmission(ctx, worker, b1Key)
	if err != nil || claim.Artifact.Ref != b1.Ref || claim.Artifact.AdmissionAttempt == nil {
		t.Fatalf("native B1 claim: %v", err)
	}
	version := int64(created.Recipe.Version)
	requested, err := r.Manage(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "REQUEST_BUILD", ProjectRef: project.Project.Ref, RecipeRef: created.Recipe.Ref,
		Mutation: roleImageTestMutation("terminal-proof-build-b2", "REQUEST_BUILD", &version)})
	if err != nil || requested.Build == nil || requested.Build.Ref == created.Build.Ref || requested.Recipe.Generation != created.Recipe.Generation {
		t.Fatalf("native same-generation B2: %v", err)
	}
	b2 := seedPromotionArtifact(t, ctx, r, resolved, requested.Recipe, *requested.Build, "PENDING")
	const b2Key = "terminal-proof-claim-b2"
	fresh, err := r.ClaimAdmission(ctx, worker, b2Key)
	if err != nil || fresh.Artifact.Ref != b2.Ref || fresh.AdmissionAttemptRef == claim.AdmissionAttemptRef {
		t.Fatalf("fresh B2 claim after revoked B1: %v", err)
	}
	inputFor := func(c entity.ImageAdmissionClaim, key string) roleimagerepo.AdmissionTerminalInput {
		a := c.Artifact
		return roleimagerepo.AdmissionTerminalInput{AdmissionExpiryInput: roleimagerepo.AdmissionExpiryInput{Principal: worker,
			ArtifactRef: a.Ref, ExpectedVersion: a.Version, ExpectedFence: c.Fence, ExpectedAuthorityGeneration: c.AuthorityGeneration,
			ExpectedAdmissionAttemptRef: c.AdmissionAttemptRef, ExpectedAdmissionAttempt: c.AdmissionAttempt,
			ManifestDigest: a.ManifestDigest, ImmutableBuildSHA256: a.ImmutableBuildSHA256, ProvenanceSHA256: a.ProvenanceSHA256,
			PolicyRevision: a.PolicyRevision, PolicySHA256: a.PolicySHA256, BuildRef: a.BuildRef, ExpectedBuildAttempt: a.BuildAttempt,
			RecipeGeneration: a.RecipeGeneration, SpecSHA256: a.SpecSHA256}, ClaimIdempotencyKey: key, RiskAcceptanceSHA256: c.RiskAcceptanceSHA256,
			SourceAdmissionRevision: c.SourceAdmissionRevision, SourceAdmissionReceiptSHA256: c.SourceAdmissionReceiptSHA256, SourceEvidenceManifestDigest: c.SourceEvidenceManifestDigest}
	}
	input := inputFor(claim, b1Key)
	// Иной активный actor того же tenant проходит resolveScope, но не владеет
	// original claim receipt. Не выдаём ему owner binding или worker grant.
	identity := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000089", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Foreign terminal actor", CallerWorkload: "control-api-gateway", Operation: "platform.query.projects.get"}
	if _, err := r.ResolveProofAuthority(ctx, identity); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("unbound foreign actor fixture: %v", err)
	}
	subjects, _, err := s.ListAccessSubjects(ctx, owner, query.Filter{Query: identity.ExternalDisplayName}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("resolve foreign actor fixture: %v", err)
	}
	foreignActor := worker
	foreignActor.ActorID = subjects[0].Ref
	if _, err := r.resolveScope(ctx, foreignActor); err != nil {
		t.Fatalf("same-tenant actor fixture is not active: %v", err)
	}
	// Снимок сравнивает все artifact/attempt поля, receipts/audit/outbox,
	// но не выводит synthetic claim token hashes или JSON в диагностику.
	type snapshot struct {
		artifacts, attempts      string
		audits, receipts, events int
	}
	readSnapshot := func() snapshot {
		t.Helper()
		var got snapshot
		err := pool.QueryRow(ctx, `SELECT
 md5(COALESCE(jsonb_agg(to_jsonb(artifact) ORDER BY artifact.ref)::text,'[]')),
 (SELECT md5(COALESCE(jsonb_agg(to_jsonb(attempt) ORDER BY attempt.ref)::text,'[]')) FROM control_plane.image_admission_attempts attempt WHERE attempt.artifact_id IN (SELECT id FROM control_plane.image_artifacts WHERE ref IN ($1,$2))),
 (SELECT count(*) FROM control_plane.audit_events),
 (SELECT count(*) FROM control_plane.idempotency_receipts),
 (SELECT count(*) FROM control_plane.outbox_events)
 FROM control_plane.image_artifacts artifact WHERE artifact.ref IN ($1,$2)`, b1.Ref, b2.Ref).Scan(&got.artifacts, &got.attempts, &got.audits, &got.receipts, &got.events)
		if err != nil {
			t.Fatal("read isolated terminal proof snapshot")
		}
		return got
	}
	before := readSnapshot()
	proof, err := r.GetAdmissionTerminal(ctx, input)
	if err != nil {
		t.Fatalf("read exact superseded B1 terminal: %v", err)
	}
	controller := worker
	controller.CallerWorkload, controller.Permission = "image-admission-controller", "platform.role-images.admission.recovery-terminal.get"
	// Credential поколения разных workload не сравниваются между собой.
	controller.CredentialRevision = 1
	controllerProof, err := r.GetAdmissionRecoveryTerminal(ctx, controller, b1Key)
	if err != nil || !reflect.DeepEqual(controllerProof, proof) || readSnapshot() != before {
		t.Fatalf("controller original receipt terminal proof: %v", err)
	}
	for _, scenario := range []string{"live", "key", "actor", "tenant", "workload", "permission", "credential"} {
		t.Run("controller-"+scenario, func(t *testing.T) {
			p, key := controller, b1Key
			switch scenario {
			case "live":
				key = b2Key
			case "key":
				key = "unknown-claim-receipt"
			case "actor":
				p.ActorID = foreignActor.ActorID
			case "tenant":
				p.AuthorityTenant = "org_synthetic_foreign_tenant"
			case "workload":
				p.CallerWorkload = "image-admission"
			case "permission":
				p.Permission = "platform.role-images.supply-work.get"
			case "credential":
				p.CredentialRevision = 0
			}
			if _, err := r.GetAdmissionRecoveryTerminal(ctx, p, key); !errors.Is(err, errs.ErrForbidden) || readSnapshot() != before {
				t.Fatalf("invalid controller recovery read %s: %v", scenario, err)
			}
		})
	}
	// Immutable receipt сравнивается в каноническом JSON: time.Time после
	// PostgreSQL→JSON roundtrip может иметь иной Location при том же instant.
	artifactJSONMatches := reflect.DeepEqual(asJSON(proof.ClaimedArtifact), asJSON(claim.Artifact))
	t.Logf("terminal proof: stateMatch=%t artifactReflectMatch=%t artifactJSONMatch=%t projectMatch=%t attemptMatch=%t artifactVersion=%d/%d fence=%d/%d attemptVersion=%d/%d",
		proof.State == "CANCELLED", reflect.DeepEqual(proof.ClaimedArtifact, claim.Artifact), artifactJSONMatches, proof.ClaimedArtifact.ProjectRef == project.Project.Ref,
		proof.AttemptRef == claim.AdmissionAttemptRef && proof.Attempt == claim.AdmissionAttempt, proof.TerminalArtifactVersion, claim.Artifact.Version+1,
		proof.TerminalFence, claim.Fence+1, proof.TerminalAttemptVersion, claim.Artifact.AdmissionAttempt.Version+1)
	if proof.State != "CANCELLED" || !artifactJSONMatches || proof.ClaimedArtifact.ProjectRef != project.Project.Ref ||
		proof.AttemptRef != claim.AdmissionAttemptRef || proof.Attempt != claim.AdmissionAttempt || proof.ClaimFence != claim.Fence || proof.ClaimAuthorityGeneration != claim.AuthorityGeneration ||
		proof.TerminalArtifactVersion != claim.Artifact.Version+1 || proof.TerminalFence != claim.Fence+1 || proof.TerminalAttemptVersion != claim.Artifact.AdmissionAttempt.Version+1 ||
		proof.RiskAcceptanceSHA256 != claim.RiskAcceptanceSHA256 || proof.SourceAdmissionRevision != claim.SourceAdmissionRevision ||
		proof.SourceAdmissionReceiptSHA256 != claim.SourceAdmissionReceiptSHA256 || proof.SourceEvidenceManifestDigest != claim.SourceEvidenceManifestDigest {
		t.Fatal("terminal proof lost exact original claim or server terminal pins")
	}
	if _, err := r.GetAdmissionTerminal(ctx, inputFor(fresh, b2Key)); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("live B2 exposed terminal proof: %v", err)
	}
	mutations := []struct {
		name   string
		change func(*roleimagerepo.AdmissionTerminalInput)
	}{
		{"unknown-claim-key", func(x *roleimagerepo.AdmissionTerminalInput) { x.ClaimIdempotencyKey = "terminal-proof-unknown-key" }},
		{"different-real-claim-key", func(x *roleimagerepo.AdmissionTerminalInput) { x.ClaimIdempotencyKey = b2Key }},
		{"foreign-actor", func(x *roleimagerepo.AdmissionTerminalInput) { x.Principal = foreignActor }},
		{"foreign-tenant", func(x *roleimagerepo.AdmissionTerminalInput) {
			x.Principal.AuthorityTenant = "org_synthetic_foreign_tenant"
		}},
		{"version", func(x *roleimagerepo.AdmissionTerminalInput) { x.ExpectedVersion++ }},
		{"fence", func(x *roleimagerepo.AdmissionTerminalInput) { x.ExpectedFence++ }},
		{"claim-authority-generation", func(x *roleimagerepo.AdmissionTerminalInput) { x.ExpectedAuthorityGeneration++ }},
		{"credential-rollback", func(x *roleimagerepo.AdmissionTerminalInput) { x.Principal.CredentialRevision = 0 }},
		{"artifact", func(x *roleimagerepo.AdmissionTerminalInput) { x.ArtifactRef = b2.Ref }},
		{"build", func(x *roleimagerepo.AdmissionTerminalInput) { x.BuildRef = b2.BuildRef }},
		{"build-attempt", func(x *roleimagerepo.AdmissionTerminalInput) { x.ExpectedBuildAttempt++ }},
		{"admission-attempt", func(x *roleimagerepo.AdmissionTerminalInput) {
			x.ExpectedAdmissionAttemptRef = fresh.AdmissionAttemptRef
		}},
		{"admission-attempt-number", func(x *roleimagerepo.AdmissionTerminalInput) { x.ExpectedAdmissionAttempt++ }},
		{"recipe-generation", func(x *roleimagerepo.AdmissionTerminalInput) { x.RecipeGeneration++ }},
		{"spec", func(x *roleimagerepo.AdmissionTerminalInput) { x.SpecSHA256 = strings.Repeat("0", 64) }},
		{"manifest", func(x *roleimagerepo.AdmissionTerminalInput) { x.ManifestDigest = "sha256:" + strings.Repeat("0", 64) }},
		{"immutable-build", func(x *roleimagerepo.AdmissionTerminalInput) { x.ImmutableBuildSHA256 = strings.Repeat("0", 64) }},
		{"provenance", func(x *roleimagerepo.AdmissionTerminalInput) { x.ProvenanceSHA256 = strings.Repeat("0", 64) }},
		{"policy-revision", func(x *roleimagerepo.AdmissionTerminalInput) { x.PolicyRevision++ }},
		{"policy-sha", func(x *roleimagerepo.AdmissionTerminalInput) { x.PolicySHA256 = strings.Repeat("0", 64) }},
		{"risk-acceptance", func(x *roleimagerepo.AdmissionTerminalInput) { x.RiskAcceptanceSHA256 = strings.Repeat("0", 64) }},
		{"source-admission-revision", func(x *roleimagerepo.AdmissionTerminalInput) { x.SourceAdmissionRevision++ }},
		{"source-admission-receipt", func(x *roleimagerepo.AdmissionTerminalInput) {
			x.SourceAdmissionReceiptSHA256 = strings.Repeat("0", 64)
		}},
		{"source-evidence-manifest", func(x *roleimagerepo.AdmissionTerminalInput) {
			x.SourceEvidenceManifestDigest = "sha256:" + strings.Repeat("0", 64)
		}},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			wrong := input
			test.change(&wrong)
			if _, err := r.GetAdmissionTerminal(ctx, wrong); !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("wrong terminal proof %s: %v", test.name, err)
			}
			if readSnapshot() != before {
				t.Fatal("denied terminal read changed owner state")
			}
		})
	}
	replay, err := r.GetAdmissionTerminal(ctx, input)
	if err != nil || !reflect.DeepEqual(replay, proof) || readSnapshot() != before {
		t.Fatalf("terminal read replay changed state or proof: %v", err)
	}
	freshAuthority := input
	freshAuthority.Principal.CorrelationRef = "terminal-proof-fresh-read"
	freshAuthority.Principal.CredentialRevision++
	rotated, err := r.GetAdmissionTerminal(ctx, freshAuthority)
	if err != nil || !reflect.DeepEqual(rotated, proof) || readSnapshot() != before {
		t.Fatalf("fresh proof authority changed immutable terminal result: %v", err)
	}
	// Read proof не разрешает stale verdict, fail или expiry заново.
	_, err = r.RecordAdmission(ctx, roleimagerepo.AdmissionRecordInput{Principal: worker, IdempotencyKey: "terminal-proof-stale-record", ArtifactRef: b1.Ref,
		ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt, ExpectedVersion: claim.Artifact.Version, ExpectedFence: claim.Fence, ClaimToken: claim.ClaimToken, Verdict: "ACCEPTED"})
	if !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("terminal proof revived stale record: %v", err)
	}
	_, err = r.FailAdmission(ctx, roleimagerepo.AdmissionFailureInput{Principal: worker, IdempotencyKey: "terminal-proof-stale-fail", ArtifactRef: b1.Ref,
		ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt, ExpectedVersion: claim.Artifact.Version, ExpectedFence: claim.Fence,
		ExpectedAuthorityGeneration: claim.AuthorityGeneration, ClaimToken: claim.ClaimToken, ManifestDigest: b1.ManifestDigest, ImmutableBuildSHA256: b1.ImmutableBuildSHA256,
		ProvenanceSHA256: b1.ProvenanceSHA256, PolicyRevision: b1.PolicyRevision, PolicySHA256: b1.PolicySHA256, BuildRef: b1.BuildRef, ExpectedBuildAttempt: b1.BuildAttempt,
		RecipeGeneration: b1.RecipeGeneration, SpecSHA256: b1.SpecSHA256, ErrorCode: "SYNTHETIC_WORKER_FAILURE"})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("terminal proof revived stale failure: %v", err)
	}
	expiry := input.AdmissionExpiryInput
	expiry.IdempotencyKey = "terminal-proof-stale-expire"
	if _, err := r.ExpireAdmission(ctx, expiry); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("terminal proof revived stale expiry: %v", err)
	}
	if readSnapshot() != before {
		t.Fatal("terminal proof or stale callback changed owner state")
	}
}
