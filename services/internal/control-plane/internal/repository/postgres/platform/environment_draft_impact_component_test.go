package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testEnvironmentPrepublicationImpact(t *testing.T, ctx context.Context, r *Repository, s *platformservice.Service, owner value.Principal, project, environment string, spec entity.RuntimeEnvironmentDraftSpecification, first, second string) {
	invoke := func(kind command.Kind, key string, version *int64, payload command.RuntimeEnvironmentDraftInput) (command.Result, error) {
		return s.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: version}, Payload: payload})
	}
	current, err := s.GetRuntimeEnvironment(ctx, owner, environment)
	if err != nil {
		t.Fatal(err)
	}
	spec.Values = []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "prepublish-plan"}}
	created, err := invoke(command.CreateRuntimeEnvironmentDraft, "preimpact-create", nil, command.RuntimeEnvironmentDraftInput{ProjectRef: project, EnvironmentRef: environment, ExpectedEnvironmentVersion: current.Version, Specification: spec})
	if err != nil {
		t.Fatal(err)
	}
	draft := created.RuntimeEnvironmentDraft
	validated, err := invoke(command.ValidateRuntimeEnvironmentDraft, "preimpact-validate", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if err != nil {
		t.Fatal(err)
	}
	draft = validated.RuntimeEnvironmentDraft
	if _, err = invoke(command.PublishRuntimeEnvironmentDraft, "preimpact-no-plan", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("publication without plan: %v", err)
	}
	prepared, err := invoke(command.PrepareEnvironmentDraftImpact, "preimpact-prepare", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if err != nil || prepared.RevisionImpactPlan == nil {
		t.Fatalf("prepare existing impact: %v", err)
	}
	plan := prepared.RevisionImpactPlan
	if plan.Total != 2 || plan.SourceRef != environment || plan.SourceRevisionRef != current.CurrentVersion.Ref || plan.TargetDigest != draft.ValidationDigest {
		t.Fatal("prepublication plan pins mismatch")
	}
	unchanged, err := s.GetRuntimeEnvironment(ctx, owner, environment)
	if err != nil || unchanged.Version != current.Version {
		t.Fatal("prepare published environment")
	}
	page, err := s.GetRevisionImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.NextPageToken == "" {
		t.Fatalf("plan page: %v", err)
	}
	last, err := s.GetRevisionImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 1, Token: page.NextPageToken})
	if err != nil || len(last.Items) != 1 || last.Items[0].Ref == page.Items[0].Ref {
		t.Fatalf("plan page continuation: %v", err)
	}
	if _, err = s.GetRevisionImpactPlan(ctx, owner, plan.Ref, "changed", query.Page{Size: 1, Token: page.NextPageToken}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("plan cursor scope: %v", err)
	}
	items := append(page.Items, last.Items...)
	byName, err := s.GetRevisionImpactPlan(ctx, owner, plan.Ref, "Draft pin first", query.Page{Size: 100})
	if err != nil || byName.Total != 1 || len(byName.Items) != 1 || byName.Items[0].ConsumerRef != first {
		t.Fatalf("impact search by agent name: %v", err)
	}
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	currentScope, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	expired := *plan
	expired.Ref, err = newRef("rvip")
	if err != nil {
		t.Fatal(err)
	}
	expired.Digest, err = revisionImpactDigest(expired, currentScope.actorID, items)
	if err != nil {
		t.Fatal(err)
	}
	var expiredID string
	if err = r.pool.QueryRow(ctx, `INSERT INTO control_plane.revision_impact_plans(ref,organization_id,actor_id,kind,snapshot,digest,created_at,expires_at)
SELECT $2,organization_id,actor_id,kind,$3::jsonb,$4,clock_timestamp()-interval '16 minutes',clock_timestamp()-interval '1 minute'
FROM control_plane.revision_impact_plans WHERE ref=$1 RETURNING id::text`, plan.Ref, expired.Ref, string(asJSON(expired)), expired.Digest).Scan(&expiredID); err != nil {
		t.Fatal(err)
	}
	if _, err = r.pool.Exec(ctx, `INSERT INTO control_plane.revision_impact_items(plan_id,ref,snapshot)
SELECT $2::uuid,item.ref,item.snapshot FROM control_plane.revision_impact_items item JOIN control_plane.revision_impact_plans plan ON plan.id=item.plan_id WHERE plan.ref=$1`, plan.Ref, expiredID); err != nil {
		t.Fatal(err)
	}
	expiredRead, err := s.GetRevisionImpactPlan(ctx, owner, expired.Ref, "", query.Page{Size: 100})
	if err != nil || expiredRead.Plan.State != "EXPIRED" {
		t.Fatalf("expired plan tombstone: %v", err)
	}
	if _, err = invoke(command.PublishRuntimeEnvironmentDraft, "preimpact-expired", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, PlanRef: expired.Ref}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("expired publish: %v", err)
	}
	if _, err = r.pool.Exec(ctx, `UPDATE control_plane.revision_impact_plans SET digest=repeat('0',64) WHERE ref=$1`, plan.Ref); err == nil {
		t.Fatal("plan commitment changed")
	}
	if _, err = r.pool.Exec(ctx, `UPDATE control_plane.revision_impact_items SET snapshot='{}'::jsonb WHERE plan_id=(SELECT id FROM control_plane.revision_impact_plans WHERE ref=$1)`, plan.Ref); err == nil {
		t.Fatal("item snapshot changed")
	}
	var selected entity.RevisionImpactItem
	for _, item := range items {
		if item.ConsumerRef == first {
			selected = item
		}
	}
	if selected.Ref == "" {
		t.Fatal("selected item absent")
	}
	bad := command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, PlanRef: plan.Ref, SelectedItemRefs: []string{"rvit_unknown000"}}
	if _, err = invoke(command.PublishRuntimeEnvironmentDraft, "preimpact-foreign-item", &draft.Version, bad); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("arbitrary checkbox ref: %v", err)
	}
	beforeSecond, err := s.GetAgentRuntimeConfiguration(ctx, owner, second)
	if err != nil {
		t.Fatal(err)
	}
	payload := command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, PlanRef: plan.Ref, SelectedItemRefs: []string{selected.Ref}}
	published, err := invoke(command.PublishRuntimeEnvironmentDraft, "preimpact-publish", &draft.Version, payload)
	if err != nil || published.RevisionImpactPlan == nil || published.RevisionImpactPlan.State != "APPLIED" {
		t.Fatalf("publish with selection: %v", err)
	}
	newVersion := published.RuntimeEnvironment.CurrentVersion.Ref
	firstView, err := s.GetAgentRuntimeConfiguration(ctx, owner, first)
	if err != nil || firstView.EnvironmentBinding.VersionRef != newVersion {
		t.Fatalf("actual selected binding: %v", err)
	}
	secondView, err := s.GetAgentRuntimeConfiguration(ctx, owner, second)
	if err != nil || secondView.EnvironmentBinding != beforeSecond.EnvironmentBinding {
		t.Fatalf("unselected binding changed: %v", err)
	}
	outcomes, err := s.GetRevisionImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 100})
	if err != nil || outcomes.Plan.Version != 2 || outcomes.Plan.PublishedRevisionRef != newVersion {
		t.Fatalf("plan receipt read: %v", err)
	}
	for _, item := range outcomes.Items {
		if item.ConsumerRef == first {
			if item.Outcome != "APPLIED" || item.ResultRevisionRef != newVersion || item.ResultBindingRef != selected.BindingRef || item.ResultBindingVersion <= selected.BindingVersion || item.ResultConsumerVersion <= selected.ConsumerVersion {
				t.Fatal("applied item receipt mismatch")
			}
		} else if item.Outcome != "NOT_SELECTED" {
			t.Fatal("unselected item outcome mismatch")
		}
	}
	replay, err := invoke(command.PublishRuntimeEnvironmentDraft, "preimpact-publish", &draft.Version, payload)
	if err != nil || replay.RuntimeEnvironment.Version != published.RuntimeEnvironment.Version || replay.RevisionImpactPlan.Ref != plan.Ref {
		t.Fatalf("publish replay: %v", err)
	}
	if _, err = r.pool.Exec(ctx, `INSERT INTO control_plane.revision_impact_items(plan_id,ref,snapshot)
SELECT plan.id,'rvit_late000001',item.snapshot FROM control_plane.revision_impact_plans plan JOIN control_plane.revision_impact_items item ON item.plan_id=plan.id WHERE plan.ref=$1 LIMIT 1`, plan.Ref); err == nil {
		t.Fatal("terminal plan accepted late item")
	}
	spec.Values = []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "prepublish-partial"}}
	created, err = invoke(command.CreateRuntimeEnvironmentDraft, "preimpact-partial-create", nil, command.RuntimeEnvironmentDraftInput{ProjectRef: project, EnvironmentRef: environment, ExpectedEnvironmentVersion: published.RuntimeEnvironment.Version, Specification: spec})
	if err != nil {
		t.Fatal(err)
	}
	partialDraft := created.RuntimeEnvironmentDraft
	validated, err = invoke(command.ValidateRuntimeEnvironmentDraft, "preimpact-partial-validate", &partialDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: partialDraft.Ref})
	if err != nil {
		t.Fatal(err)
	}
	partialDraft = validated.RuntimeEnvironmentDraft
	prepared, err = invoke(command.PrepareEnvironmentDraftImpact, "preimpact-partial-prepare", &partialDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: partialDraft.Ref})
	if err != nil {
		t.Fatal(err)
	}
	partialPlan := prepared.RevisionImpactPlan
	partialItems, err := s.GetRevisionImpactPlan(ctx, owner, partialPlan.Ref, "", query.Page{Size: 100})
	if err != nil || len(partialItems.Items) != 2 {
		t.Fatalf("partial plan: %v", err)
	}
	selection := []string{}
	for _, item := range partialItems.Items {
		selection = append(selection, item.Ref)
	}
	if _, err = s.Execute(ctx, command.Command{Kind: command.BindAgentRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "preimpact-concurrent-bind", ExpectedVersion: &firstView.AgentVersion}, Payload: command.RuntimeEnvironmentBindingInput{AgentRef: first, EnvironmentRef: environment, VersionRef: newVersion}}); err != nil {
		t.Fatal(err)
	}
	partial, err := invoke(command.PublishRuntimeEnvironmentDraft, "preimpact-partial-publish", &partialDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: partialDraft.Ref, PlanRef: partialPlan.Ref, SelectedItemRefs: selection})
	if err != nil {
		t.Fatal(err)
	}
	partialReceipts, err := s.GetRevisionImpactPlan(ctx, owner, partialPlan.Ref, "", query.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range partialReceipts.Items {
		if item.ConsumerRef == first && item.Outcome != "CONFLICT" {
			t.Fatal("stale binding not preserved as conflict")
		}
		if item.ConsumerRef == second && (item.Outcome != "APPLIED" || item.ResultRevisionRef != partial.RuntimeEnvironment.CurrentVersion.Ref) {
			t.Fatal("independent selected binding not applied")
		}
	}
	firstAfter, err := s.GetAgentRuntimeConfiguration(ctx, owner, first)
	if err != nil || firstAfter.EnvironmentBinding.VersionRef != newVersion {
		t.Fatalf("conflicted binding moved: %v", err)
	}
	created, err = invoke(command.CreateRuntimeEnvironmentDraft, "preimpact-forbidden-create", nil, command.RuntimeEnvironmentDraftInput{ProjectRef: project, EnvironmentRef: environment, ExpectedEnvironmentVersion: partial.RuntimeEnvironment.Version, Specification: spec})
	if err != nil {
		t.Fatal(err)
	}
	forbiddenDraft := created.RuntimeEnvironmentDraft
	validated, err = invoke(command.ValidateRuntimeEnvironmentDraft, "preimpact-forbidden-validate", &forbiddenDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: forbiddenDraft.Ref})
	if err != nil {
		t.Fatal(err)
	}
	forbiddenDraft = validated.RuntimeEnvironmentDraft
	prepared, err = invoke(command.PrepareEnvironmentDraftImpact, "preimpact-forbidden-prepare", &forbiddenDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: forbiddenDraft.Ref})
	if err != nil {
		t.Fatal(err)
	}
	forbiddenPlan := prepared.RevisionImpactPlan
	forbiddenItems, err := s.GetRevisionImpactPlan(ctx, owner, forbiddenPlan.Ref, "", query.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	var forbiddenItem string
	for _, item := range forbiddenItems.Items {
		if item.ConsumerRef == second {
			forbiddenItem = item.Ref
		}
	}
	if forbiddenItem == "" {
		t.Fatal("future forbidden item absent")
	}
	secondAgent, err := s.GetAgent(ctx, owner, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Execute(ctx, command.Command{Kind: command.ArchiveAgent, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "preimpact-archive-consumer", ExpectedVersion: &secondAgent.Version}, Payload: command.AgentInput{Ref: second}}); err != nil {
		t.Fatal(err)
	}
	forbiddenPublication, err := invoke(command.PublishRuntimeEnvironmentDraft, "preimpact-forbidden-publish", &forbiddenDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: forbiddenDraft.Ref, PlanRef: forbiddenPlan.Ref, SelectedItemRefs: []string{forbiddenItem}})
	if err != nil || forbiddenPublication.RevisionImpactPlan.State != "APPLIED" {
		t.Fatalf("publication with revoked consumer: %v", err)
	}
	var storedOutcome, storedResult string
	if err = r.pool.QueryRow(ctx, `SELECT item.outcome,item.result_revision_ref FROM control_plane.revision_impact_items item JOIN control_plane.revision_impact_plans plan ON plan.id=item.plan_id WHERE plan.ref=$1 AND item.ref=$2`, forbiddenPlan.Ref, forbiddenItem).Scan(&storedOutcome, &storedResult); err != nil || storedOutcome != "FORBIDDEN" || storedResult != "" {
		t.Fatalf("revoked consumer receipt: %s %v", storedOutcome, err)
	}
	visible, err := s.GetRevisionImpactPlan(ctx, owner, forbiddenPlan.Ref, "", query.Page{Size: 100})
	if err != nil || visible.Total != 1 || len(visible.Items) != 1 || visible.Items[0].ConsumerRef == second {
		t.Fatalf("hidden consumer readback: %v", err)
	}
	foreign := owner
	foreign.AuthorityTenant = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	if _, err = s.GetRevisionImpactPlan(ctx, foreign, plan.Ref, "", query.Page{}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("foreign plan read: %v", err)
	}
}
