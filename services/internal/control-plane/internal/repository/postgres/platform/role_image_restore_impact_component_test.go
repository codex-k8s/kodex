package platform

import (
	"context"
	"strings"
	"testing"

	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Восстановленная generation проходит тот же admission/promotion и impact path.
func testRestoredProjectRoleImageImpact(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner, resolved value.Principal, parent entity.RoleImageRecipe) {
	t.Helper()
	_, input := promotionComponentCatalog(t)
	created, err := r.Manage(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "CREATE", ProjectRef: parent.ProjectRef,
		RoleDefinitionRef: parent.RoleDefinitionRef, Name: "Restored promotion source", Recipe: input, Mutation: roleImageTestMutation("restored-project-create", "CREATE", nil)})
	if err != nil || created.Build == nil {
		t.Fatalf("restored project canonical recipe fixture: %v", err)
	}
	version := int64(created.Recipe.Version)
	archived, err := r.Manage(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "ARCHIVE", ProjectRef: parent.ProjectRef,
		RecipeRef: created.Recipe.Ref, Mutation: roleImageTestMutation("restored-project-archive", "ARCHIVE", &version)})
	if err != nil {
		t.Fatal(err)
	}
	version = int64(archived.Recipe.Version)
	restore := roleimagerepo.ManageInput{Principal: resolved, Action: "RESTORE", ProjectRef: parent.ProjectRef,
		RecipeRef: created.Recipe.Ref, Mutation: roleImageTestMutation("restored-project-restore", "RESTORE", &version)}
	restored, err := r.Manage(ctx, restore)
	if err != nil || restored.Build == nil || restored.Build.ConfigurationRevisionRef == "" || restored.Build.ConfigurationRevisionRef == created.Build.ConfigurationRevisionRef {
		t.Fatalf("restored project fresh generation source: %v", err)
	}
	recipe, build := restored.Recipe, *restored.Build
	assertManagedRoleImageBuild(t, ctx, r, recipe, build)
	replayed, err := r.Manage(ctx, restore)
	if err != nil || replayed.Build == nil || replayed.Recipe.Generation != recipe.Generation || replayed.Build.ConfigurationRevisionRef != build.ConfigurationRevisionRef {
		t.Fatalf("restored project exact generation replay: %v", err)
	}
	artifact := seedAdmittedPromotionArtifact(t, ctx, r, resolved, recipe, build)
	detail, err := r.Get(ctx, resolved, recipe.Ref)
	if err != nil {
		t.Fatal(err)
	}
	version = int64(detail.Recipe.Version)
	if _, err := r.RequestPromotion(ctx, roleimagerepo.PromotionRequestInput{Principal: resolved, Mutation: roleImageTestMutation("restored-project-promote", "PROMOTE", &version), RecipeRef: recipe.Ref, ArtifactRef: artifact.Ref, ExpectedProvenanceSHA256: artifact.ProvenanceSHA256}); err != nil {
		t.Fatalf("restored project promotion request: %v", err)
	}
	worker := resolved
	worker.CallerWorkload, worker.Permission = "image-promotion", "platform.role-images.promotion.claim"
	claim, err := r.ClaimPromotion(ctx, worker, "restored-project-claim")
	if err != nil || claim.Artifact.Ref != artifact.Ref {
		t.Fatalf("restored project promotion claim: %v", err)
	}
	worker.Permission = "platform.role-images.promotion.authorize"
	authorization, err := r.AuthorizePromotion(ctx, roleimagerepo.PromotionAuthorizeInput{Principal: worker, IdempotencyKey: "restored-project-authorize", ArtifactRef: artifact.Ref, PromotionClaim: claim.PromotionClaim, ManifestDigest: artifact.ManifestDigest, ExpectedVersion: claim.Artifact.Version})
	if err != nil {
		t.Fatal(err)
	}
	worker.Permission = "platform.role-images.promotion.complete"
	if _, err := r.CompletePromotion(ctx, roleimagerepo.PromotionCompleteInput{Principal: worker, IdempotencyKey: "restored-project-complete", ArtifactRef: artifact.Ref, AuthorizationToken: authorization.AuthorizationToken, ManifestDigest: artifact.ManifestDigest, ExpectedVersion: authorization.Artifact.Version, PromotedReference: r.roleImages.PromotedRepository + "@" + artifact.ManifestDigest, PromotionReadbackSHA256: strings.Repeat("7", 64)}); err != nil {
		t.Fatal(err)
	}
	configuration, _, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, recipe.ManagedLineage.ConfigurationRef, query.Page{Size: 20})
	if err != nil || configuration.CurrentRevision == nil || configuration.CurrentRevision.Ref != build.ConfigurationRevisionRef {
		t.Fatalf("restored project exact managed history: %v", err)
	}
	payload := command.ManagedConfigurationInput{ConfigurationRef: configuration.Ref, RevisionRef: build.ConfigurationRevisionRef}
	prepared, err := service.Execute(ctx, command.Command{Kind: command.PrepareRoleImageImpactPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "restored-project-impact-prepare", ExpectedVersion: &configuration.Version}, Payload: payload})
	if err != nil || prepared.RoleImageImpactPlan == nil || prepared.RoleImageImpactPlan.ArtifactRef != artifact.Ref || uint64(prepared.RoleImageImpactPlan.RecipeGeneration) != recipe.Generation || prepared.RoleImageImpactPlan.Total != 0 {
		t.Fatalf("restored project exact promoted impact target: %v", err)
	}
	payload.PlanRef, payload.ImpactDigest = prepared.RoleImageImpactPlan.Ref, prepared.RoleImageImpactPlan.Digest
	apply := command.Command{Kind: command.RebindRoleImage, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "restored-project-impact-apply", ExpectedVersion: &configuration.Version}, Payload: payload}
	for range 2 {
		applied, err := service.Execute(ctx, apply)
		if err != nil || applied.RoleImageImpactPlan == nil || applied.RoleImageImpactPlan.State != "APPLIED" || applied.ManagedRevision == nil || applied.ManagedRevision.Ref != build.ConfigurationRevisionRef {
			t.Fatalf("restored project impact apply and replay: %v", err)
		}
	}
}
