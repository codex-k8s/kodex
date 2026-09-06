package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"testing"
)

// Старые lifecycle fixtures проходят новый настоящий Prepare, не подставляют plan ref.
func executePromptPublicationFixture(t *testing.T, ctx context.Context, service *platformservice.Service, input command.Command) (command.Result, error) {
	t.Helper()
	if input.Kind != command.PublishPromptTemplateDraft {
		return service.Execute(ctx, input)
	}
	payload := input.Payload.(command.ManagedConfigurationInput)
	prepare := input
	prepare.Kind = command.PreparePromptTemplateImpact
	prepare.Mutation.IdempotencyKey += "-impact"
	prepare.Payload = payload
	result, err := service.Execute(ctx, prepare)
	if err != nil {
		return command.Result{}, err
	}
	if result.RevisionImpactPlan == nil {
		return command.Result{}, errs.ErrUnavailable
	}
	payload.PlanRef = result.RevisionImpactPlan.Ref
	payload.SelectedItemRefs = []string{}
	input.Payload = payload
	return service.Execute(ctx, input)
}

func testPromptImpactLifecycle(t *testing.T, ctx context.Context, r *Repository) {
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	service, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-impact-project"}, Payload: command.ProjectInput{Name: "Prompt impact", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	first := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "prompt-impact-first", "Prompt applied")
	second := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "prompt-impact-second", "Prompt conflict")
	third := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "prompt-impact-third", "Prompt forbidden")
	invoke := func(kind command.Kind, key string, version *int64, p command.ManagedConfigurationInput) command.Result {
		t.Helper()
		result, err := executePromptPublicationFixture(t, ctx, service, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: version}, Payload: p})
		if err != nil {
			t.Fatalf("prompt impact %s: %v", key, err)
		}
		return result
	}
	p := command.ManagedConfigurationInput{ProjectRef: project.Project.Ref, Name: "Impact source", ContentFormat: "TEXT", Content: "Original instructions for the assigned task."}
	created := invoke(command.CreatePromptTemplateDraft, "prompt-impact-create", nil, p)
	p.ConfigurationRef = created.ManagedConfiguration.Ref
	p.RevisionRef = created.ManagedRevision.Ref
	valid := invoke(command.ValidatePromptTemplateDraft, "prompt-impact-valid", &created.ManagedConfiguration.Version, p)
	published := invoke(command.PublishPromptTemplateDraft, "prompt-impact-publish", &valid.ManagedConfiguration.Version, p)
	old := published.ManagedRevision.Ref
	impact, err := service.GetManagedConfigurationImpact(ctx, owner, p.ConfigurationRef, old, query.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	p.ImpactDigest = impact.Digest
	p.Consumers = []entity.ManagedConfigurationConsumer{{Kind: "AGENT", Ref: first.Ref}, {Kind: "AGENT", Ref: second.Ref}, {Kind: "AGENT", Ref: third.Ref}}
	bound := invoke(command.RebindPromptTemplate, "prompt-impact-bind", &published.ManagedConfiguration.Version, p)
	p.Consumers = nil
	p.ImpactDigest = ""
	p.Content = "Updated instructions with a distinct immutable revision."
	draft := invoke(command.CreatePromptTemplateDraft, "prompt-impact-new", &bound.ManagedConfiguration.Version, p)
	p.RevisionRef = draft.ManagedRevision.Ref
	valid = invoke(command.ValidatePromptTemplateDraft, "prompt-impact-new-valid", &draft.ManagedConfiguration.Version, p)
	prepared := invoke(command.PreparePromptTemplateImpact, "prompt-impact-plan", &valid.ManagedConfiguration.Version, p)
	page, err := service.GetRevisionImpactPlan(ctx, owner, prepared.RevisionImpactPlan.Ref, "", query.Page{Size: 100})
	if err != nil || page.Total != 3 {
		t.Fatalf("prompt plan consumers: %v", err)
	}
	if _, err = service.Execute(ctx, command.Command{Kind: command.CreateInstructions, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-impact-stale-consumer", ExpectedVersion: &second.Version}, Payload: command.AgentInput{Ref: second.Ref, Instructions: "Unpublished draft changes only the consumer version."}}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Execute(ctx, command.Command{Kind: command.ArchiveAgent, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-impact-archive", ExpectedVersion: &third.Version}, Payload: command.AgentInput{Ref: third.Ref}}); err != nil {
		t.Fatal(err)
	}
	p.PlanRef = page.Plan.Ref
	for _, item := range page.Items {
		p.SelectedItemRefs = append(p.SelectedItemRefs, item.Ref)
	}
	publish := command.Command{Kind: command.PublishPromptTemplateDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "prompt-impact-selected", ExpectedVersion: &valid.ManagedConfiguration.Version}, Payload: p}
	result, err := service.Execute(ctx, publish)
	if err != nil || result.RevisionImpactPlan == nil || result.RevisionImpactPlan.State != "APPLIED" {
		t.Fatalf("selective prompt publication: %v", err)
	}
	read, err := service.GetRevisionImpactPlan(ctx, owner, page.Plan.Ref, "", query.Page{Size: 100})
	if err != nil || read.Total != 2 || read.Plan.Total != 3 {
		t.Fatalf("filtered plan read: %v", err)
	}
	for _, item := range read.Items {
		if item.ConsumerRef == first.Ref && (item.Outcome != "APPLIED" || item.ResultRevisionRef != p.RevisionRef || item.ResultBindingRef != item.BindingRef || item.ResultBindingVersion <= item.BindingVersion) {
			t.Fatal("applied prompt result mismatch")
		}
		if item.ConsumerRef == second.Ref && item.Outcome != "CONFLICT" {
			t.Fatal("stale prompt binding was overwritten")
		}
	}
	var forbidden string
	if err = r.pool.QueryRow(ctx, `SELECT item.outcome FROM control_plane.revision_impact_items item JOIN control_plane.revision_impact_plans plan ON plan.id=item.plan_id WHERE plan.ref=$1 AND item.snapshot->>'ConsumerRef'=$2`, page.Plan.Ref, third.Ref).Scan(&forbidden); err != nil || forbidden != "FORBIDDEN" {
		t.Fatalf("hidden forbidden receipt: %v", err)
	}
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ agent, revision string }{{first.Ref, p.RevisionRef}, {second.Ref, old}} {
		effective, err := r.GetEffectivePromptTemplate(ctx, resolved, check.agent)
		if err != nil || effective.Ref != check.revision {
			t.Fatalf("effective prompt binding: %v", err)
		}
	}
	replay, err := service.Execute(ctx, publish)
	if err != nil || replay.RevisionImpactPlan == nil || replay.RevisionImpactPlan.Digest != result.RevisionImpactPlan.Digest {
		t.Fatalf("prompt publication replay: %v", err)
	}
}
