package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testRoleImageImpactLifecycle(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, images *roleimageservice.Service, owner, resolved value.Principal, recipe entity.RoleImageRecipe, old entity.ImageArtifact, agent entity.Agent) {
	t.Helper()
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "role-impact-environment"}, Payload: command.RuntimeEnvironmentInput{ProjectRef: recipe.ProjectRef, Name: "Role impact", ImageArtifactRef: old.Ref, Values: []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "retained"}}}})
	if err != nil || created.RuntimeEnvironment == nil {
		t.Fatalf("create role impact environment: %v", err)
	}
	environment := *created.RuntimeEnvironment
	bound, err := service.Execute(ctx, command.Command{Kind: command.BindAgentRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "role-impact-bind", ExpectedVersion: &agent.Version}, Payload: command.RuntimeEnvironmentBindingInput{AgentRef: agent.Ref, EnvironmentRef: environment.Ref, VersionRef: environment.CurrentVersion.Ref}})
	if err != nil || bound.RuntimeConfiguration == nil {
		t.Fatalf("bind role impact environment: %v", err)
	}
	oldBinding := bound.RuntimeConfiguration.EnvironmentBinding
	unselectedAgent := createLifecycleAgent(t, ctx, service, owner, recipe.ProjectRef, "role-impact-unselected-agent", "Unselected image consumer")
	if _, err = service.Execute(ctx, command.Command{Kind: command.BindAgentRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "role-impact-bind-unselected", ExpectedVersion: &unselectedAgent.Version}, Payload: command.RuntimeEnvironmentBindingInput{AgentRef: unselectedAgent.Ref, EnvironmentRef: environment.Ref, VersionRef: environment.CurrentVersion.Ref}}); err != nil {
		t.Fatal(err)
	}
	version := int64(recipe.Version)
	updated, err := r.Manage(ctx, roleimagerepo.ManageInput{Principal: resolved, Action: "UPDATE", ProjectRef: recipe.ProjectRef, RecipeRef: recipe.Ref, RoleDefinitionRef: agent.RoleDefinitionRef, Name: recipe.Name, Recipe: recipe.Input, Mutation: roleImageTestMutation("role-impact-recipe-update", "UPDATE", &version)})
	if err != nil || updated.Build == nil {
		t.Fatalf("update role impact recipe: %v", err)
	}
	target := seedAdmittedPromotionArtifact(t, ctx, r, resolved, updated.Recipe, *updated.Build)
	version = int64(updated.Recipe.Version)
	if _, err = images.Promote(ctx, roleimagerepo.PromotionRequestInput{Principal: owner, Mutation: value.Mutation{IdempotencyKey: "role-impact-promote", ExpectedVersion: &version}, RecipeRef: recipe.Ref, ArtifactRef: target.Ref, ExpectedProvenanceSHA256: target.ProvenanceSHA256}); err != nil {
		t.Fatal(err)
	}
	worker := owner
	worker.CallerWorkload = "image-promotion"
	worker.Permission = "platform.role-images.promotion.claim"
	claim, err := images.ClaimPromotion(ctx, worker, "role-impact-claim")
	if err != nil {
		t.Fatal(err)
	}
	worker.Permission = "platform.role-images.promotion.authorize"
	authorization, err := images.AuthorizePromotion(ctx, roleimagerepo.PromotionAuthorizeInput{Principal: worker, IdempotencyKey: "role-impact-authorize", ArtifactRef: target.Ref, PromotionClaim: claim.PromotionClaim, ManifestDigest: target.ManifestDigest, ExpectedVersion: claim.Artifact.Version})
	if err != nil {
		t.Fatal(err)
	}
	worker.Permission = "platform.role-images.promotion.complete"
	if _, err = images.CompletePromotion(ctx, roleimagerepo.PromotionCompleteInput{Principal: worker, IdempotencyKey: "role-impact-complete", ArtifactRef: target.Ref, AuthorizationToken: authorization.AuthorizationToken, PromotedReference: r.roleImages.PromotedRepository + "@" + target.ManifestDigest, ManifestDigest: target.ManifestDigest, PromotionReadbackSHA256: strings.Repeat("8", 64), ExpectedVersion: authorization.Artifact.Version}); err != nil {
		t.Fatal(err)
	}
	s, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	var configurationRef string
	if r.pool.QueryRow(ctx, queryRoleImageManagedConfiguration, s.organizationID, recipe.Ref).Scan(&configurationRef) != nil {
		t.Fatal("managed role configuration missing")
	}
	configuration, revisions, _, _, err := service.ListManagedConfigurationHistory(ctx, owner, configurationRef, query.Page{Size: 20})
	if err != nil {
		t.Fatal(err)
	}
	var revisionRef string
	for _, revision := range revisions {
		if revision.State == "PUBLISHED" {
			revisionRef = revision.Ref
			break
		}
	}
	prepare := command.Command{Kind: command.PrepareRoleImageImpactPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "role-impact-prepare", ExpectedVersion: &configuration.Version}, Payload: command.ManagedConfigurationInput{ConfigurationRef: configurationRef, RevisionRef: revisionRef}}
	prepared, err := service.Execute(ctx, prepare)
	if err != nil || prepared.RoleImageImpactPlan == nil {
		t.Fatalf("prepare actual role impact: %v", err)
	}
	plan := *prepared.RoleImageImpactPlan
	if plan.ArtifactRef != target.Ref || plan.Total != 3 || plan.State != "PREPARED" {
		t.Fatalf("incorrect role impact plan: %+v", plan)
	}
	replay, err := service.Execute(ctx, prepare)
	if err != nil || !reflect.DeepEqual(replay.RoleImageImpactPlan, prepared.RoleImageImpactPlan) {
		t.Fatalf("prepare replay: %v", err)
	}
	page, err := service.GetRoleImageImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 1})
	if err != nil || page.Total != 3 || len(page.Items) != 1 || page.NextPageToken == "" {
		t.Fatalf("role impact pagination: %v %+v", err, page)
	}
	if _, err = service.GetRoleImageImpactPlan(ctx, owner, plan.Ref, "foreign", query.Page{Size: 1, Token: page.NextPageToken}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("role impact cursor filter changed: %v", err)
	}
	all, err := service.GetRoleImageImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 20})
	if err != nil {
		t.Fatal(err)
	}
	byName, err := service.GetRoleImageImpactPlan(ctx, owner, plan.Ref, "Unselected image consumer", query.Page{Size: 20})
	if err != nil || byName.Total != 1 || len(byName.Items) != 1 || byName.Items[0].Consumer.AgentRef != unselectedAgent.Ref {
		t.Fatalf("role image impact name search: %v", err)
	}
	expired := plan
	expired.Ref, err = newRef("riip")
	if err != nil {
		t.Fatal(err)
	}
	expired.Digest, err = roleImageImpactDigest(expired, s.actorID, all.Items)
	if err != nil {
		t.Fatal(err)
	}
	var expiredID string
	if err = r.pool.QueryRow(ctx, `INSERT INTO control_plane.role_image_impact_plans
 (ref,organization_id,actor_id,configuration_id,revision_id,artifact_id,snapshot,digest,created_at,expires_at)
 SELECT $2,organization_id,actor_id,configuration_id,revision_id,artifact_id,$3::jsonb,$4,clock_timestamp()-interval '16 minutes',clock_timestamp()-interval '1 minute'
 FROM control_plane.role_image_impact_plans WHERE ref=$1 RETURNING id::text`, plan.Ref, expired.Ref, string(asJSON(expired)), expired.Digest).Scan(&expiredID); err != nil {
		t.Fatal(err)
	}
	if _, err = r.pool.Exec(ctx, `INSERT INTO control_plane.role_image_impact_items(plan_id,ref,snapshot)
 SELECT $2::uuid,item.ref,item.snapshot FROM control_plane.role_image_impact_items item
 JOIN control_plane.role_image_impact_plans plan ON plan.id=item.plan_id WHERE plan.ref=$1`, plan.Ref, expiredID); err != nil {
		t.Fatal(err)
	}
	expiredRead, err := service.GetRoleImageImpactPlan(ctx, owner, expired.Ref, "", query.Page{Size: 10})
	if err != nil || expiredRead.Plan.State != "EXPIRED" {
		t.Fatalf("expired plan tombstone: %v", err)
	}
	if _, err = service.Execute(ctx, command.Command{Kind: command.RebindRoleImage, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "role-impact-expired", ExpectedVersion: &configuration.Version}, Payload: command.ManagedConfigurationInput{ConfigurationRef: configurationRef, RevisionRef: revisionRef, PlanRef: expired.Ref, ImpactDigest: expired.Digest, SelectedItemRefs: []string{all.Items[0].Ref}}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("expired plan effect accepted: %v", err)
	}
	if _, err = r.pool.Exec(ctx, `UPDATE control_plane.role_image_impact_plans SET digest=repeat('0',64) WHERE ref=$1`, plan.Ref); err == nil {
		t.Fatal("immutable plan digest changed")
	}
	if _, err = r.pool.Exec(ctx, `UPDATE control_plane.role_image_impact_items SET snapshot='{}'::jsonb WHERE plan_id=(SELECT id FROM control_plane.role_image_impact_plans WHERE ref=$1)`, plan.Ref); err == nil {
		t.Fatal("immutable item snapshot changed")
	}
	selected := []string{}
	for _, item := range all.Items {
		if item.Consumer.AgentRef == unselectedAgent.Ref {
			continue
		}
		selected = append(selected, item.Ref)
	}
	apply := command.Command{Kind: command.RebindRoleImage, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "role-impact-apply", ExpectedVersion: &configuration.Version}, Payload: command.ManagedConfigurationInput{ConfigurationRef: configurationRef, RevisionRef: revisionRef, ImpactDigest: plan.Digest, PlanRef: plan.Ref, SelectedItemRefs: selected}}
	applied, err := service.Execute(ctx, apply)
	if err != nil || applied.RoleImageImpactPlan == nil || applied.RoleImageImpactPlan.State != "APPLIED" {
		t.Fatalf("apply role impact: %v %+v", err, applied.RoleImageImpactPlan)
	}
	replayed, err := service.Execute(ctx, apply)
	if err != nil || !reflect.DeepEqual(replayed.RoleImageImpactPlan, applied.RoleImageImpactPlan) {
		t.Fatalf("apply replay: %v", err)
	}
	final, err := service.GetRoleImageImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 20})
	if err != nil {
		t.Fatal(err)
	}
	newRef := ""
	for _, item := range final.Items {
		if item.Consumer.AgentRef == unselectedAgent.Ref {
			if item.Outcome != "NOT_SELECTED" || item.ResultEnvironmentVersionRef != "" {
				t.Fatal("unselected consumer changed")
			}
			continue
		}
		if item.Outcome != "APPLIED" || item.ResultEnvironmentVersionRef == environment.CurrentVersion.Ref {
			t.Fatalf("missing real environment effect: %+v", item)
		}
		if newRef == "" {
			newRef = item.ResultEnvironmentVersionRef
		} else if newRef != item.ResultEnvironmentVersionRef {
			t.Fatal("same environment cloned twice")
		}
		if item.Consumer.AgentRef != "" && (item.ResultBindingRef != oldBinding.Ref || item.ResultBindingVersion <= oldBinding.Version) {
			t.Fatal("binding receipt changed identity")
		}
	}
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil || view.EnvironmentBinding.VersionRef != newRef || view.Environment.CurrentVersion.Image.ArtifactRef != target.Ref || !reflect.DeepEqual(view.Environment.CurrentVersion.Values, environment.CurrentVersion.Values) {
		t.Fatalf("actual image rebind readback: %v", err)
	}
	versions, _, err := service.ListRuntimeEnvironmentVersions(ctx, owner, query.Filter{ResourceRef: environment.Ref, Page: query.Page{Size: 20}})
	if err != nil || len(versions) != 2 {
		t.Fatalf("environment replay created extra revision: count=%d err=%v", len(versions), err)
	}
	prepare.Mutation = value.Mutation{IdempotencyKey: "role-impact-stale-plan", ExpectedVersion: &applied.ManagedConfiguration.Version}
	stalePlan, err := service.Execute(ctx, prepare)
	if err != nil || stalePlan.RoleImageImpactPlan.Total != 1 {
		t.Fatalf("retained old binding missing from plan: %v", err)
	}
	staleItems, err := service.GetRoleImageImpactPlan(ctx, owner, stalePlan.RoleImageImpactPlan.Ref, "", query.Page{Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	currentAgent, err := service.GetAgent(ctx, owner, unselectedAgent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Execute(ctx, command.Command{Kind: command.BindAgentRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "role-impact-concurrent-binding", ExpectedVersion: &currentAgent.Version}, Payload: command.RuntimeEnvironmentBindingInput{AgentRef: unselectedAgent.Ref, EnvironmentRef: environment.Ref, VersionRef: environment.CurrentVersion.Ref}}); err != nil {
		t.Fatal(err)
	}
	staleApply := apply
	staleApply.Mutation = value.Mutation{IdempotencyKey: "role-impact-stale-apply", ExpectedVersion: &applied.ManagedConfiguration.Version}
	staleApply.Payload = command.ManagedConfigurationInput{ConfigurationRef: configurationRef, RevisionRef: revisionRef, PlanRef: stalePlan.RoleImageImpactPlan.Ref, ImpactDigest: stalePlan.RoleImageImpactPlan.Digest, SelectedItemRefs: []string{staleItems.Items[0].Ref}}
	if _, err = service.Execute(ctx, staleApply); err != nil {
		t.Fatalf("per-item conflict rejected whole receipt: %v", err)
	}
	conflicted, err := service.GetRoleImageImpactPlan(ctx, owner, stalePlan.RoleImageImpactPlan.Ref, "", query.Page{Size: 10})
	if err != nil || conflicted.Items[0].Outcome != "CONFLICT" || conflicted.Items[0].ResultEnvironmentVersionRef != "" {
		t.Fatalf("stale binding result: %v %+v", err, conflicted)
	}
	versions, _, err = service.ListRuntimeEnvironmentVersions(ctx, owner, query.Filter{ResourceRef: environment.Ref, Page: query.Page{Size: 20}})
	if err != nil || len(versions) != 2 {
		t.Fatal("failed-only group left an unreceipted environment revision")
	}
	effective, err := service.GetEffectiveManagedConfiguration(ctx, owner, "ROLE_IMAGE", "RUNTIME_ENVIRONMENT", environment.Ref)
	if err != nil || effective.Revision.Ref != revisionRef || effective.Configuration.Ref != configurationRef {
		t.Fatalf("actual role lineage missing: %v", err)
	}
	outsider := owner
	outsider.AuthorityTenant = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	if _, err = service.GetRoleImageImpactPlan(ctx, outsider, plan.Ref, "", query.Page{Size: 10}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("foreign plan read: %v", err)
	}
	foreign := apply
	foreign.Principal = outsider
	if _, err = service.Execute(ctx, foreign); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("foreign replay resolved old receipt: %v", err)
	}
	policy := r.roleImages.PolicySHA256
	r.roleImages.PolicySHA256 = strings.Repeat("0", 64)
	_, replayErr := service.Execute(ctx, apply)
	r.roleImages.PolicySHA256 = policy
	if !errors.Is(replayErr, errs.ErrConflict) {
		t.Fatalf("stale admission policy replay accepted: %v", replayErr)
	}
}
