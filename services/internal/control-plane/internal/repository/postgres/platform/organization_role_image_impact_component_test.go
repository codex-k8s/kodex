package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/organization_image_impact_target_readback.sql
var queryOrganizationImageImpactTargetReadback string

//go:embed testdata/sql/organization_image_impact_receipt_count.sql
var queryOrganizationImageImpactReceiptCount string

func testOrganizationRoleImageImpactRevokedOwner(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner value.Principal, current scope, recipeRef string) entity.RoleImageImpactPlan {
	t.Helper()
	var configurationRef string
	if err := r.pool.QueryRow(ctx, queryRoleImageManagedConfiguration, current.organizationID, recipeRef).Scan(&configurationRef); err != nil {
		t.Fatalf("organization managed image fixture: %v", err)
	}
	configuration, revisions, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, configurationRef, query.Page{Size: 20})
	if err != nil {
		t.Fatal(err)
	}
	revisionRef := ""
	for _, revision := range revisions {
		if revision.State == "PUBLISHED" {
			revisionRef = revision.Ref
			break
		}
	}
	if revisionRef == "" {
		t.Fatal("organization promoted managed revision fixture is missing")
	}
	prepare := command.Command{Kind: command.PrepareRoleImageImpactPlan, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "org-image-impact-prepare", ExpectedVersion: &configuration.Version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: configurationRef, RevisionRef: revisionRef}}
	prepared, err := service.Execute(ctx, prepare)
	if err != nil || prepared.RoleImageImpactPlan == nil || prepared.RoleImageImpactPlan.Total != 0 {
		rows, readErr := r.pool.Query(ctx, queryOrganizationImageImpactTargetReadback, current.organizationID, configurationRef,
			r.roleImages.PolicyRevision, r.roleImages.PolicySHA256, r.roleImages.RoleRuntimeContractRevision, r.roleImages.RoleRuntimeContractSHA256)
		if readErr == nil {
			for rows.Next() {
				var state, recipeState, kind string
				var published, admitted, policy, contract bool
				var builds, artifacts int
				if err := rows.Scan(&state, &published, &recipeState, &kind, &builds, &artifacts, &admitted, &policy, &contract); err == nil {
					t.Logf("synthetic image target: revision=%s published=%v recipe=%s scope=%s builds=%d artifacts=%d promoted=%v policy=%v contract=%v", state, published, recipeState, kind, builds, artifacts, admitted, policy, contract)
				}
			}
			rows.Close()
		}
		t.Fatalf("organization zero-item image impact: %v", err)
	}
	plan := prepared.RoleImageImpactPlan
	payload := command.ManagedConfigurationInput{ConfigurationRef: configurationRef, RevisionRef: revisionRef, PlanRef: plan.Ref, ImpactDigest: plan.Digest}
	apply := command.Command{Kind: command.RebindRoleImage, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "org-image-impact-apply", ExpectedVersion: &configuration.Version}, Payload: payload}
	if result, err := service.Execute(ctx, apply); err != nil || result.RoleImageImpactPlan == nil || result.RoleImageImpactPlan.State != "APPLIED" {
		t.Fatalf("organization zero-item image impact receipt: %v", err)
	}
	var before, after int
	count := func() int {
		t.Helper()
		var count int
		if err := r.pool.QueryRow(ctx, queryOrganizationImageImpactReceiptCount, current.organizationID, current.actorID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	before = count()
	if _, err := r.pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "MEMBER"); err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{plan.Digest, strings.Repeat("0", 64)} {
		payload.ImpactDigest = digest
		apply.Payload = payload
		if _, err := service.Execute(ctx, apply); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("revoked image owner gate must precede digest and receipt: %v", err)
		}
	}
	if _, err := service.Execute(ctx, prepare); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("revoked image owner replayed a zero-item plan: %v", err)
	}
	after = count()
	if after != before {
		t.Fatal("rejected image owner commands changed idempotency receipts")
	}
	if _, err := r.pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "OWNER"); err != nil {
		t.Fatal(err)
	}
	apply.Payload = command.ManagedConfigurationInput{ConfigurationRef: configurationRef, RevisionRef: revisionRef, PlanRef: plan.Ref, ImpactDigest: plan.Digest}
	if _, err := service.Execute(ctx, apply); err != nil {
		t.Fatalf("restored organization owner lost canonical receipt replay: %v", err)
	}
	return *plan
}

func testOrganizationRoleImageImpactConsumers(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner value.Principal, current scope, recipeRef string, old entity.ImageArtifact) {
	t.Helper()
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "org-image-consumer-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("organization image consumer %s: %v", key, err)
		}
		return result
	}
	create := func(key string) entity.RuntimeEnvironmentSet {
		t.Helper()
		draft := invoke(command.CreateOrganizationRuntimeEnvironmentDraft, key+"-create", nil, command.RuntimeEnvironmentDraftInput{Specification: entity.RuntimeEnvironmentDraftSpecification{Name: key, ImageArtifactRef: old.Ref, Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy()}}).RuntimeEnvironmentDraft
		draft = invoke(command.ValidateRuntimeEnvironmentDraft, key+"-validate", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref}).RuntimeEnvironmentDraft
		plan := invoke(command.PrepareEnvironmentDraftImpact, key+"-plan", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref}).RevisionImpactPlan
		return *invoke(command.PublishRuntimeEnvironmentDraft, key+"-publish", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, PlanRef: plan.Ref}).RuntimeEnvironment
	}
	bound, unbound := create("bound"), create("unbound")
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatalf("organization image assistant fixture: %v", err)
	}
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistant.Ref)
	if err != nil {
		t.Fatalf("organization image assistant environment fixture: %v", err)
	}
	invoke(command.BindAgentRuntimeEnvironment, "bind", &view.AgentVersion, command.RuntimeEnvironmentBindingInput{AgentRef: assistant.Ref, EnvironmentRef: bound.Ref, VersionRef: bound.CurrentVersion.Ref})
	detail, err := r.GetOrganization(ctx, resolved, recipeRef)
	if err != nil {
		t.Fatalf("organization image updated recipe fixture: %v", err)
	}
	version := int64(detail.Recipe.Version)
	updated, err := r.ManageOrganization(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "UPDATE", RecipeRef: recipeRef, Name: detail.Recipe.Name, Recipe: detail.Recipe.Input, Mutation: roleImageTestMutation("org-image-consumer-update", "UPDATE", &version)})
	if err != nil || updated.Build == nil {
		t.Fatalf("organization image update fixture: %v", err)
	}
	target := seedAdmittedPromotionArtifact(t, ctx, r, resolved, updated.Recipe, *updated.Build)
	version = int64(updated.Recipe.Version)
	if _, err := r.RequestOrganizationPromotion(ctx, roleimagerepo.PromotionRequestInput{Principal: resolved, Mutation: roleImageTestMutation("org-image-consumer-promote", "PROMOTE", &version), RecipeRef: recipeRef, ArtifactRef: target.Ref, ExpectedProvenanceSHA256: target.ProvenanceSHA256}); err != nil {
		t.Fatal(err)
	}
	worker := resolved
	worker.CallerWorkload, worker.Permission = "image-promotion", "platform.role-images.promotion.claim"
	claim, err := r.ClaimPromotion(ctx, worker, "org-image-consumer-claim")
	if err != nil || claim.Artifact.Ref != target.Ref {
		t.Fatalf("organization consumer promotion claim: %v", err)
	}
	worker.Permission = "platform.role-images.promotion.authorize"
	authorization, err := r.AuthorizePromotion(ctx, roleimagerepo.PromotionAuthorizeInput{Principal: worker, IdempotencyKey: "org-image-consumer-authorize", ArtifactRef: target.Ref, PromotionClaim: claim.PromotionClaim, ManifestDigest: target.ManifestDigest, ExpectedVersion: claim.Artifact.Version})
	if err != nil {
		t.Fatal(err)
	}
	worker.Permission = "platform.role-images.promotion.complete"
	if _, err := r.CompletePromotion(ctx, roleimagerepo.PromotionCompleteInput{Principal: worker, IdempotencyKey: "org-image-consumer-complete", ArtifactRef: target.Ref, AuthorizationToken: authorization.AuthorizationToken, ManifestDigest: target.ManifestDigest, ExpectedVersion: authorization.Artifact.Version, PromotedReference: r.roleImages.PromotedRepository + "@" + target.ManifestDigest, PromotionReadbackSHA256: strings.Repeat("7", 64)}); err != nil {
		t.Fatal(err)
	}
	var configurationRef string
	if err := r.pool.QueryRow(ctx, queryRoleImageManagedConfiguration, current.organizationID, recipeRef).Scan(&configurationRef); err != nil {
		t.Fatal(err)
	}
	configuration, revisions, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, configurationRef, query.Page{Size: 20})
	if err != nil || configuration.CurrentRevision == nil || configuration.SourceEditable == nil || !*configuration.SourceEditable || len(configuration.NextActions) != 0 || len(revisions) != 3 {
		t.Fatalf("organization exact source history and closed generic actions: %v", err)
	}
	plan := invoke(command.PrepareRoleImageImpactPlan, "prepare", &configuration.Version, command.ManagedConfigurationInput{ConfigurationRef: configurationRef, RevisionRef: configuration.CurrentRevision.Ref}).RoleImageImpactPlan
	page, err := service.GetRoleImageImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 20})
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("organization image impact bound and unbound consumers: %v", err)
	}
	selected := []string{}
	for _, item := range page.Items {
		if item.Consumer.ScopeKind != "ORGANIZATION" || item.Consumer.OrganizationRef != current.organizationRef || item.Consumer.ProjectRef != "" {
			t.Fatal("organization image impact owner tuple mismatch")
		}
		selected = append(selected, item.Ref)
	}
	invoke(command.RebindRoleImage, "apply", &configuration.Version, command.ManagedConfigurationInput{ConfigurationRef: configurationRef, RevisionRef: configuration.CurrentRevision.Ref, PlanRef: plan.Ref, ImpactDigest: plan.Digest, SelectedItemRefs: selected})
	final, err := service.GetRoleImageImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 20})
	if err != nil || final.Plan.State != "APPLIED" || len(final.Items) != 3 {
		t.Fatalf("organization image impact final readback: %v", err)
	}
	for _, item := range final.Items {
		if item.Outcome != "APPLIED" || item.ResultEnvironmentVersionRef == item.SourceVersionRef {
			t.Fatalf("organization image replacement outcome: %s", item.Outcome)
		}
	}
	for _, environment := range []entity.RuntimeEnvironmentSet{bound, unbound} {
		current, err := service.GetRuntimeEnvironment(ctx, owner, environment.Ref)
		if err != nil || current.ScopeKind != "ORGANIZATION" || current.CurrentVersion.Image.ArtifactRef != target.Ref || current.Version != environment.Version+1 {
			t.Fatalf("organization image replacement exact publication: %v", err)
		}
	}
	view, err = service.GetAgentRuntimeConfiguration(ctx, owner, assistant.Ref)
	if err != nil || view.Environment.CurrentVersion.Image.ArtifactRef != target.Ref {
		t.Fatalf("organization assistant image binding readback: %v", err)
	}
}
