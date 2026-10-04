package platform

import (
	"context"
	"encoding/json"
	"testing"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Вызывается только из изолированного ORG suite; provider и Pod не запускаются.
func organizationEnvironmentClaimFixture(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner value.Principal) func(string, entity.RuntimeEnvironmentSet) string {
	t.Helper()
	seedObservedCatalogFixture(t, ctx, r, func(observation *platformrepo.ProviderModelCatalogObservation) {
		observation.Models = append(observation.Models, platformrepo.ProviderModelCatalogRecord{ID: "gpt-6.1-sol", DefaultReasoningEffort: "low", ReasoningEfforts: []string{"low", "medium", "high"}})
	})
	worker := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	warm := worker
	warm.Permission = "platform.runtime.warm.reconcile"
	execute := func(kind command.Kind, actor value.Principal, key string, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "org-pin-" + key}, Payload: payload})
		if err != nil {
			t.Fatalf("organization pin %s: %v", key, err)
		}
		return result
	}
	return func(key string, expected entity.RuntimeEnvironmentSet) string {
		t.Helper()
		assistant, _, _, err := service.ReconcileWarmRuntime(ctx, warm, "org-pin-warm-fixture")
		if err != nil {
			t.Fatal(err)
		}
		report := warm
		report.Permission = "platform.runtime.warm.report"
		if _, err := service.ReportWarmRuntime(ctx, report, command.WarmRuntimeInput{WorkloadInstance: "org-pin-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
			t.Fatal(err)
		}
		conversation := execute(command.CreateAssistantConversation, owner, key+"-create", command.AssistantConversationInput{AssistantScope: "SYSTEM"}).Conversation
		execute(command.AddAssistantTurn, owner, key+"-turn", command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic environment pin", DeliveryMode: "QUEUE"})
		items := execute(command.ClaimExecution, worker, key+"-claim", command.LeaseInput{WorkloadInstance: "org-pin-controller", Limit: 1}).RuntimeItems
		if len(items) != 1 || stringMap(items[0], "runtimeEnvironmentRef") != expected.Ref ||
			runtimeRevisionMapInt64(items[0], "runtimeEnvironmentVersion") != expected.CurrentVersion.Version || stringMap(items[0], "runtimeEnvironmentDigest") != expected.CurrentVersion.Digest {
			t.Fatal("claimed immutable revision lost the exact environment pin")
		}
		var persisted []byte
		if err := r.pool.QueryRow(ctx, `SELECT safe_snapshot FROM control_plane.runtime_revisions WHERE ref=$1`, stringMap(items[0], "runtimeRevisionRef")).Scan(&persisted); err != nil {
			t.Fatal(err)
		}
		var snapshot map[string]any
		if json.Unmarshal(persisted, &snapshot) != nil || stringMap(snapshot, "runtimeEnvironmentDigest") != expected.CurrentVersion.Digest ||
			runtimeRevisionMapInt64(snapshot, "runtimeEnvironmentVersion") != expected.CurrentVersion.Version {
			t.Fatal("durable runtime revision differs from its claimed environment pin")
		}
		conversations, _, err := service.ListAssistantConversations(ctx, owner, query.AssistantConversationFilter{Filter: query.Filter{Page: query.Page{Size: 50}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range conversations {
			if item.Ref == conversation.Ref {
				conversation = &item
				break
			}
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.CancelAssistantTurn, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "org-pin-" + key + "-cancel", ExpectedVersion: &conversation.Version},
			Payload:  command.AssistantTurnCancellationInput{ConversationRef: conversation.Ref}}); err != nil {
			t.Fatal(err)
		}
		return stringMap(items[0], "runtimeRevisionRef")
	}
}

func testOrganizationEnvironmentPinnedClaim(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, current scope, assistantRef string, base entity.RuntimeEnvironmentDraftSpecification, claim func(string, entity.RuntimeEnvironmentSet) string) {
	t.Helper()
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistantRef)
	if err != nil {
		t.Fatal(err)
	}
	old := view.Environment
	base.Name = old.Name
	base.Values = []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "new-current-without-rebind"}}
	base.SecretBindings = nil
	for _, descriptor := range old.CurrentVersion.SecretDescriptors {
		base.SecretBindings = append(base.SecretBindings, entity.RuntimeSecretBinding{Name: descriptor.Name, SecretRef: descriptor.SecretRef, Revision: descriptor.Revision})
	}
	invoke := func(kind command.Kind, suffix string, version int64, input any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "org-pin-publish-" + suffix, ExpectedVersion: &version}, Payload: input})
		if err != nil {
			t.Fatalf("organization pin publication %s: %v", suffix, err)
		}
		return result
	}
	draft := invoke(command.CreateOrganizationRuntimeEnvironmentDraft, "create", old.Version,
		command.RuntimeEnvironmentDraftInput{EnvironmentRef: old.Ref, ExpectedEnvironmentVersion: old.Version, Specification: base}).RuntimeEnvironmentDraft
	draft = invoke(command.ValidateRuntimeEnvironmentDraft, "validate", draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref}).RuntimeEnvironmentDraft
	plan := invoke(command.PrepareEnvironmentDraftImpact, "prepare", draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref}).RevisionImpactPlan
	page, err := service.GetRevisionImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 100})
	if err != nil || len(page.Items) != 1 || page.Items[0].ScopeKind != "ORGANIZATION" || page.Items[0].OrganizationRef != current.organizationRef || page.Items[0].ProjectRef != "" || page.Items[0].SourceRevisionRef != old.CurrentVersion.Ref {
		t.Fatalf("organization publication preview owner and source pin: %v", err)
	}
	updated := invoke(command.PublishRuntimeEnvironmentDraft, "publish", draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, PlanRef: plan.Ref}).RuntimeEnvironment
	if updated.CurrentVersion.Ref == old.CurrentVersion.Ref {
		t.Fatal("publication did not advance the current environment")
	}
	oldRevision := claim("old-explicit", old)
	impact, err := service.GetRuntimeEnvironmentImpact(ctx, owner, old.Ref, updated.CurrentVersion.Ref, "", query.Page{Size: 10})
	if err != nil || len(impact.Consumers) != 1 || impact.Consumers[0].VersionRef != old.CurrentVersion.Ref || impact.Consumers[0].ScopeKind != "ORGANIZATION" || impact.Consumers[0].OrganizationRef != current.organizationRef {
		t.Fatalf("selective environment impact retained exact old pin: %v", err)
	}
	invoke(command.RebindRuntimeEnvironment, "rebind", updated.Version, command.RuntimeEnvironmentRebindInput{EnvironmentRef: old.Ref, VersionRef: updated.CurrentVersion.Ref, Consumers: impact.Consumers})
	newRevision := claim("new-explicit", *updated)
	if oldRevision == newRevision {
		t.Fatal("selective rebind reused an old immutable runtime revision")
	}
}
