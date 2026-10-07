package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
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

func imageRiskReportFixture(t *testing.T, artifact entity.ImageArtifact, sbom string, blocking bool) (string, string, string) {
	t.Helper()
	matches := "[]"
	count := "0"
	if blocking {
		count = "1"
		matches = `[{"artifact":{"name":"synthetic-package","version":"1.0.0","type":"go-module"},"vulnerability":{"id":"CVE-2026-12345","severity":"High","fix":{"state":"fixed","versions":["1.0.1"]}}}]`
	}
	raw := []byte(`{"matches":` + matches + `,"ignoredMatches":[],"kodexPolicy":{"schema":"kodex.dev/fix-available-high-or-critical/v1","policyRevision":1,"policySHA256":"` + artifact.PolicySHA256 + `","highOrCriticalMatchCount":` + count + `,"blockingMatchCount":` + count + `,"unresolvedNoFixMatchCount":0}}`)
	report, err := runtimecontract.ProjectImageVulnerabilityReport(raw, runtimecontract.ImageVulnerabilityReport{ArtifactRef: artifact.Ref, ImageDigest: artifact.ManifestDigest, SBOMSHA256: sbom, ScopeKind: artifact.ScopeKind, OrganizationRef: artifact.OrganizationRef, ProjectRef: artifact.ProjectRef, RecipeRef: artifact.RecipeRef, RecipeVersion: artifact.RecipeVersion, RecipeGeneration: artifact.RecipeGeneration, BuildRef: artifact.BuildRef, BuildVersion: artifact.BuildVersion, BuildAttempt: artifact.BuildAttempt, PolicyRevision: artifact.PolicyRevision, PolicySHA256: artifact.PolicySHA256})
	if err != nil {
		t.Fatal("construct canonical report", err)
	}
	projection, err := runtimecontract.CanonicalImageVulnerabilityReport(report)
	if err != nil {
		t.Fatal(err)
	}
	return string(projection), runtimecontract.ImageVulnerabilitySHA256(projection), report.ReportSHA256
}

// Только disposable PostgreSQL: реальные owner TX, синтетические scanner bytes.
func TestImageAdmissionRiskComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles", DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	var graphNodes, graphEdges int
	var graphNodeHash, graphEdgeHash string
	if err := pool.QueryRow(ctx, `WITH RECURSIVE nodes(relation_id) AS (SELECT 'control_plane.projects'::regclass::oid UNION SELECT c.conrelid FROM nodes JOIN pg_constraint c ON c.contype='f' AND c.confrelid=nodes.relation_id JOIN pg_namespace n ON n.oid=c.connamespace AND n.nspname='control_plane') SELECT (SELECT count(*) FROM nodes),(SELECT md5(string_agg(n.nspname||'.'||c.relname,E'\n' ORDER BY n.nspname,c.relname)) FROM nodes JOIN pg_class c ON c.oid=nodes.relation_id JOIN pg_namespace n ON n.oid=c.relnamespace),count(*),md5(string_agg(cn.nspname||'.'||cc.relname||'|'||c.conname||'|'||pn.nspname||'.'||pc.relname||'|'||c.confdeltype::text,E'\n' ORDER BY cn.nspname,cc.relname,c.conname)) FROM pg_constraint c JOIN pg_class cc ON cc.oid=c.conrelid JOIN pg_namespace cn ON cn.oid=cc.relnamespace JOIN pg_class pc ON pc.oid=c.confrelid JOIN pg_namespace pn ON pn.oid=pc.relnamespace WHERE c.contype='f' AND c.confrelid IN (SELECT relation_id FROM nodes) AND c.conrelid IN (SELECT relation_id FROM nodes)`).Scan(&graphNodes, &graphNodeHash, &graphEdges, &graphEdgeHash); err != nil {
		t.Fatal(err)
	}
	t.Logf("project purge graph nodes=%d hash=%s edges=%d hash=%s", graphNodes, graphNodeHash, graphEdges, graphEdgeHash)
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.organization.role-images.recipes.manage"}, "control-api-gateway")
	verifiedOwner := owner
	owner, err = r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	projectResult, err := platform.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: verifiedOwner, Mutation: value.Mutation{IdempotencyKey: "risk-project"}, Payload: command.ProjectInput{Name: "Risk fixture", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := createLifecycleAgent(t, ctx, platform, verifiedOwner, projectResult.Project.Ref, "risk-agent", "Risk specialist")
	worker := owner
	worker.CallerWorkload = "image-admission"
	worker.Permission = "platform.role-images.admission.claim"
	worker.CredentialRevision = 1
	for _, scopeKind := range []string{"ORGANIZATION", "PROJECT"} {
		t.Run(scopeKind, func(t *testing.T) {
			_, spec := promotionComponentCatalog(t)
			input := roleimagerepo.ManageInput{Principal: owner, Action: "CREATE", Name: "Risk " + scopeKind, Recipe: spec, Mutation: roleImageTestMutation("risk-create-"+scopeKind, "CREATE", nil)}
			var created roleimagerepo.ManageResult
			if scopeKind == "ORGANIZATION" {
				created, err = r.ManageOrganization(ctx, input)
			} else {
				input.ProjectRef = projectResult.Project.Ref
				input.RoleDefinitionRef = agent.RoleDefinitionRef
				created, err = r.Manage(ctx, input)
			}
			if err != nil {
				t.Fatal(err)
			}
			seedPromotionArtifact(t, ctx, r, owner, created.Recipe, *created.Build, "PENDING")
			claim, err := r.ClaimAdmission(ctx, worker, "risk-claim-"+scopeKind)
			if err != nil {
				t.Fatal("claim", err)
			}
			a := claim.Artifact
			if a.AdmissionAttempt == nil || a.AdmissionAttempt.Ref != claim.AdmissionAttemptRef || a.AdmissionAttempt.Number != claim.AdmissionAttempt || a.AdmissionAttempt.Fence != claim.Fence || a.AdmissionAttempt.State != "CLAIMED" {
				t.Fatal("claim lost authoritative nested attempt")
			}
			missingFilter := roleimagerepo.VulnerabilityReportFilter{ScopeKind: scopeKind, ProjectRef: created.Recipe.ProjectRef, RecipeRef: created.Recipe.Ref, ArtifactRef: a.Ref}
			missing, err := r.GetVulnerabilityReport(ctx, owner, missingFilter)
			if err != nil || missing.Available || len(missing.NextActions) != 1 || missing.NextActions[0] != "REBUILD_FOR_REPORT" {
				t.Fatal("missing projection did not require rebuild", err)
			}
			record := roleimagerepo.AdmissionRecordInput{Principal: worker, IdempotencyKey: "risk-record-" + scopeKind, ArtifactRef: a.Ref, ClaimToken: claim.ClaimToken, ExpectedVersion: a.Version, ExpectedFence: claim.Fence, ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt, ManifestDigest: a.ManifestDigest, ImmutableBuildSHA256: a.ImmutableBuildSHA256, ProvenanceSHA256: a.ProvenanceSHA256, PolicyRevision: a.PolicyRevision, PolicySHA256: a.PolicySHA256, Verdict: "REJECTED", SBOMSHA256: strings.Repeat("1", 64), SignatureIdentity: "synthetic-owner", SignatureSHA256: strings.Repeat("3", 64), AdmissionReceiptSHA256: strings.Repeat("4", 64), AdmissionReceiptOCIManifestDigest: "sha256:" + strings.Repeat("5", 64)}
			record.ToolInventoryJSON, record.ToolInventorySHA256 = imageInventoryFixture(a)
			record.VulnerabilityReportJSON, record.VulnerabilityReportProjectionSHA256, record.VulnerabilityEvidenceSHA256 = imageRiskReportFixture(t, a, record.SBOMSHA256, true)
			rejected, err := r.RecordAdmission(ctx, record)
			if err != nil {
				t.Fatal("record rejected", err)
			}
			filter := roleimagerepo.VulnerabilityReportFilter{ScopeKind: scopeKind, ProjectRef: created.Recipe.ProjectRef, RecipeRef: created.Recipe.Ref, ArtifactRef: a.Ref, Page: query.Page{Size: 1}}
			report, err := r.GetVulnerabilityReport(ctx, owner, filter)
			if err != nil || !report.Available || len(report.Findings) != 1 || report.Report.BlockingMatchCount != 1 {
				t.Fatalf("report unavailable: %v", err)
			}
			wrongHash := filter
			wrongHash.ExpectedReportSHA256 = strings.Repeat("0", 64)
			if _, err := r.GetVulnerabilityReport(ctx, owner, wrongHash); !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("wrong report pin: %v", err)
			}
			foreignScope := filter
			if scopeKind == "PROJECT" {
				foreignScope.ScopeKind = "ORGANIZATION"
				foreignScope.ProjectRef = ""
			} else {
				foreignScope.ScopeKind = "PROJECT"
				foreignScope.ProjectRef = projectResult.Project.Ref
			}
			if _, err := r.GetVulnerabilityReport(ctx, owner, foreignScope); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("foreign report scope: %v", err)
			}
			version := int64(rejected.Version)
			decision := roleimagerepo.AdmissionRiskInput{Principal: owner, ScopeKind: scopeKind, ProjectRef: created.Recipe.ProjectRef, RecipeRef: created.Recipe.Ref, ArtifactRef: a.Ref, ExpectedArtifactVersion: rejected.Version, ExpectedAdmissionRevision: rejected.AdmissionRevision, ExpectedRecipeVersion: created.Recipe.Version, ExpectedRecipeGeneration: created.Recipe.Generation, ExpectedBuildRef: a.BuildRef, ExpectedBuildAttempt: a.BuildAttempt, ManifestDigest: a.ManifestDigest, VulnerabilityEvidenceSHA256: rejected.VulnerabilityEvidenceSHA256, ProjectionSHA256: report.ProjectionSHA256, PriorAdmissionReceiptSHA256: rejected.AdmissionReceiptSHA256, PriorEvidenceManifestDigest: rejected.AdmissionReceiptOCIManifestDigest, PolicyRevision: a.PolicyRevision, PolicySHA256: a.PolicySHA256, Action: "ACCEPT_RISK", Reason: "Synthetic owner acceptance", Mutation: roleImageTestMutation("risk-decide-"+scopeKind, "platform.command.organization.role-images.risk.decide", &version)}
			if scopeKind == "PROJECT" {
				decision.Mutation.Operation = "platform.command.role-images.risk.decide"
			}
			denied := decision
			denied.Principal.CallerWorkload = "runtime-controller"
			if _, err := r.DecideAdmissionRisk(ctx, denied); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("assistant risk authority: %v", err)
			}
			denied = decision
			denied.Principal.ProjectRef = projectResult.Project.Ref
			if _, err := r.DecideAdmissionRisk(ctx, denied); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("signed project risk authority: %v", err)
			}
			stale := decision
			stale.ExpectedArtifactVersion++
			if _, err := r.DecideAdmissionRisk(ctx, stale); !errors.Is(err, errs.ErrVersionMismatch) {
				t.Fatalf("stale OCC: %v", err)
			}
			if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "MEMBER"); err != nil {
				t.Fatal(err)
			}
			if _, err := r.GetVulnerabilityReport(ctx, owner, filter); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("member read: %v", err)
			}
			if _, err := r.DecideAdmissionRisk(ctx, decision); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("member write: %v", err)
			}
			if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "ADMINISTRATOR"); err != nil {
				t.Fatal(err)
			}
			accepted, err := r.DecideAdmissionRisk(ctx, decision)
			if err != nil {
				t.Fatal("accept risk", err)
			}
			if accepted.AdmissionAttempt == nil || accepted.AdmissionAttempt.State != "PENDING" || accepted.Artifact.Version != rejected.Version+1 || accepted.Artifact.AdmissionVerdict != "" {
				t.Fatal("risk acceptance fabricated accepted evidence")
			}
			replay, err := r.DecideAdmissionRisk(ctx, decision)
			if err != nil || replay.Decision.Ref != accepted.Decision.Ref || replay.AdmissionAttempt.Ref != accepted.AdmissionAttempt.Ref {
				t.Fatalf("decision replay: %v", err)
			}
			if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "MEMBER"); err != nil {
				t.Fatal(err)
			}
			if _, err := r.DecideAdmissionRisk(ctx, decision); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("revoked admin receipt replay: %v", err)
			}
			if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "OWNER"); err != nil {
				t.Fatal(err)
			}
			next, err := r.ClaimAdmission(ctx, worker, "risk-reclaim-"+scopeKind)
			if err != nil || next.AdmissionAttempt <= claim.AdmissionAttempt || next.RiskAcceptanceSHA256 == "" || next.SourceAdmissionRevision != rejected.AdmissionRevision {
				t.Fatalf("risk claim pins: %v", err)
			}
			decoded, err := runtimecontract.DecodeImageRiskAcceptance([]byte(next.RiskAcceptanceJSON))
			if err != nil || decoded.DecisionRef != accepted.Decision.Ref {
				t.Fatal("invalid acceptance projection", err)
			}
			late := record
			late.IdempotencyKey = "risk-late-" + scopeKind
			if _, err := r.RecordAdmission(ctx, late); err == nil {
				t.Fatal("old fenced ACK succeeded")
			}
			nextRecord := record
			nextRecord.IdempotencyKey = "risk-new-record-" + scopeKind
			nextRecord.ExpectedVersion = next.Artifact.Version
			nextRecord.ExpectedFence = next.Fence
			nextRecord.ClaimToken = next.ClaimToken
			nextRecord.ExpectedAdmissionAttemptRef = next.AdmissionAttemptRef
			nextRecord.ExpectedAdmissionAttempt = next.AdmissionAttempt
			nextRecord.RiskAcceptanceSHA256 = next.RiskAcceptanceSHA256
			nextRecord.Verdict = "ACCEPTED"
			nextRecord.AdmissionReceiptSHA256 = strings.Repeat("6", 64)
			nextRecord.AdmissionReceiptOCIManifestDigest = "sha256:" + strings.Repeat("7", 64)
			final, err := r.RecordAdmission(ctx, nextRecord)
			if err != nil || final.AdmissionRevision != rejected.AdmissionRevision+1 || final.AdmissionVerdict != "ACCEPTED" {
				t.Fatalf("new signed admission: %v", err)
			}
			if final.AdmissionAttempt == nil || final.AdmissionAttempt.Ref != next.AdmissionAttemptRef || final.AdmissionAttempt.State != "ACCEPTED" || final.AdmissionAttempt.AdmissionReceiptSHA256 != nextRecord.AdmissionReceiptSHA256 || final.AdmissionAttempt.EvidenceManifestDigest != nextRecord.AdmissionReceiptOCIManifestDigest {
				t.Fatal("terminal record lost exact nested receipt")
			}
			if _, err := r.DecideAdmissionRisk(ctx, decision); err == nil {
				t.Fatal("old report decision survived new admission")
			}
			var count int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.image_vulnerability_reports WHERE artifact_id=(SELECT id FROM control_plane.image_artifacts WHERE ref=$1)", a.Ref).Scan(&count); err != nil || count != 2 {
				t.Fatal("immutable report history missing", err)
			}
			for _, action := range []string{"REJECT_RISK", "ARCHIVE", "UPDATE", "REQUEST_BUILD"} {
				t.Run(action, func(t *testing.T) {
					caseInput := input
					caseInput.Name = "Lifecycle " + scopeKind + " " + action
					caseInput.Mutation = roleImageTestMutation("risk-lifecycle-create-"+scopeKind+action, "CREATE", nil)
					manage := r.Manage
					if scopeKind == "ORGANIZATION" {
						manage = r.ManageOrganization
					}
					caseCreated, err := manage(ctx, caseInput)
					if err != nil {
						t.Fatal(err)
					}
					seedPromotionArtifact(t, ctx, r, owner, caseCreated.Recipe, *caseCreated.Build, "PENDING")
					caseClaim, err := r.ClaimAdmission(ctx, worker, "risk-lifecycle-claim-"+scopeKind+action)
					if err != nil {
						t.Fatal(err)
					}
					ca := caseClaim.Artifact
					caseRecord := record
					caseRecord.IdempotencyKey = "risk-lifecycle-record-" + scopeKind + action
					caseRecord.ArtifactRef = ca.Ref
					caseRecord.ManifestDigest = ca.ManifestDigest
					caseRecord.ImmutableBuildSHA256 = ca.ImmutableBuildSHA256
					caseRecord.ProvenanceSHA256 = ca.ProvenanceSHA256
					caseRecord.ExpectedVersion = ca.Version
					caseRecord.ExpectedFence = caseClaim.Fence
					caseRecord.ClaimToken = caseClaim.ClaimToken
					caseRecord.ExpectedAdmissionAttemptRef = caseClaim.AdmissionAttemptRef
					caseRecord.ExpectedAdmissionAttempt = caseClaim.AdmissionAttempt
					caseRecord.ToolInventoryJSON, caseRecord.ToolInventorySHA256 = imageInventoryFixture(ca)
					caseRecord.VulnerabilityReportJSON, caseRecord.VulnerabilityReportProjectionSHA256, caseRecord.VulnerabilityEvidenceSHA256 = imageRiskReportFixture(t, ca, caseRecord.SBOMSHA256, true)
					caseRejected, err := r.RecordAdmission(ctx, caseRecord)
					if err != nil {
						t.Fatal(err)
					}
					caseDecision := decision
					caseVersion := int64(caseRejected.Version)
					caseDecision.RecipeRef = caseCreated.Recipe.Ref
					caseDecision.ArtifactRef = ca.Ref
					caseDecision.ExpectedBuildRef = ca.BuildRef
					caseDecision.ExpectedArtifactVersion = caseRejected.Version
					caseDecision.ExpectedAdmissionRevision = caseRejected.AdmissionRevision
					caseDecision.ExpectedRecipeVersion = caseCreated.Recipe.Version
					caseDecision.ExpectedRecipeGeneration = caseCreated.Recipe.Generation
					caseDecision.ManifestDigest = ca.ManifestDigest
					caseDecision.VulnerabilityEvidenceSHA256 = caseRejected.VulnerabilityEvidenceSHA256
					caseDecision.ProjectionSHA256 = caseRecord.VulnerabilityReportProjectionSHA256
					caseDecision.Mutation = roleImageTestMutation("risk-lifecycle-decision-"+scopeKind+action, decision.Mutation.Operation, &caseVersion)
					if action == "REJECT_RISK" {
						caseDecision.Action = action
					}
					result, err := r.DecideAdmissionRisk(ctx, caseDecision)
					if err != nil {
						t.Fatal(err)
					}
					if action == "REJECT_RISK" {
						if result.AdmissionAttempt != nil || result.Artifact.Version != caseRejected.Version || result.Artifact.AdmissionVerdict != "REJECTED" || result.Decision.Action != action {
							t.Fatal("rejection changed signed state")
						}
						replay, err := r.DecideAdmissionRisk(ctx, caseDecision)
						if err != nil || replay.Decision.Ref != result.Decision.Ref {
							t.Fatal("reject replay", err)
						}
						return
					}
					caseInput.Action = action
					caseInput.RecipeRef = caseCreated.Recipe.Ref
					caseInput.Name += " updated"
					caseRecipeVersion := int64(caseCreated.Recipe.Version)
					caseInput.Mutation = roleImageTestMutation("risk-lifecycle-change-"+scopeKind+action, action, &caseRecipeVersion)
					if _, err := manage(ctx, caseInput); err != nil {
						t.Fatal("invalidate risk attempt", err)
					}
					var attemptState string
					if err := pool.QueryRow(ctx, "SELECT state FROM control_plane.image_admission_attempts WHERE ref=$1", result.AdmissionAttempt.Ref).Scan(&attemptState); err != nil || attemptState != "CANCELLED" {
						t.Fatal("lifecycle retained active risk attempt", err)
					}
					if _, err := r.DecideAdmissionRisk(ctx, caseDecision); err == nil {
						t.Fatal("lifecycle replay revived old decision")
					}
					if err := pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.image_vulnerability_reports WHERE artifact_id=(SELECT id FROM control_plane.image_artifacts WHERE ref=$1)", ca.Ref).Scan(&count); err != nil || count != 1 {
						t.Fatal("lifecycle rewrote report history", err)
					}
				})
			}
		})
	}
	var organizationReports int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.image_vulnerability_reports report JOIN control_plane.image_artifacts artifact ON artifact.id=report.artifact_id WHERE artifact.scope_kind='ORGANIZATION'").Scan(&organizationReports); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM control_plane.image_vulnerability_reports WHERE organization_id=$1", current.organizationID); err == nil {
		t.Fatal("ordinary runtime deleted immutable report")
	}
	var projectID string
	if err := pool.QueryRow(ctx, "SELECT id FROM control_plane.projects WHERE organization_id=$1 AND ref=$2", current.organizationID, projectResult.Project.Ref).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	candidate := ProjectPurgeCandidate{OrganizationID: current.organizationID, OrganizationRef: current.organizationRef, ProjectID: projectID, ProjectRef: projectResult.Project.Ref}
	if err := r.FinalizeProjectPurge(ctx, candidate, nil); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("history purge without receipt: %v", err)
	}
	freshProject, err := platform.GetProject(ctx, verifiedOwner, projectResult.Project.Ref)
	if err != nil {
		t.Fatal(err)
	}
	trashed, err := platform.Execute(ctx, command.Command{Kind: command.TrashProject, Principal: verifiedOwner, Mutation: value.Mutation{IdempotencyKey: "risk-trash-project", ExpectedVersion: &freshProject.Version}, Payload: command.ProjectLifecycleInput{Ref: freshProject.Ref}})
	if err != nil {
		t.Fatal("trash risk project", err)
	}
	pending, err := platform.Execute(ctx, command.Command{Kind: command.PurgeProject, Principal: verifiedOwner, Mutation: value.Mutation{IdempotencyKey: "risk-purge-project", ExpectedVersion: &trashed.Project.Version}, Payload: command.ProjectLifecycleInput{Ref: freshProject.Ref}})
	if err != nil || pending.Project.Lifecycle != "PURGE_PENDING" {
		t.Fatal("purge intent", err)
	}
	inventory, err := r.ProjectPurgeInventory(ctx, candidate)
	if err != nil || len(inventory) != 0 {
		t.Fatal("synthetic purge inventory", err)
	}
	foreign := candidate
	foreign.OrganizationID = "00000000-0000-4000-8000-000000000099"
	if err := r.FinalizeProjectPurge(ctx, foreign, inventory); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign history purge: %v", err)
	}
	if err := r.FinalizeProjectPurge(ctx, candidate, inventory); err != nil {
		t.Fatal("canonical risk history purge", err)
	}
	if err := r.FinalizeProjectPurge(ctx, candidate, inventory); err != nil {
		t.Fatal("purge replay", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.image_vulnerability_reports").Scan(&remaining); err != nil || remaining != organizationReports {
		t.Fatal("project purge deleted organization history or retained project history", err)
	}
}
