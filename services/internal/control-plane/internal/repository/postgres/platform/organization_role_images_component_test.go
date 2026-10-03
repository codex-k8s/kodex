package platform

import (
	"context"
	_ "embed"
	"errors"
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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/organization_role_image_component_owner.sql
var queryOrganizationImageComponentOwner string

//go:embed testdata/sql/organization_role_image_component_expiry.sql
var queryOrganizationImageComponentExpiry string

//go:embed testdata/sql/organization_role_image_component_reparent.sql
var queryOrganizationImageComponentReparent string

//go:embed testdata/sql/organization_role_image_component_project.sql
var queryOrganizationImageComponentProject string

//go:embed testdata/sql/organization_role_image_component_terminal_expiry.sql
var queryOrganizationImageComponentTerminalExpiry string

// Только пустая disposable БД и синтетические build/admission callbacks.
func TestOrganizationRoleImagesComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("parse isolated fixture configuration")
	}
	config.ConnConfig.Tracer = organizationImageQueryTracer{t: t}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles",
		DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.organization.role-images.recipes.manage",
	}, "control-api-gateway")
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	manage := func(action, key string, recipe entity.RoleImageRecipe, buildRef string) roleimagerepo.ManageResult {
		t.Helper()
		var version *int64
		if action != "CREATE" {
			current := int64(recipe.Version)
			version = &current
		}
		input := roleimagerepo.ManageInput{Principal: resolved, Action: action, RecipeRef: recipe.Ref, BuildRef: buildRef,
			Mutation: roleImageTestMutation("org-image-"+key, action, version)}
		if action == "CREATE" {
			input.Name = "Organization image"
			_, input.Recipe = promotionComponentCatalog(t)
		}
		result, err := repository.ManageOrganization(ctx, input)
		if err != nil {
			t.Fatalf("%s organization image: %v", action, err)
		}
		if result.Recipe.ScopeKind != "ORGANIZATION" || result.Recipe.OrganizationRef == "" || result.Recipe.ProjectRef != "" {
			t.Fatal("organization recipe owner tuple mismatch")
		}
		return result
	}
	created := manage("CREATE", "create", entity.RoleImageRecipe{}, "")
	if created.Build == nil || created.Build.ScopeKind != "ORGANIZATION" || created.Build.ProjectRef != "" {
		t.Fatal("organization build owner tuple mismatch")
	}
	if _, err := repository.Get(ctx, resolved, created.Recipe.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("project endpoint read organization recipe: %v", err)
	}
	detail, err := repository.GetOrganization(ctx, resolved, created.Recipe.Ref)
	if err != nil || detail.Recipe.Ref != created.Recipe.Ref || len(detail.Builds) != 1 {
		t.Fatalf("organization exact read: %v", err)
	}
	items, _, total, err := repository.ListOrganization(ctx, resolved, roleimagerepo.Filter{Page: query.Page{Size: 50}})
	if err != nil || total != int64(len(items)) || total < 2 {
		t.Fatalf("organization list/count: %v", err)
	}
	if _, _, err := repository.ListOrganizationRevisions(ctx, resolved, created.Recipe.Ref, query.Page{Size: 50}); err != nil {
		t.Fatalf("organization revisions: %v", err)
	}
	worker := resolved
	worker.CallerWorkload = "role-image-builder"
	worker.Permission = "platform.role-images.builds.claim"
	claim, err := repository.ClaimBuild(ctx, worker, "org-image-claim")
	if err != nil || claim.Input.ScopeKind != "ORGANIZATION" || claim.Input.OrganizationRef != created.Recipe.OrganizationRef || claim.Input.ProjectRef != "" {
		t.Fatalf("organization immutable claim: %v", err)
	}
	renewed, err := repository.RenewBuild(ctx, roleimagerepo.BuildLeaseInput{Principal: worker, IdempotencyKey: "org-image-renew", BuildRef: claim.Build.Ref,
		LeaseToken: claim.LeaseToken, ExpectedVersion: claim.Build.Version, ExpectedAttempt: claim.Build.Attempt, ExpectedFence: claim.Build.Fence})
	if err != nil || renewed.Input.ScopeKind != "ORGANIZATION" || renewed.Input.OrganizationRef != claim.Input.OrganizationRef {
		t.Fatalf("organization renewal owner tuple: %v", err)
	}
	if _, err := pool.Exec(ctx, queryOrganizationImageComponentExpiry, claim.Build.Ref); err != nil {
		t.Fatal(err)
	}
	expiredRetry, err := repository.ClaimBuild(ctx, worker, "org-image-expired-claim")
	if err != nil || expiredRetry.Build.Attempt != claim.Build.Attempt+1 || expiredRetry.Build.Fence <= claim.Build.Fence || expiredRetry.Input.OrganizationRef != claim.Input.OrganizationRef {
		t.Fatalf("expired organization lease did not create fenced attempt: %v", err)
	}
	if _, err := repository.RenewBuild(ctx, roleimagerepo.BuildLeaseInput{Principal: worker, IdempotencyKey: "org-image-stale-renew", BuildRef: claim.Build.Ref,
		LeaseToken: renewed.LeaseToken, ExpectedVersion: renewed.Build.Version, ExpectedAttempt: renewed.Build.Attempt, ExpectedFence: renewed.Build.Fence}); err == nil {
		t.Fatal("stale organization build lease survived retry")
	}
	cancelled := manage("CANCEL_BUILD", "cancel", created.Recipe, created.Build.Ref)
	if cancelled.Build == nil || cancelled.Build.Stage != "CANCELLED" {
		t.Fatal("organization cancellation did not close exact build")
	}
	retry := manage("REQUEST_BUILD", "retry", cancelled.Recipe, "")
	if retry.Build == nil || retry.Build.Ref == created.Build.Ref {
		t.Fatal("organization retry reused cancelled build")
	}
	archived := manage("ARCHIVE", "archive", retry.Recipe, "")
	if archived.Recipe.State != "ARCHIVED" {
		t.Fatal("organization recipe did not archive")
	}
	restored := manage("RESTORE", "restore", archived.Recipe, "")
	if restored.Recipe.State != "ACTIVE" {
		t.Fatal("organization recipe did not restore")
	}
	claim, err = repository.ClaimBuild(ctx, worker, "org-image-restored-claim")
	if err != nil {
		t.Fatalf("claim restored build: %v", err)
	}
	manifest := "sha256:" + strings.Repeat("8", 64)
	_, artifact, err := repository.CompleteBuild(ctx, roleimagerepo.BuildCompletionInput{
		BuildLeaseInput: roleimagerepo.BuildLeaseInput{Principal: worker, IdempotencyKey: "org-image-complete", BuildRef: claim.Build.Ref,
			LeaseToken: claim.LeaseToken, ExpectedVersion: claim.Build.Version, ExpectedAttempt: claim.Build.Attempt, ExpectedFence: claim.Build.Fence},
		StagingReference: repository.roleImages.StagingRepository + "@" + manifest, ManifestDigest: manifest,
		ProvenanceSHA256: strings.Repeat("9", 64), ImmutableBuildSHA256: claim.Input.ImmutableBuildSHA256,
	})
	if err != nil || artifact.ScopeKind != "ORGANIZATION" {
		t.Fatalf("complete organization build: %v", err)
	}
	admissionWorker := worker
	admissionWorker.CallerWorkload = "image-admission"
	admissionWorker.Permission = "platform.role-images.admission.claim"
	admission, err := repository.ClaimAdmission(ctx, admissionWorker, "org-image-admission")
	if err != nil || admission.Artifact.Ref != artifact.Ref || admission.Artifact.OrganizationRef != artifact.OrganizationRef {
		t.Fatalf("claim organization admission: %v", err)
	}
	admitted, err := repository.RecordAdmission(ctx, roleimagerepo.AdmissionRecordInput{
		Principal: admissionWorker, IdempotencyKey: "org-image-admit", ArtifactRef: artifact.Ref, ClaimToken: admission.ClaimToken,
		ExpectedVersion: admission.Artifact.Version, ExpectedFence: admission.Fence, ManifestDigest: manifest,
		ImmutableBuildSHA256: artifact.ImmutableBuildSHA256, ProvenanceSHA256: artifact.ProvenanceSHA256,
		PolicyRevision: repository.roleImages.PolicyRevision, PolicySHA256: repository.roleImages.PolicySHA256, Verdict: "ACCEPTED",
		SBOMSHA256: strings.Repeat("1", 64), VulnerabilityEvidenceSHA256: strings.Repeat("2", 64), SignatureIdentity: "synthetic-owner",
		SignatureSHA256: strings.Repeat("3", 64), AdmissionReceiptSHA256: strings.Repeat("4", 64), AdmissionReceiptOCIManifestDigest: "sha256:" + strings.Repeat("5", 64),
	})
	if err != nil || admitted.ScopeKind != "ORGANIZATION" {
		t.Fatalf("admit organization artifact: %v", err)
	}
	detail, err = repository.GetOrganization(ctx, resolved, created.Recipe.Ref)
	if err != nil {
		t.Fatal(err)
	}
	version := int64(detail.Recipe.Version)
	promotionInput := roleimagerepo.PromotionRequestInput{Principal: resolved, Mutation: roleImageTestMutation("org-image-promote", "PROMOTE", &version),
		RecipeRef: created.Recipe.Ref, ArtifactRef: artifact.Ref, ExpectedProvenanceSHA256: artifact.ProvenanceSHA256}
	if _, err := repository.RequestPromotion(ctx, promotionInput); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("project promotion endpoint accepted organization image: %v", err)
	}
	if _, err := repository.RequestOrganizationPromotion(ctx, promotionInput); err != nil {
		t.Fatalf("request organization promotion: %v", err)
	}
	promotionWorker := worker
	promotionWorker.CallerWorkload = "image-promotion"
	promotionWorker.Permission = "platform.role-images.promotion.claim"
	promotion, err := repository.ClaimPromotion(ctx, promotionWorker, "org-image-promote-claim")
	if err != nil || promotion.Artifact.Ref != artifact.Ref {
		t.Fatalf("claim organization promotion: %v", err)
	}
	authorized, err := repository.AuthorizePromotion(ctx, roleimagerepo.PromotionAuthorizeInput{Principal: promotionWorker, IdempotencyKey: "org-image-authorize",
		ArtifactRef: artifact.Ref, PromotionClaim: promotion.PromotionClaim, ManifestDigest: manifest, ExpectedVersion: promotion.Artifact.Version})
	if err != nil {
		t.Fatalf("authorize organization promotion: %v", err)
	}
	promoted, err := repository.CompletePromotion(ctx, roleimagerepo.PromotionCompleteInput{Principal: promotionWorker, IdempotencyKey: "org-image-promoted",
		ArtifactRef: artifact.Ref, AuthorizationToken: authorized.AuthorizationToken, ManifestDigest: manifest, ExpectedVersion: authorized.Artifact.Version,
		PromotedReference: repository.roleImages.PromotedRepository + "@" + manifest, PromotionReadbackSHA256: strings.Repeat("6", 64)})
	if err != nil || promoted.PromotionState != "PROMOTED" || promoted.ScopeKind != "ORGANIZATION" {
		t.Fatalf("complete organization promotion: %v", err)
	}
	revisions, _, err := repository.ListOrganizationRevisions(ctx, resolved, created.Recipe.Ref, query.Page{Size: 50})
	if err != nil || len(revisions) != 1 {
		t.Fatalf("organization promoted revision readback: %v", err)
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "MEMBER"); err != nil {
		t.Fatal(err)
	}
	_, memberRead := repository.GetOrganization(ctx, resolved, created.Recipe.Ref)
	_, _, _, memberList := repository.ListOrganization(ctx, resolved, roleimagerepo.Filter{Page: query.Page{Size: 10}})
	if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "OWNER"); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(memberRead, errs.ErrNotFound) || !errors.Is(memberList, errs.ErrNotFound) {
		t.Fatal("ordinary member accessed organization image")
	}
	platform, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := platform.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "org-image-project"}, Payload: command.ProjectInput{Name: "Image owner boundary", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create signed project fixture: %v", err)
	}
	signed := resolved
	if err := pool.QueryRow(ctx, queryOrganizationImageComponentProject, current.organizationID, project.Project.Ref).Scan(&signed.ProjectRef); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetOrganization(ctx, signed, created.Recipe.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("signed project principal accessed organization image: %v", err)
	}
	if _, err := repository.RequestOrganizationPromotion(ctx, roleimagerepo.PromotionRequestInput{Principal: signed, Mutation: promotionInput.Mutation,
		RecipeRef: created.Recipe.Ref, ArtifactRef: artifact.Ref, ExpectedProvenanceSHA256: artifact.ProvenanceSHA256}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("signed project replayed organization promotion receipt: %v", err)
	}
	version = int64(detail.Recipe.Version)
	if _, err := platform.Execute(ctx, command.Command{Kind: command.CopyRoleImageConfiguration, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "org-image-generic-copy", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.Recipe.ManagedLineage.ConfigurationRef, ProjectRef: project.Project.Ref, Name: "Forbidden generic copy"}}); err == nil {
		t.Fatal("generic project lifecycle copied organization image configuration")
	}
	var foreignRecipe string
	for _, item := range items {
		if item.Ref != created.Recipe.Ref {
			foreignRecipe = item.Ref
			break
		}
	}
	if foreignRecipe == "" {
		t.Fatal("immutable owner fixture has no foreign parent")
	}
	if _, err := pool.Exec(ctx, queryOrganizationImageComponentReparent, claim.Build.Ref, foreignRecipe); err == nil {
		t.Fatal("image owner tuple was mutable")
	}
	_, recipeInput := promotionComponentCatalog(t)
	terminal, err := repository.ManageOrganization(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "CREATE", Name: "Terminal expiry image", Recipe: recipeInput,
		Mutation: roleImageTestMutation("org-image-terminal", "CREATE", nil)})
	if err != nil {
		t.Fatal(err)
	}
	terminalClaim, err := repository.ClaimBuild(ctx, worker, "org-image-terminal-claim")
	if err != nil || terminalClaim.Build.Ref != terminal.Build.Ref {
		t.Fatalf("claim terminal expiry fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, queryOrganizationImageComponentTerminalExpiry, terminalClaim.Build.Ref); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ClaimBuild(ctx, worker, "org-image-dead-letter-claim"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("exhausted organization build was claimed: %v", err)
	}
	terminalDetail, err := repository.GetOrganization(ctx, resolved, terminal.Recipe.Ref)
	if err != nil || len(terminalDetail.Builds) != 1 || terminalDetail.Builds[0].Stage != "DEAD_LETTER" || terminalDetail.Builds[0].LeaseExpiresAt != nil || terminalDetail.Builds[0].AuthorityGeneration != 0 || terminalDetail.Builds[0].LeaseTokenSHA256 != "" {
		t.Fatalf("terminal expiry closure was rolled back without next claim: %v", err)
	}
}

type organizationImageQueryTracer struct{ t *testing.T }

func (tracer organizationImageQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}
func (tracer organizationImageQueryTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, result pgx.TraceQueryEndData) {
	var failure *pgconn.PgError
	if errors.As(result.Err, &failure) {
		tracer.t.Logf("synthetic SQL rejection: code=%s constraint=%s message=%s", failure.Code, failure.ConstraintName, failure.Message)
	}
}
