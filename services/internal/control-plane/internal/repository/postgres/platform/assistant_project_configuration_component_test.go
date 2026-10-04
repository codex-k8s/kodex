package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed testdata/sql/assistant_configuration_component_conversation_scope.sql
var queryAssistantConfigurationComponentConversationScope string

//go:embed testdata/sql/assistant_configuration_component_membership.sql
var queryAssistantConfigurationComponentMembership string

//go:embed testdata/sql/assistant_configuration_component_membership_restore.sql
var queryAssistantConfigurationComponentMembershipRestore string

func testAssistantProjectConfiguration(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner, worker, reader value.Principal, lease map[string]any, sourceScope string, sourceProjectRefs ...string) {
	t.Helper()
	prefix := "helper-project-configuration-" + sourceScope
	sourceProjectRef := ""
	if len(sourceProjectRefs) > 0 {
		sourceProjectRef = sourceProjectRefs[0]
		prefix += "-selected-source"
	}
	catalog, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), entity.AssistantConfigurationCatalogRequest{Kind: "ASSISTANTS", AssistantRef: stringMap(lease, "agentRef")})
	if err != nil {
		t.Fatal(err)
	}
	var target entity.AssistantConfigurationCatalogEntry
	for _, entry := range catalog.Entries {
		if entry.ScopeKind == "PROJECT" && (sourceScope == "SYSTEM" || entry.Ref == stringMap(lease, "agentRef")) {
			if sourceProjectRef != "" && entry.ProjectRef == sourceProjectRef {
				continue
			}
			view, viewErr := service.GetAgentRuntimeConfiguration(ctx, owner, entry.Ref)
			if viewErr != nil || view.DraftOverlay != nil {
				continue
			}
			target = entry
			break
		}
	}
	if target.Ref == "" || target.RuntimeEnvironmentRef == "" {
		t.Fatal("fresh helper catalog did not expose the persisted project environment binding")
	}
	invoke := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s %s: %v", kind, key, err)
		}
		return result
	}
	prepare := func(kind, key string, parameters map[string]any) entity.AssistantPlan {
		t.Helper()
		operation := entity.AssistantPlanOperation{Key: key, Type: kind, Title: "Configure project helper", Summary: "Synthetic helper configuration", Parameters: parameters}
		result := executeWorkerAssistantPlan(t, ctx, service, worker, lease, prefix+key, operation)
		if result.Plan == nil {
			t.Fatal("helper configuration plan missing")
		}
		plan := *result.Plan
		op := plan.Operations[0]
		for _, fields := range []map[string]any{op.Parameters, op.Before, op.After} {
			if assistantString(fields, "projectAssistantRef") != target.Ref || assistantString(fields, "assistantScope") != "PROJECT" || assistantString(fields, "scopeKind") != "PROJECT" || assistantString(fields, "organizationRef") != target.OrganizationRef || assistantString(fields, "projectRef") != target.ProjectRef || assistantString(fields, "assistantProfileRef") != target.AssistantProfileRef {
				t.Fatal("specialized helper plan lost authoritative owner pins")
			}
		}
		if sourceScope == "SYSTEM" && plan.ProjectRef != sourceProjectRef {
			t.Fatal("global helper plan was rebound to the project")
		}
		return plan
	}
	apply := func(plan entity.AssistantPlan, key string) command.Result {
		t.Helper()
		validated := invoke(command.ValidateAssistantPlan, key+"-validate", &plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision})
		if validated.Plan == nil || validated.Plan.State != "VALID" {
			t.Fatalf("helper plan validation: %+v", validated.Plan)
		}
		result := invoke(command.ApplyAssistantPlan, key+"-apply", &validated.Plan.Version, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision})
		if result.Plan == nil || result.Plan.State != "APPLIED" || result.PlanReceipt == nil || len(result.PlanReceipt.Operations) != len(plan.Operations) {
			t.Fatalf("helper plan application: %+v", result.Plan)
		}
		if sourceScope == "SYSTEM" {
			if result.Conversation == nil || result.Conversation.ProjectRef != sourceProjectRef || result.Plan.ProjectRef != sourceProjectRef {
				t.Fatal("project helper effects rebound the global conversation response")
			}
			resolved, err := r.ResolvePrincipal(ctx, owner)
			if err != nil {
				t.Fatal(err)
			}
			actorScope, err := r.resolveScope(ctx, resolved)
			if err != nil {
				t.Fatal(err)
			}
			var databaseProjectRef, eventProjectRef string
			if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentConversationScope, pgx.StrictNamedArgs{"organization_id": actorScope.organizationID, "organization_ref": actorScope.organizationRef, "plan_ref": plan.Ref, "conversation_ref": plan.ConversationRef}).Scan(&databaseProjectRef, &eventProjectRef); err != nil || databaseProjectRef != sourceProjectRef || eventProjectRef != sourceProjectRef {
				t.Fatalf("global conversation/event scope changed: %v", err)
			}
		}
		return result
	}
	prepareMany := func(key string, operations ...entity.AssistantPlanOperation) entity.AssistantPlan {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: prefix + key}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Summary: "Synthetic compound helper configuration", Operations: operations}})
		if err != nil || result.Plan == nil {
			t.Fatalf("compound helper proposal %s: %v", key, err)
		}
		return *result.Plan
	}
	environmentPlan := prepare("PREPARE_RUNTIME_ENVIRONMENT_REVISION", "environment", map[string]any{"projectAssistantRef": target.Ref, "description": "Synthetic helper environment " + sourceScope})
	edited := environmentPlan.Operations[0]
	edited.Parameters = cloneAssistantFields(edited.Parameters)
	edited.Parameters["description"] = "Owner reviewed helper environment " + sourceScope
	updated := invoke(command.UpdateAssistantPlan, "environment-edit", &environmentPlan.Version, command.AssistantPlanDraftInput{PlanRef: environmentPlan.Ref, Summary: environmentPlan.Summary, Operations: []entity.AssistantPlanOperation{edited}})
	environmentPlan = *updated.Plan
	environmentApplied := apply(environmentPlan, "environment")
	draftRef := environmentApplied.PlanReceipt.Operations[0].ResourceRef
	draft, err := service.GetRuntimeEnvironmentDraft(ctx, owner, draftRef)
	if err != nil || draft.ProjectRef != target.ProjectRef || draft.ScopeKind != "PROJECT" || draft.State != "DRAFT" {
		t.Fatalf("helper plan did not create a canonical project environment draft: %v", err)
	}
	checked := invoke(command.ValidateRuntimeEnvironmentDraft, "environment-draft-validate", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if checked.RuntimeEnvironmentDraft.State != "VALID" {
		t.Fatalf("helper environment draft invalid: %+v", checked.RuntimeEnvironmentDraft)
	}
	draft = *checked.RuntimeEnvironmentDraft
	impact := invoke(command.PrepareEnvironmentDraftImpact, "environment-impact", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	published := invoke(command.PublishRuntimeEnvironmentDraft, "environment-publish", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, PlanRef: impact.RevisionImpactPlan.Ref})
	if published.RuntimeEnvironment.Ref != target.RuntimeEnvironmentRef {
		t.Fatal("helper environment publication changed its owner locator")
	}
	bindingOperation := entity.AssistantPlanOperation{Key: "binding", Type: "BIND_AGENT_RUNTIME_ENVIRONMENT", Title: "Bind helper environment", Summary: "Synthetic helper binding", Parameters: map[string]any{"projectAssistantRef": target.Ref, "environmentRef": target.RuntimeEnvironmentRef}}
	instructionOperation := entity.AssistantPlanOperation{Key: "instructions", Type: "CREATE_INSTRUCTION_DRAFT", Title: "Prepare helper instructions", Summary: "Synthetic helper instruction draft", Parameters: map[string]any{"projectAssistantRef": target.Ref, "instructions": "Use the explicitly configured project environment and tools only."}}
	binding := prepareMany("instructions-then-binding", instructionOperation, bindingOperation)
	apply(binding, "binding")
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, target.Ref)
	if err != nil || view.EnvironmentBinding.VersionRef != published.RuntimeEnvironment.CurrentVersion.Ref {
		t.Fatalf("helper binding did not select the new published revision: %v", err)
	}
	secondEnvironment := prepare("PREPARE_RUNTIME_ENVIRONMENT_REVISION", "environment-second", map[string]any{"projectAssistantRef": target.Ref, "description": "Second synthetic helper environment " + sourceScope})
	secondApplied := apply(secondEnvironment, "environment-second")
	secondDraft, err := service.GetRuntimeEnvironmentDraft(ctx, owner, secondApplied.PlanReceipt.Operations[0].ResourceRef)
	if err != nil {
		t.Fatal(err)
	}
	secondChecked := invoke(command.ValidateRuntimeEnvironmentDraft, "environment-second-validate", &secondDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: secondDraft.Ref})
	if secondChecked.RuntimeEnvironmentDraft.State != "VALID" {
		t.Fatal("second helper draft invalid")
	}
	secondDraft = *secondChecked.RuntimeEnvironmentDraft
	secondImpact := invoke(command.PrepareEnvironmentDraftImpact, "environment-second-impact", &secondDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: secondDraft.Ref})
	invoke(command.PublishRuntimeEnvironmentDraft, "environment-second-publish", &secondDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: secondDraft.Ref, PlanRef: secondImpact.RevisionImpactPlan.Ref})
	instructionOperation.Parameters["instructions"] = "Use the second explicitly selected environment and bounded project tools."
	reverse := prepareMany("binding-then-instructions", bindingOperation, instructionOperation)
	apply(reverse, "binding-then-instructions")
	view, err = service.GetAgentRuntimeConfiguration(ctx, owner, target.Ref)
	if err != nil {
		t.Fatal(err)
	}
	modelOperation := entity.AssistantPlanOperation{Key: "model", Type: "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION", Title: "Prepare helper model", Summary: "Synthetic compound helper model", Parameters: map[string]any{"agentRef": target.Ref, "runtimeProfileRef": view.Configuration.RuntimeProfileRef, "model": "gpt-5", "reasoningEffort": "low", "providerPolicyMode": view.Configuration.ProviderPolicy.Mode, "providerAccounts": minimalAssistantRuntimeAccounts(view.Configuration.ProviderPolicy.AccountCandidates)}}
	if overlay, err := runtimecontract.ParseConfigOverlay(view.PublishedOverlay.Content); err == nil && overlay.ModelReasoningEffort == "low" {
		modelOperation.Parameters["reasoningEffort"] = "high"
	}
	instructionOperation.Parameters["instructions"] = "Use the configured model, selected environment and project instructions."
	modelInstructions := prepareMany("model-then-instructions", modelOperation, instructionOperation)
	apply(modelInstructions, "model-then-instructions")
	thirdEnvironment := prepare("PREPARE_RUNTIME_ENVIRONMENT_REVISION", "environment-third", map[string]any{"projectAssistantRef": target.Ref, "description": "Third synthetic helper environment " + sourceScope})
	thirdApplied := apply(thirdEnvironment, "environment-third")
	thirdDraft, err := service.GetRuntimeEnvironmentDraft(ctx, owner, thirdApplied.PlanReceipt.Operations[0].ResourceRef)
	if err != nil {
		t.Fatal(err)
	}
	thirdChecked := invoke(command.ValidateRuntimeEnvironmentDraft, "environment-third-validate", &thirdDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: thirdDraft.Ref})
	if thirdChecked.RuntimeEnvironmentDraft.State != "VALID" {
		t.Fatal("third helper draft invalid")
	}
	thirdDraft = *thirdChecked.RuntimeEnvironmentDraft
	thirdImpact := invoke(command.PrepareEnvironmentDraftImpact, "environment-third-impact", &thirdDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: thirdDraft.Ref})
	invoke(command.PublishRuntimeEnvironmentDraft, "environment-third-publish", &thirdDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: thirdDraft.Ref, PlanRef: thirdImpact.RevisionImpactPlan.Ref})
	modelOperation.Parameters["reasoningEffort"] = "medium"
	apply(prepareMany("binding-then-model", bindingOperation, modelOperation), "binding-then-model")
	fourthEnvironment := prepare("PREPARE_RUNTIME_ENVIRONMENT_REVISION", "environment-fourth", map[string]any{"projectAssistantRef": target.Ref, "description": "Fourth synthetic helper environment " + sourceScope})
	fourthApplied := apply(fourthEnvironment, "environment-fourth")
	fourthDraft, err := service.GetRuntimeEnvironmentDraft(ctx, owner, fourthApplied.PlanReceipt.Operations[0].ResourceRef)
	if err != nil {
		t.Fatal(err)
	}
	fourthChecked := invoke(command.ValidateRuntimeEnvironmentDraft, "environment-fourth-validate", &fourthDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: fourthDraft.Ref})
	if fourthChecked.RuntimeEnvironmentDraft.State != "VALID" {
		t.Fatal("fourth helper draft invalid")
	}
	fourthDraft = *fourthChecked.RuntimeEnvironmentDraft
	fourthImpact := invoke(command.PrepareEnvironmentDraftImpact, "environment-fourth-impact", &fourthDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: fourthDraft.Ref})
	invoke(command.PublishRuntimeEnvironmentDraft, "environment-fourth-publish", &fourthDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: fourthDraft.Ref, PlanRef: fourthImpact.RevisionImpactPlan.Ref})
	modelOperation.Parameters["reasoningEffort"] = "high"
	apply(prepareMany("model-then-binding", modelOperation, bindingOperation), "model-then-binding")
	instructions := prepare("CREATE_INSTRUCTION_DRAFT", "instructions", map[string]any{"projectAssistantRef": target.Ref, "instructions": "Use the explicitly configured project environment and tools only."})
	instructionApplied := apply(instructions, "instructions")
	resolvedOwner, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	actorScope, err := r.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal(err)
	}
	var effectsBefore, effectsAfter string
	if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, actorScope.organizationID).Scan(&effectsBefore); err != nil {
		t.Fatal(err)
	}
	var revokedBindings []string
	if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentMembership, pgx.StrictNamedArgs{"organization_id": actorScope.organizationID, "actor_id": actorScope.actorID}).Scan(&revokedBindings); err != nil {
		t.Fatal(err)
	}
	wrongVersion := instructionApplied.Plan.Version + 99
	_, revokedErr := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "-instructions-apply", ExpectedVersion: &wrongVersion}, Payload: command.AssistantPlanInput{PlanRef: instructions.Ref, Revision: instructions.Revision}})
	if _, err := r.pool.Exec(ctx, queryAssistantConfigurationComponentMembershipRestore, pgx.StrictNamedArgs{"organization_id": actorScope.organizationID, "actor_id": actorScope.actorID, "refs": revokedBindings}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(revokedErr, errs.ErrForbidden) && !errors.Is(revokedErr, errs.ErrNotFound) {
		t.Fatalf("revoked helper owner reached old receipt/OCC: %v", revokedErr)
	}
	if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, actorScope.organizationID).Scan(&effectsAfter); err != nil || effectsBefore != effectsAfter {
		t.Fatal("revoked helper owner mutated audit/receipt/outbox")
	}
	for key, extra := range map[string]map[string]any{
		"empty-system": {"systemAssistantRef": ""}, "empty-environment": {"environmentRef": ""}, "null-environment": {"environmentRef": nil}, "ordinary-agent": {"agentRef": target.Ref},
	} {
		parameters := map[string]any{"projectAssistantRef": target.Ref, "description": "Reject ambiguous helper configuration"}
		for field, value := range extra {
			parameters[field] = value
		}
		_, negativeErr := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: prefix + "-" + key}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Summary: "Forbidden mixed locators", Operations: []entity.AssistantPlanOperation{{Key: "forbidden", Type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION", Title: "Forbidden", Summary: "Forbidden", Parameters: parameters}}}})
		if !errors.Is(negativeErr, errs.ErrInvalid) && !errors.Is(negativeErr, errs.ErrForbidden) {
			t.Fatalf("helper mixed locator %s accepted: %v", key, negativeErr)
		}
	}
	if sourceScope == "PROJECT" {
		system, err := service.GetSystemAssistant(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: prefix + "-deny-system-locator"}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Summary: "Forbidden owner scope", Operations: []entity.AssistantPlanOperation{{Key: "forbidden", Type: "CREATE_INSTRUCTION_DRAFT", Title: "Forbidden", Summary: "Forbidden", Parameters: map[string]any{"projectAssistantRef": system.Ref, "instructions": "Never modify another assistant owner configuration."}}}}})
		if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("project helper reached another assistant locator: %v", err)
		}
	}
}
