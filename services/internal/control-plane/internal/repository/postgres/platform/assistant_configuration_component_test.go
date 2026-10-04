package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed testdata/sql/assistant_configuration_component_revision.sql
var queryAssistantConfigurationComponentRevision string

//go:embed testdata/sql/assistant_configuration_component_effects.sql
var queryAssistantConfigurationComponentEffects string

//go:embed testdata/sql/assistant_configuration_component_profile_bump.sql
var queryAssistantConfigurationComponentProfileBump string

//go:embed testdata/sql/assistant_configuration_component_profile_clone.sql
var queryAssistantConfigurationComponentProfileClone string

//go:embed testdata/sql/assistant_configuration_component_account_binding_constraint.sql
var queryAssistantConfigurationComponentAccountBindingConstraint string

//go:embed testdata/sql/assistant_current_configuration_expire.sql
var queryAssistantCurrentConfigurationExpire string

//go:embed testdata/sql/assistant_current_configuration_restore_expiry.sql
var queryAssistantCurrentConfigurationRestoreExpiry string

// Сценарий вызывается публичной обязательной profile suite; callbacks только
// синтетические, provider и пользовательский браузер не используются.
func testAssistantConfigurationPipeline(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner, worker, reader value.Principal, lease map[string]any, sourceScope string) {
	t.Helper()
	prefix := "helper-configuration-" + sourceScope
	agentRef := stringMap(lease, "agentRef")
	generation := lease["generation"].(int64)
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agentRef)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := promotionComponentCatalog(t)
	r.ConfigureRoleImageCatalog(catalog)
	readCatalog := func(kind string) entity.AssistantConfigurationCatalogResponse {
		t.Helper()
		input := entity.AssistantConfigurationCatalogRequest{Kind: kind, AssistantRef: agentRef}
		if kind == "MODELS" {
			input.AccountRef = view.Configuration.ProviderPolicy.AccountCandidates[0].AccountRef
		}
		result, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), generation, input)
		if err != nil || result.AssistantRef != agentRef || result.OrganizationRef == "" || result.ScopeKind != map[string]string{"SYSTEM": "ORGANIZATION", "PROJECT": "PROJECT"}[sourceScope] {
			t.Fatalf("fresh %s catalog owner: %+v %v", kind, result, err)
		}
		for _, entry := range result.Entries {
			if entry.OrganizationRef != result.OrganizationRef || kind != "ASSISTANTS" && (entry.ScopeKind != result.ScopeKind || entry.ProjectRef != result.ProjectRef || entry.AssistantProfileRef != result.AssistantProfileRef) {
				t.Fatalf("%s catalog mixed owner", kind)
			}
		}
		return result
	}
	for _, kind := range []string{"ASSISTANTS", "RUNTIME_PROFILES", "PROVIDER_ACCOUNTS", "MODELS", "ROLE_IMAGE_RECIPES", "IMAGE_ARTIFACTS", "ROLE_ENVIRONMENTS"} {
		result := readCatalog(kind)
		if len(result.Entries) == 0 {
			t.Fatalf("%s catalog unexpectedly empty", kind)
		}
		if kind == "RUNTIME_PROFILES" {
			for _, entry := range result.Entries {
				if entry.Version < 1 || entry.Ref == "" || entry.Provider == "" {
					t.Fatal("runtime profile lost persisted version")
				}
			}
		}
		if kind == "MODELS" {
			for _, entry := range result.Entries {
				if entry.CatalogRevision == "" || len(entry.CatalogDigest) != 64 {
					t.Fatal("model catalog lost immutable pins")
				}
			}
		}
	}
	resolvedOwnRead, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	ownReadScope, err := r.resolveScope(ctx, resolvedOwnRead)
	if err != nil {
		t.Fatal(err)
	}
	var ownReadEffectsBefore, ownReadEffectsAfter string
	if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, ownReadScope.organizationID).Scan(&ownReadEffectsBefore); err != nil {
		t.Fatal(err)
	}
	current := readCatalog("CURRENT_CONFIGURATION")
	if len(current.Entries) != 0 || current.NextOffset != 0 || current.CurrentConfiguration == nil ||
		current.CurrentConfiguration.AgentVersion != view.AgentVersion || current.CurrentConfiguration.Configuration.Digest != view.Configuration.Digest ||
		current.CurrentConfiguration.Environment.Digest != view.Environment.CurrentVersion.Digest || current.CurrentConfiguration.PublishedInstructions == "" ||
		len(current.CurrentConfiguration.TemplateVariables) == 0 || len(current.CurrentConfiguration.Environment.SecretDescriptors) != 0 {
		t.Fatal("own current configuration lost fresh safe authoritative settings")
	}
	if sourceScope == "SYSTEM" && (current.CurrentConfiguration.SystemCoreRevision == "" || current.CurrentConfiguration.SystemCoreInstructions == "" || current.CurrentConfiguration.OwnerInstructionsRevision < 1) {
		t.Fatal("SYSTEM current configuration lost versioned core/owner instructions")
	}
	for _, denied := range []struct {
		ref, lease, fence string
		generation        int64
	}{
		{agentRef + "foreign", stringMap(lease, "leaseRef"), stringMap(lease, "fence"), generation},
		{agentRef, "lease_missing123", stringMap(lease, "fence"), generation},
		{agentRef, stringMap(lease, "leaseRef"), "stale-fence", generation},
		{agentRef, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), generation + 1},
	} {
		_, err := service.ListAssistantConfigurationCatalog(ctx, reader, denied.lease, denied.fence, denied.generation, entity.AssistantConfigurationCatalogRequest{Kind: "CURRENT_CONFIGURATION", AssistantRef: denied.ref})
		if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("current configuration accepted foreign target or inactive exact lease")
		}
	}
	var originalExpiry time.Time
	if err := r.pool.QueryRow(ctx, queryAssistantCurrentConfigurationExpire, stringMap(lease, "leaseRef")).Scan(&originalExpiry); err != nil {
		t.Fatal(err)
	}
	_, expiredErr := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), generation, entity.AssistantConfigurationCatalogRequest{Kind: "CURRENT_CONFIGURATION", AssistantRef: agentRef})
	if tag, err := r.pool.Exec(ctx, queryAssistantCurrentConfigurationRestoreExpiry, stringMap(lease, "leaseRef"), originalExpiry); err != nil || tag.RowsAffected() != 1 {
		t.Fatal("could not restore disposable exact lease fixture")
	}
	if !errors.Is(expiredErr, errs.ErrNotFound) {
		t.Fatal("expired lease disclosed own configuration")
	}
	if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, ownReadScope.organizationID).Scan(&ownReadEffectsAfter); err != nil || ownReadEffectsBefore != ownReadEffectsAfter {
		t.Fatal("own configuration query changed audit/receipt/state/events")
	}
	invalid := entity.AssistantConfigurationCatalogRequest{Kind: "MODELS", AssistantRef: agentRef}
	if _, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), generation, invalid); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("missing account catalog allowed: %v", err)
	}
	invalid.Kind = "ASSISTANTS"
	invalid.AccountRef = "account-forbidden"
	if _, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), generation, invalid); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("mixed catalog fields allowed: %v", err)
	}
	if _, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), "stale-fence", generation, entity.AssistantConfigurationCatalogRequest{Kind: "ASSISTANTS", AssistantRef: agentRef}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("stale lease catalog allowed: %v", err)
	}
	if sourceScope == "PROJECT" {
		var constraintValidated bool
		var bindingConstraint string
		if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentAccountBindingConstraint).Scan(&constraintValidated, &bindingConstraint); err != nil {
			t.Fatal(err)
		}
		if !constraintValidated || !strings.Contains(bindingConstraint, "'PROVIDER_ACCOUNT'") || strings.Contains(bindingConstraint, "'ROLE_IMAGE'") || strings.Contains(bindingConstraint, "'SECRET'") || strings.Contains(bindingConstraint, "'RUNTIME_ENVIRONMENT'") {
			t.Fatal("instance binding migration widened unrelated organization kinds")
		}
		resolved, err := r.ResolvePrincipal(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		actorScope, err := r.resolveScope(ctx, resolved)
		if err != nil {
			t.Fatal(err)
		}
		accountRef := view.Configuration.ProviderPolicy.AccountCandidates[0].AccountRef
		accountRole := createRoleImageAccessRole(t, ctx, service, owner, prefix+"-account-role", "Helper account reader", []string{"provider.account.view"}, []string{"RESOURCE_INSTANCE"})
		if _, err := r.pool.Exec(ctx, queryOrganizationImageComponentOwner, actorScope.organizationID, actorScope.actorID, "MEMBER"); err != nil {
			t.Fatal(err)
		}
		accounts, accountsErr := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), generation, entity.AssistantConfigurationCatalogRequest{Kind: "PROVIDER_ACCOUNTS", AssistantRef: agentRef})
		models, modelsErr := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), generation, entity.AssistantConfigurationCatalogRequest{Kind: "MODELS", AssistantRef: agentRef, AccountRef: accountRef})
		_, canonicalErr := service.GetProviderAccount(ctx, owner, accountRef)
		if _, err := r.pool.Exec(ctx, queryOrganizationImageComponentOwner, actorScope.organizationID, actorScope.actorID, "OWNER"); err != nil {
			t.Fatal(err)
		}
		if accountsErr != nil || len(accounts.Entries) != 0 || modelsErr != nil || len(models.Entries) == 0 || !errors.Is(canonicalErr, errs.ErrNotFound) {
			t.Fatalf("account catalog widened canonical eligibility: list=%v count=%d models=%v canonical=%v", accountsErr, len(accounts.Entries), modelsErr, canonicalErr)
		}
		createRoleImageAccessBinding(t, ctx, service, owner, prefix+"-account-binding", actorScope.actorRef, accountRole.CurrentVersion.Ref, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROVIDER_ACCOUNT", ResourceRef: accountRef})
		if _, err := r.pool.Exec(ctx, queryOrganizationImageComponentOwner, actorScope.organizationID, actorScope.actorID, "MEMBER"); err != nil {
			t.Fatal(err)
		}
		accounts, accountsErr = service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), generation, entity.AssistantConfigurationCatalogRequest{Kind: "PROVIDER_ACCOUNTS", AssistantRef: agentRef})
		models, modelsErr = service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), generation, entity.AssistantConfigurationCatalogRequest{Kind: "MODELS", AssistantRef: agentRef, AccountRef: accountRef})
		if _, err := r.pool.Exec(ctx, queryOrganizationImageComponentOwner, actorScope.organizationID, actorScope.actorID, "OWNER"); err != nil {
			t.Fatal(err)
		}
		if accountsErr != nil || len(accounts.Entries) != 1 || accounts.Entries[0].Ref != accountRef || modelsErr != nil || len(models.Entries) == 0 {
			t.Fatalf("instance-only account grant was clipped: list=%v count=%d models=%v", accountsErr, len(accounts.Entries), modelsErr)
		}
	}
	operation := entity.AssistantPlanOperation{Key: "model", Type: "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION", Title: "Prepare model", Summary: "Synthetic versioned model configuration", Parameters: map[string]any{"agentRef": agentRef, "runtimeProfileRef": view.Configuration.RuntimeProfileRef, "model": "gpt-5", "reasoningEffort": "low", "providerPolicyMode": view.Configuration.ProviderPolicy.Mode, "providerAccounts": minimalAssistantRuntimeAccounts(view.Configuration.ProviderPolicy.AccountCandidates)}}
	operation.Parameters["webSearchMode"] = "cached"
	propose := func(op entity.AssistantPlanOperation, key string) entity.AssistantPlan {
		t.Helper()
		return *executeWorkerAssistantPlan(t, ctx, service, worker, lease, prefix+"-"+key, op).Plan
	}
	execute := func(kind command.Kind, key string, plan entity.AssistantPlan, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "-" + key, ExpectedVersion: &plan.Version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s %s: %v", kind, key, err)
		}
		return result
	}
	validate := func(plan entity.AssistantPlan, key string) entity.AssistantPlan {
		t.Helper()
		result := execute(command.ValidateAssistantPlan, key, plan, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision})
		if result.Plan == nil || result.Plan.State != "VALID" {
			t.Fatalf("plan validation failed: %+v", result.Plan)
		}
		return *result.Plan
	}
	apply := func(plan entity.AssistantPlan, key string) command.Result {
		t.Helper()
		frozenOperations, marshalErr := json.Marshal(plan.Operations)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		result := execute(command.ApplyAssistantPlan, key, plan, command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision})
		if result.Plan == nil || result.Plan.State != "APPLIED" {
			t.Fatalf("plan apply failed: %+v", result.Plan)
		}
		if !assistantJSONEqual(json.RawMessage(frozenOperations), result.Plan.Operations) {
			t.Fatal("effect carry rewrote immutable plan operations")
		}
		return result
	}
	plan := propose(operation, "model-proposal")
	beforeAgentVersion, versionOK := assistantInt64(plan.Operations[0].Before, "agentVersion")
	if !versionOK || assistantString(plan.Operations[0].Before, "agentRef") != agentRef || beforeAgentVersion != view.AgentVersion || plan.Operations[0].Target.Kind != "AGENT" || *plan.Operations[0].ExpectedVersion != view.AgentVersion {
		t.Fatal("model plan lost server-owned target")
	}
	unchanged, err := service.GetAgentRuntimeConfiguration(ctx, owner, agentRef)
	if err != nil || unchanged.Configuration.Ref != view.Configuration.Ref {
		t.Fatal("proposal published without human gate")
	}
	edited := plan.Operations[0]
	edited.Parameters = cloneAssistantFields(edited.Parameters)
	edited.Parameters["reasoningEffort"] = "medium"
	edited.Parameters["webSearchMode"] = "live"
	updated := execute(command.UpdateAssistantPlan, "model-edit", plan, command.AssistantPlanDraftInput{PlanRef: plan.Ref, Summary: plan.Summary, Operations: []entity.AssistantPlanOperation{edited}})
	plan = *updated.Plan
	validated := validate(plan, "model-validate")
	var beforeRevision, afterRevision []byte
	if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentRevision, stringMap(lease, "leaseRef")).Scan(&beforeRevision); err != nil {
		t.Fatal(err)
	}
	applied := apply(validated, "model-apply")
	if applied.PlanReceipt == nil || len(applied.PlanReceipt.Operations) != 1 || applied.PlanReceipt.Operations[0].ResourceRef != agentRef {
		t.Fatal("runtime receipt did not pin agent")
	}
	changed, err := service.GetAgentRuntimeConfiguration(ctx, owner, agentRef)
	if err != nil || changed.Configuration.Ref == view.Configuration.Ref {
		t.Fatalf("model config not published: %v", err)
	}
	parsed, err := runtimecontract.ParseConfigOverlay(changed.PublishedOverlay.Content)
	if err != nil || parsed.ModelReasoningEffort != "medium" || parsed.WebSearchMode != "live" || changed.PublishedOverlay.Digest == view.PublishedOverlay.Digest || changed.PublishedOverlay.Version <= view.PublishedOverlay.Version {
		t.Fatalf("reasoning overlay not published: %v", err)
	}
	noChange := operation
	noChange.Key = "unchanged-runtime"
	noChange.Parameters = cloneAssistantFields(operation.Parameters)
	noChange.Parameters["reasoningEffort"] = "medium"
	delete(noChange.Parameters, "webSearchMode") // отсутствие сохраняет опубликованный режим
	actualChange := noChange
	actualChange.Key = "changed-runtime"
	actualChange.Parameters = cloneAssistantFields(noChange.Parameters)
	actualChange.Parameters["reasoningEffort"] = "high"
	mixed, mixedErr := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: prefix + "-mixed-no-change"}, Payload: command.ProposeAssistantPlanInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: generation,
			Summary: "Synthetic unchanged and effective configuration", Operations: []entity.AssistantPlanOperation{noChange, actualChange},
		}})
	if mixedErr != nil || mixed.Plan == nil || mixed.Plan.State != "DRAFT" || len(mixed.Plan.Operations) != 1 || mixed.Plan.Operations[0].Key != actualChange.Key {
		t.Fatalf("unchanged runtime prevented an authorized effective draft: %v", mixedErr)
	}
	_, emptyErr := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: prefix + "-all-no-change"}, Payload: command.ProposeAssistantPlanInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: generation,
			Summary: "Synthetic unchanged configuration", Operations: []entity.AssistantPlanOperation{noChange},
		}})
	stage, category, _, diagnostic := errs.AssistantPlanDiagnostic(emptyErr)
	if !errors.Is(emptyErr, errs.ErrConflict) || !diagnostic || stage != errs.AssistantPlanEmpty || category != "CONFLICT" {
		t.Fatal("all unchanged operations created an empty plan or lost the closed EMPTY result")
	}
	for _, invalid := range []struct {
		name     string
		expected error
	}{
		{"invalid-title", errs.ErrInvalid},
		{"invalid-search-mode", errs.ErrInvalid},
		{"null-search-mode", errs.ErrInvalid},
		{"ineligible-account", errs.ErrConflict},
		{"stale-lease", errs.ErrForbidden},
	} {
		candidate := noChange
		candidate.Parameters = cloneAssistantFields(noChange.Parameters)
		fence := stringMap(lease, "fence")
		switch invalid.name {
		case "invalid-title":
			candidate.Title = ""
		case "invalid-search-mode":
			candidate.Parameters["webSearchMode"] = "future-mode"
		case "null-search-mode":
			candidate.Parameters["webSearchMode"] = nil
		case "ineligible-account":
			candidate.Parameters["providerAccounts"] = []map[string]any{{"accountRef": "pacc_absent_synthetic", "weight": 1}}
		case "stale-lease":
			fence = "stale-synthetic-fence"
		}
		_, candidateErr := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: prefix + "-no-change-" + invalid.name}, Payload: command.ProposeAssistantPlanInput{
				LeaseRef: stringMap(lease, "leaseRef"), Fence: fence, Generation: generation,
				Summary: "Synthetic no-change boundary rejection", Operations: []entity.AssistantPlanOperation{candidate, actualChange},
			}})
		if !errors.Is(candidateErr, invalid.expected) {
			t.Fatalf("no-change skipped %s boundary", invalid.name)
		}
	}
	afterNoChange, noChangeReadErr := service.GetAgentRuntimeConfiguration(ctx, owner, agentRef)
	if noChangeReadErr != nil || !assistantJSONEqual(changed, afterNoChange) {
		t.Fatal("preparing a mixed no-change draft changed current configuration")
	}
	if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentRevision, stringMap(lease, "leaseRef")).Scan(&afterRevision); err != nil || string(beforeRevision) != string(afterRevision) {
		t.Fatal("self-config rewrote active immutable runtime")
	}
	replay := execute(command.ApplyAssistantPlan, "model-apply", validated, command.AssistantPlanInput{PlanRef: validated.Ref, Revision: validated.Revision})
	if replay.Plan == nil || !assistantJSONEqual(replay.PlanReceipt, applied.PlanReceipt) {
		t.Fatal("runtime lost-ACK replay duplicated publication")
	}
	operation.Parameters["model"], operation.Parameters["reasoningEffort"] = "future-model", "adaptive"
	modelSwitch := propose(operation, "future-model-proposal")
	modelSwitchValidated := validate(modelSwitch, "future-model-validate")
	apply(modelSwitchValidated, "future-model-apply")
	changed, err = service.GetAgentRuntimeConfiguration(ctx, owner, agentRef)
	if err != nil || changed.Configuration.Model != "future-model" {
		t.Fatalf("model switch did not use prospective overlay: %v", err)
	}
	parsed, err = runtimecontract.ParseConfigOverlay(changed.PublishedOverlay.Content)
	if err != nil || parsed.ModelReasoningEffort != "adaptive" {
		t.Fatal("model switch lost catalog-compatible reasoning")
	}
	operation.Parameters["model"] = "gpt-5"
	operation.Parameters["reasoningEffort"] = "high"
	staleProfile := propose(operation, "stale-profile-proposal")
	if _, err := r.pool.Exec(ctx, queryAssistantConfigurationComponentProfileBump, changed.Configuration.RuntimeProfileRef); err != nil {
		t.Fatal(err)
	}
	profileInvalidated := execute(command.ValidateAssistantPlan, "stale-profile-validate", staleProfile, command.AssistantPlanInput{PlanRef: staleProfile.Ref, Revision: staleProfile.Revision})
	if profileInvalidated.Plan == nil || profileInvalidated.Plan.State == "VALID" {
		t.Fatal("stale runtime profile version/revision was accepted")
	}
	refreshPlan := validate(propose(operation, "refresh-proposal"), "refresh-validate")
	originalOperation := refreshPlan.Operations[0]
	if _, err := r.pool.Exec(ctx, queryAssistantConfigurationComponentProfileBump, changed.Configuration.RuntimeProfileRef); err != nil {
		t.Fatal(err)
	}
	conflict := execute(command.ApplyAssistantPlan, "refresh-stale-apply", refreshPlan, command.AssistantPlanInput{PlanRef: refreshPlan.Ref, Revision: refreshPlan.Revision})
	if conflict.Plan == nil || conflict.Plan.State != "STALE" || conflict.PlanReceipt == nil || conflict.PlanReceipt.Outcome != "CONFLICT" {
		t.Fatal("stale model dependency did not produce a conflict receipt")
	}
	staleOperation := conflict.Plan.Operations[0]
	staleOperation.Parameters = cloneAssistantFields(staleOperation.Parameters)
	staleOperation.Parameters["organizationRef"] = "org_foreign_snapshot"
	_, badOwnerErr := service.Execute(ctx, command.Command{Kind: command.UpdateAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "-refresh-bad-owner", ExpectedVersion: &conflict.Plan.Version}, Payload: command.AssistantPlanDraftInput{PlanRef: conflict.Plan.Ref, Summary: conflict.Plan.Summary, Operations: []entity.AssistantPlanOperation{staleOperation}}})
	if !errors.Is(badOwnerErr, errs.ErrForbidden) {
		t.Fatalf("STALE refresh accepted an edited owner locator: %v", badOwnerErr)
	}
	refreshed := execute(command.UpdateAssistantPlan, "refresh-new-revision", *conflict.Plan, command.AssistantPlanDraftInput{PlanRef: conflict.Plan.Ref, Summary: conflict.Plan.Summary, Operations: conflict.Plan.Operations})
	if refreshed.Plan == nil || refreshed.Plan.State != "DRAFT" || refreshed.Plan.Revision != refreshPlan.Revision+1 || assistantJSONEqual(originalOperation.After["runtimeProfilePin"], refreshed.Plan.Operations[0].After["runtimeProfilePin"]) {
		t.Fatal("STALE model plan did not create a fresh versioned snapshot")
	}
	apply(validate(*refreshed.Plan, "refresh-new-validate"), "refresh-new-apply")
	changed, err = service.GetAgentRuntimeConfiguration(ctx, owner, agentRef)
	if err != nil {
		t.Fatal(err)
	}
	alternativeProfile := "fixture_assistant_" + sourceScope
	if _, err := r.pool.Exec(ctx, queryAssistantConfigurationComponentProfileClone, pgx.StrictNamedArgs{"source_ref": changed.Configuration.RuntimeProfileRef, "target_ref": alternativeProfile}); err != nil {
		t.Fatal(err)
	}
	alternative := operation
	alternative.Parameters = cloneAssistantFields(operation.Parameters)
	alternative.Parameters["runtimeProfileRef"] = alternativeProfile
	alternativePlan := propose(alternative, "alternate-profile-draft")
	if _, err := r.pool.Exec(ctx, queryAssistantConfigurationComponentProfileBump, alternativeProfile); err != nil {
		t.Fatal(err)
	}
	alternativeEdited := alternativePlan.Operations[0]
	alternativeEdited.Parameters = cloneAssistantFields(alternativeEdited.Parameters)
	alternativeEdited.Parameters["reasoningEffort"] = "medium"
	_, alternativeEditErr := service.Execute(ctx, command.Command{Kind: command.UpdateAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "-alternate-drift-edit", ExpectedVersion: &alternativePlan.Version}, Payload: command.AssistantPlanDraftInput{PlanRef: alternativePlan.Ref, Summary: alternativePlan.Summary, Operations: []entity.AssistantPlanOperation{alternativeEdited}}})
	if !errors.Is(alternativeEditErr, errs.ErrConflict) {
		t.Fatalf("DRAFT edit silently healed alternate selected profile: %v", alternativeEditErr)
	}
	selectionPlan := propose(alternative, "selection-source-draft")
	selectionEdited := selectionPlan.Operations[0]
	selectionEdited.Parameters = cloneAssistantFields(selectionEdited.Parameters)
	selectionEdited.Parameters["runtimeProfileRef"] = changed.Configuration.RuntimeProfileRef
	selectionEdited.Parameters["reasoningEffort"] = "medium"
	selectionNew := execute(command.UpdateAssistantPlan, "selection-explicit-edit", selectionPlan, command.AssistantPlanDraftInput{PlanRef: selectionPlan.Ref, Summary: selectionPlan.Summary, Operations: []entity.AssistantPlanOperation{selectionEdited}})
	validate(*selectionNew.Plan, "selection-explicit-validate")
	catalogDriftPlan := propose(alternative, "catalog-source-draft")
	catalogBefore := readCatalog("MODELS")
	accountRef := view.Configuration.ProviderPolicy.AccountCandidates[0].AccountRef
	resolvedCatalogOwner, catalogOwnerErr := r.ResolvePrincipal(ctx, owner)
	if catalogOwnerErr != nil {
		t.Fatal(catalogOwnerErr)
	}
	catalogOwnerScope, catalogOwnerErr := r.resolveScope(ctx, resolvedCatalogOwner)
	if catalogOwnerErr != nil {
		t.Fatal(catalogOwnerErr)
	}
	if _, advanceErr := r.pool.Exec(ctx, queryCatalogFixtureAdvanceAccount, catalogOwnerScope.organizationID, accountRef); advanceErr != nil {
		t.Fatal(advanceErr)
	}
	seedObservedCatalogFixture(t, ctx, r, func(observation *platformrepo.ProviderModelCatalogObservation) {
		if observation.AccountRef == accountRef {
			observation.Models[0].DefaultReasoningEffort = "low"
			for _, entry := range catalogBefore.Entries {
				if entry.Model == "gpt-5" && entry.DefaultReasoningEffort == "low" {
					observation.Models[0].DefaultReasoningEffort = "high"
				}
			}
		}
	})
	catalogAfter := readCatalog("MODELS")
	if len(catalogBefore.Entries) == 0 || len(catalogAfter.Entries) == 0 || catalogBefore.Entries[0].CatalogRevision == catalogAfter.Entries[0].CatalogRevision || catalogBefore.Entries[0].CatalogDigest == catalogAfter.Entries[0].CatalogDigest {
		t.Fatal("verification did not advance selected immutable account catalog")
	}
	catalogEdited := catalogDriftPlan.Operations[0]
	catalogEdited.Parameters = cloneAssistantFields(catalogEdited.Parameters)
	catalogEdited.Parameters["reasoningEffort"] = "medium"
	_, catalogEditErr := service.Execute(ctx, command.Command{Kind: command.UpdateAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "-catalog-drift-edit", ExpectedVersion: &catalogDriftPlan.Version}, Payload: command.AssistantPlanDraftInput{PlanRef: catalogDriftPlan.Ref, Summary: catalogDriftPlan.Summary, Operations: []entity.AssistantPlanOperation{catalogEdited}}})
	if !errors.Is(catalogEditErr, errs.ErrConflict) {
		t.Fatalf("DRAFT edit silently healed selected account catalog: %v", catalogEditErr)
	}
	catalogRefreshView, catalogRefreshErr := service.GetAgentRuntimeConfiguration(ctx, owner, agentRef)
	if catalogRefreshErr != nil {
		t.Fatal(catalogRefreshErr)
	}
	catalogRefreshOverlay, catalogRefreshErr := runtimecontract.ParseConfigOverlay(catalogRefreshView.PublishedOverlay.Content)
	if catalogRefreshErr != nil {
		t.Fatal(catalogRefreshErr)
	}
	catalogRefresh := operation
	catalogRefresh.Parameters = map[string]any{"agentRef": agentRef, "runtimeProfileRef": catalogRefreshView.Configuration.RuntimeProfileRef,
		"model": catalogRefreshView.Configuration.Model, "reasoningEffort": catalogRefreshOverlay.ModelReasoningEffort,
		"providerPolicyMode": catalogRefreshView.Configuration.ProviderPolicy.Mode, "providerAccounts": minimalAssistantRuntimeAccounts(catalogRefreshView.Configuration.ProviderPolicy.AccountCandidates)}
	catalogRefreshPlan := propose(catalogRefresh, "catalog-pins-refresh")
	if len(catalogRefreshPlan.Operations) != 1 || assistantJSONEqual(catalogRefreshView.Configuration.ProviderPolicy.AccountCandidates, catalogRefreshPlan.Operations[0].Parameters["providerCatalogPins"]) {
		t.Fatal("same settings with advanced catalog pins were discarded as a no-op")
	}
	operation.Parameters["reasoningEffort"] = "medium"
	if sourceScope == "SYSTEM" {
		compoundModel := operation
		compoundModel.Parameters = cloneAssistantFields(operation.Parameters)
		compoundImage := entity.AssistantPlanOperation{Key: "compound-image", Type: "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", Title: "Prepare compound image", Summary: "Synthetic compound model and image", Parameters: map[string]any{"systemAssistantRef": agentRef, "name": "Compound assistant image", "environmentKey": "promotion", "dockerfile": ""}}
		compound, compoundErr := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: prefix + "-compound-model-image"}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: generation, Summary: "Synthetic compound model and image", Operations: []entity.AssistantPlanOperation{compoundModel, compoundImage}}})
		if compoundErr != nil {
			t.Fatal(compoundErr)
		}
		apply(validate(*compound.Plan, "compound-model-image-validate"), "compound-model-image-apply")
		compoundModel.Parameters["reasoningEffort"] = "low"
		compoundImage.Parameters = cloneAssistantFields(compoundImage.Parameters)
		compoundImage.Parameters["name"] = "Reverse compound assistant image"
		reverse, reverseErr := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: prefix + "-compound-image-model"}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: generation, Summary: "Synthetic image then model", Operations: []entity.AssistantPlanOperation{compoundImage, compoundModel}}})
		if reverseErr != nil {
			t.Fatal(reverseErr)
		}
		apply(validate(*reverse.Plan, "compound-image-model-validate"), "compound-image-model-apply")
		compoundModel.Parameters["reasoningEffort"] = "medium"
		compoundImage.Parameters = cloneAssistantFields(compoundImage.Parameters)
		compoundImage.Parameters["name"] = "Rolled back compound assistant image"
		rollbackProposal, rollbackErr := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: prefix + "-compound-rollback"}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: generation, Summary: "Synthetic atomic rollback", Operations: []entity.AssistantPlanOperation{compoundImage, compoundModel}}})
		if rollbackErr != nil {
			t.Fatal(rollbackErr)
		}
		rollbackPlan := validate(*rollbackProposal.Plan, "compound-rollback-validate")
		resolved, err := r.ResolvePrincipal(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		actorScope, err := r.resolveScope(ctx, resolved)
		if err != nil {
			t.Fatal(err)
		}
		var beforeEffects, afterEffects string
		if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, actorScope.organizationID).Scan(&beforeEffects); err != nil {
			t.Fatal(err)
		}
		if _, err := r.pool.Exec(ctx, queryAssistantConfigurationComponentProfileBump, assistantString(compoundModel.Parameters, "runtimeProfileRef")); err != nil {
			t.Fatal(err)
		}
		rollbackResult := execute(command.ApplyAssistantPlan, "compound-rollback-apply", rollbackPlan, command.AssistantPlanInput{PlanRef: rollbackPlan.Ref, Revision: rollbackPlan.Revision})
		if rollbackResult.Plan == nil || rollbackResult.Plan.State != "STALE" || rollbackResult.PlanReceipt.Outcome != "CONFLICT" {
			t.Fatal("external drift was healed by previous in-plan effects")
		}
		if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, actorScope.organizationID).Scan(&afterEffects); err != nil {
			t.Fatal(err)
		}
		var beforeCounts, afterCounts []int64
		if json.Unmarshal([]byte(beforeEffects), &beforeCounts) != nil || json.Unmarshal([]byte(afterEffects), &afterCounts) != nil || len(beforeCounts) != 5 || len(afterCounts) != 5 || beforeCounts[2] != afterCounts[2] || beforeCounts[3] != afterCounts[3] {
			t.Fatal("later failed operation left an earlier image recipe/build effect")
		}
		operation.Parameters["reasoningEffort"] = "low"
		changed, err = service.GetAgentRuntimeConfiguration(ctx, owner, agentRef)
		if err != nil {
			t.Fatal(err)
		}
	}
	testAssistantProjectConfiguration(t, ctx, r, service, owner, worker, reader, lease, sourceScope)
	changed, err = service.GetAgentRuntimeConfiguration(ctx, owner, agentRef)
	if err != nil {
		t.Fatal(err)
	}
	operation.Parameters["reasoningEffort"] = "low"
	if currentOverlay, parseErr := runtimecontract.ParseConfigOverlay(changed.PublishedOverlay.Content); parseErr == nil && currentOverlay.ModelReasoningEffort == "low" {
		operation.Parameters["reasoningEffort"] = "high"
	}
	stale := propose(operation, "stale-draft-proposal")
	_, err = service.Execute(ctx, command.Command{Kind: command.CreateConfigOverlayDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "-manual-overlay", ExpectedVersion: &changed.AgentVersion}, Payload: command.ConfigOverlayInput{AgentRef: agentRef, Content: "model_reasoning_effort = \"low\"\n"}})
	if err != nil {
		t.Fatal(err)
	}
	invalidated := execute(command.ValidateAssistantPlan, "stale-draft-validate", stale, command.AssistantPlanInput{PlanRef: stale.Ref, Revision: stale.Revision})
	if invalidated.Plan == nil || invalidated.Plan.State == "VALID" {
		t.Fatal("changed manual draft was silently overwritten")
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: prefix + "-preexisting-manual-draft"}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: generation, Summary: operation.Summary, Operations: []entity.AssistantPlanOperation{operation}}})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("preexisting manual draft superseded by prepare: %v", err)
	}
	if sourceScope == "PROJECT" {
		testAssistantProjectImageTemplateSelection(t, ctx, r, service, owner, lease)
		system, err := service.GetSystemAssistant(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		operation.Parameters["agentRef"] = system.Ref
		_, err = service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: prefix + "-deny-system-config"}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: generation, Summary: operation.Summary, Operations: []entity.AssistantPlanOperation{operation}}})
		if !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("project helper configured system helper: %v", err)
		}
		return
	}
	image := entity.AssistantPlanOperation{Key: "image", Type: "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", Title: "Prepare system image", Summary: "Synthetic organization image draft", Parameters: map[string]any{"systemAssistantRef": agentRef, "name": "Assistant configuration image", "environmentKey": "promotion", "dockerfile": ""}}
	imagePlan := propose(image, "image-proposal")
	if imagePlan.Operations[0].Target.Kind != "ROLE_IMAGE_RECIPE" || imagePlan.Operations[0].Action != "CREATE" || assistantString(imagePlan.Operations[0].After, "scopeKind") != "ORGANIZATION" || assistantString(imagePlan.Operations[0].After, "organizationRef") == "" {
		t.Fatal("image plan lacks server owner tuple")
	}
	editedImage := imagePlan.Operations[0]
	editedImage.Parameters = cloneAssistantFields(editedImage.Parameters)
	editedImage.Parameters["name"] = "Edited assistant configuration image"
	imagePlan = *execute(command.UpdateAssistantPlan, "image-edit", imagePlan, command.AssistantPlanDraftInput{PlanRef: imagePlan.Ref, Summary: imagePlan.Summary, Operations: []entity.AssistantPlanOperation{editedImage}}).Plan
	imageValidated := validate(imagePlan, "image-validate")
	imageApplied := apply(imageValidated, "image-apply")
	if imageApplied.PlanReceipt == nil || len(imageApplied.PlanReceipt.Operations) != 1 {
		t.Fatal("image receipt missing")
	}
	recipeRef := imageApplied.PlanReceipt.Operations[0].ResourceRef
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := r.GetOrganization(ctx, resolved, recipeRef)
	if err != nil || detail.Recipe.ScopeKind != "ORGANIZATION" || detail.Recipe.ProjectRef != "" || len(detail.Builds) != 1 || detail.Builds[0].ConfigurationRevisionRef == "" {
		t.Fatalf("canonical managed org build not created: %v", err)
	}
	if len(readCatalog("ROLE_IMAGE_RECIPES").Entries) == 0 {
		t.Fatal("owned recipe absent from discovery")
	}
	image.Parameters = map[string]any{"systemAssistantRef": agentRef, "recipeRef": recipeRef, "name": "Updated assistant configuration image", "environmentKey": "promotion", "dockerfile": detail.Recipe.Input.Dockerfile + "\nRUN echo synthetic\n"}
	image.Type = "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
	if _, checkErr := r.roleImageCatalogResolver(entity.RoleEnvironmentSelection{EnvironmentKey: "promotion", Dockerfile: assistantString(image.Parameters, "dockerfile")}); checkErr != nil {
		t.Fatalf("synthetic update Dockerfile invalid (sourceAvailable=%t, bytes=%d): %v", detail.Recipe.SourceAvailable, len(detail.Recipe.Input.Dockerfile), checkErr)
	}
	debugScope, debugErr := r.resolveScope(ctx, resolved)
	if debugErr != nil {
		t.Fatal(debugErr)
	}
	debugTx, debugErr := r.pool.Begin(ctx)
	if debugErr != nil {
		t.Fatal(debugErr)
	}
	hydrated, debugErr := r.hydrateSystemAssistantImage(ctx, debugTx, debugScope, image)
	if debugErr == nil {
		hydrated, debugErr = normalizeAssistantOperation(hydrated)
		if debugErr != nil {
			t.Errorf("normalize image update: %v", debugErr)
		}
	} else {
		t.Errorf("hydrate image update: %v", debugErr)
	}
	if debugErr == nil {
		_, debugErr = assistantOperationCommand(hydrated)
		if debugErr != nil {
			t.Errorf("command image update: %v", debugErr)
		}
	}
	_ = debugTx.Rollback(ctx)
	if debugErr != nil {
		t.Fatal("image update typed pipeline is invalid")
	}
	updatePlan := propose(image, "image-update-proposal")
	updateValidated := validate(updatePlan, "image-update-validate")
	updateApplied := apply(updateValidated, "image-update-apply")
	updatedDetail, err := r.GetOrganization(ctx, resolved, recipeRef)
	if err != nil || updatedDetail.Recipe.Generation != detail.Recipe.Generation+1 || len(updatedDetail.Builds) != 2 {
		t.Fatalf("canonical image update lost generation: %v", err)
	}
	updateReplay := execute(command.ApplyAssistantPlan, "image-update-apply", updateValidated, command.AssistantPlanInput{PlanRef: updateValidated.Ref, Revision: updateValidated.Revision})
	if !assistantJSONEqual(updateReplay.PlanReceipt, updateApplied.PlanReceipt) {
		t.Fatal("image update replay duplicated generation")
	}
	image.Parameters["name"] = "Owner refreshed assistant image"
	imageStalePlan := validate(propose(image, "image-stale-proposal"), "image-stale-validate")
	image.Parameters["name"] = "Concurrent assistant image configuration"
	concurrentImage := validate(propose(image, "image-concurrent-proposal"), "image-concurrent-validate")
	apply(concurrentImage, "image-concurrent-apply")
	imageConflict := execute(command.ApplyAssistantPlan, "image-stale-apply", imageStalePlan, command.AssistantPlanInput{PlanRef: imageStalePlan.Ref, Revision: imageStalePlan.Revision})
	if imageConflict.Plan == nil || imageConflict.Plan.State != "STALE" || imageConflict.PlanReceipt == nil || imageConflict.PlanReceipt.Outcome != "CONFLICT" {
		t.Fatal("image stale dependency did not produce a conflict receipt")
	}
	imageRefreshed := execute(command.UpdateAssistantPlan, "image-refresh-new-revision", *imageConflict.Plan, command.AssistantPlanDraftInput{PlanRef: imageConflict.Plan.Ref, Summary: imageConflict.Plan.Summary, Operations: imageConflict.Plan.Operations})
	if imageRefreshed.Plan == nil || imageRefreshed.Plan.State != "DRAFT" || imageRefreshed.Plan.Revision != imageStalePlan.Revision+1 || assistantJSONEqual(imageRefreshed.Plan.Operations[0].Before, imageStalePlan.Operations[0].Before) {
		t.Fatal("STALE image update did not refresh its versioned snapshot")
	}
	apply(validate(*imageRefreshed.Plan, "image-refresh-validate"), "image-refresh-apply")
	// Обновление опубликованного каталога не переписывает исходный recipe.
	// Только новый owner-confirmed UPDATE создаёт immutable input поколения 2.
	beforeRepair, err := r.GetOrganization(ctx, resolved, recipeRef)
	if err != nil {
		t.Fatal(err)
	}
	nameOnly := image
	nameOnly.Parameters = map[string]any{"systemAssistantRef": agentRef, "recipeRef": recipeRef, "name": "Name-only custom image"}
	nameOnlyPlan := propose(nameOnly, "image-name-only-custom-proposal")
	if assistantImageDockerfile(nameOnlyPlan.Operations[0].After) != beforeRepair.Recipe.Input.Dockerfile {
		t.Fatal("name-only proposal discarded the existing custom Dockerfile")
	}
	environments := catalog.List()
	environments[0].Input.SourceRevision = "revision-2"
	environments[0].Input.SourceSHA256 = strings.Repeat("c", 64)
	environments[0].Input.ContextRef = "oci://registry.internal/role-input@sha256:" + strings.Repeat("c", 64)
	environments[0].Input.ContextSHA256 = strings.Repeat("c", 64)
	environments[0].Input.ToolchainSHA256 = strings.Repeat("c", 64)
	environments[0].Input.BaseImageDigest = "sha256:" + strings.Repeat("c", 64)
	repairCatalog, err := roleimageservice.NewCatalog(environments)
	if err != nil {
		t.Fatal(err)
	}
	r.ConfigureRoleImageCatalog(repairCatalog)
	freshTemplate, err := repairCatalog.Resolve(entity.RoleEnvironmentSelection{EnvironmentKey: "promotion"})
	if err != nil || freshTemplate.Dockerfile == beforeRepair.Recipe.Input.Dockerfile {
		t.Fatal("changed base did not produce a fresh server Dockerfile template")
	}
	for index, parameters := range []map[string]any{
		nameOnly.Parameters,
		{"systemAssistantRef": agentRef, "recipeRef": recipeRef, "environmentKey": "promotion", "dockerfile": beforeRepair.Recipe.Input.Dockerfile},
	} {
		invalid := image
		invalid.Parameters = parameters
		_, denied := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: prefix + "-image-old-base-" + string(rune('a'+index))},
			Payload:  command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: generation, Summary: invalid.Summary, Operations: []entity.AssistantPlanOperation{invalid}}})
		if !errors.Is(denied, errs.ErrInvalid) {
			t.Fatal("old explicit or name-only Dockerfile was silently repinned")
		}
	}
	repairImage := image
	repairImage.Parameters = map[string]any{"systemAssistantRef": agentRef, "recipeRef": recipeRef, "environmentKey": "promotion"}
	callerPinned := repairImage
	callerPinned.Parameters = cloneAssistantFields(repairImage.Parameters)
	callerPinned.Parameters["specSha256"] = strings.Repeat("c", 64)
	_, callerPinErr := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: prefix + "-image-catalog-caller-pin"},
		Payload:  command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: generation, Summary: repairImage.Summary, Operations: []entity.AssistantPlanOperation{callerPinned}}})
	if !errors.Is(callerPinErr, errs.ErrInvalid) {
		t.Fatal("caller assigned server-owned build specification pin")
	}
	repairPlan := propose(repairImage, "image-catalog-repair-proposal")
	repairOperation := repairPlan.Operations[0]
	if assistantString(repairOperation.Before, "specSha256") != beforeRepair.Recipe.SpecSHA256 ||
		assistantString(repairOperation.After, "specSha256") == beforeRepair.Recipe.SpecSHA256 ||
		!exactSHA256(assistantString(repairOperation.After, "specSha256")) ||
		assistantString(repairOperation.After, "name") != beforeRepair.Recipe.Name ||
		assistantImageDockerfile(repairOperation.Before) != beforeRepair.Recipe.Input.Dockerfile ||
		assistantImageDockerfile(repairOperation.After) != freshTemplate.Dockerfile {
		t.Fatal("same textual selection did not pin an explicit immutable catalog repair")
	}
	stillFrozen, err := r.GetOrganization(ctx, resolved, recipeRef)
	if err != nil || !assistantJSONEqual(stillFrozen.Recipe.Input, beforeRepair.Recipe.Input) {
		t.Fatal("proposal auto-repinned the saved recipe")
	}
	for _, field := range []string{"specSha256", "organizationRef", "recipeRef"} {
		forged := repairOperation
		forged.Parameters = cloneAssistantFields(repairOperation.Parameters)
		forged.Parameters[field] = strings.Repeat("d", 64)
		v := repairPlan.Version
		_, denied := service.Execute(ctx, command.Command{Kind: command.UpdateAssistantPlan, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: prefix + "-image-repair-forged-" + field, ExpectedVersion: &v},
			Payload:  command.AssistantPlanDraftInput{PlanRef: repairPlan.Ref, Summary: repairPlan.Summary, Operations: []entity.AssistantPlanOperation{forged}}})
		if !errors.Is(denied, errs.ErrForbidden) {
			t.Fatalf("caller changed immutable repair pin %s: %v", field, denied)
		}
	}
	repairValidated := validate(repairPlan, "image-catalog-repair-validate")
	wrongRepairVersion := repairValidated.Version + 99
	_, wrongRepairErr := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: prefix + "-image-catalog-repair-wrong-version", ExpectedVersion: &wrongRepairVersion},
		Payload:  command.AssistantPlanInput{PlanRef: repairValidated.Ref, Revision: repairValidated.Revision}})
	if !errors.Is(wrongRepairErr, errs.ErrVersionMismatch) {
		t.Fatal("repair ignored confirmed plan OCC version")
	}
	draftRepair := propose(repairImage, "image-catalog-repair-draft-drift")
	// Каталог, изменённый после человеческого подтверждения, не усыновляется.
	environments[0].Input.SourceRevision = "revision-3"
	environments[0].Input.SourceSHA256 = strings.Repeat("d", 64)
	environments[0].Input.ContextSHA256 = strings.Repeat("d", 64)
	environments[0].Input.ToolchainSHA256 = strings.Repeat("d", 64)
	newCatalog, err := roleimageservice.NewCatalog(environments)
	if err != nil {
		t.Fatal(err)
	}
	r.ConfigureRoleImageCatalog(newCatalog)
	draftEditVersion := draftRepair.Version
	_, draftEditErr := service.Execute(ctx, command.Command{Kind: command.UpdateAssistantPlan, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: prefix + "-image-catalog-repair-draft-heal", ExpectedVersion: &draftEditVersion},
		Payload:  command.AssistantPlanDraftInput{PlanRef: draftRepair.Ref, Summary: draftRepair.Summary, Operations: draftRepair.Operations}})
	if !errors.Is(draftEditErr, errs.ErrConflict) {
		t.Fatal("ordinary DRAFT edit silently refreshed changed catalog pins")
	}
	drifted := execute(command.ApplyAssistantPlan, "image-catalog-repair-drift", repairValidated, command.AssistantPlanInput{PlanRef: repairValidated.Ref, Revision: repairValidated.Revision})
	if drifted.Plan == nil || drifted.Plan.State != "STALE" || drifted.PlanReceipt == nil || drifted.PlanReceipt.Outcome != "CONFLICT" {
		t.Fatal("changed catalog silently replaced confirmed build input")
	}
	stillFrozen, err = r.GetOrganization(ctx, resolved, recipeRef)
	if err != nil || !assistantJSONEqual(stillFrozen.Recipe.Input, beforeRepair.Recipe.Input) || len(stillFrozen.Builds) != len(beforeRepair.Builds) {
		t.Fatal("stale repair mutated generation or enqueued a build")
	}
	freshRepair := execute(command.UpdateAssistantPlan, "image-catalog-repair-new-revision", *drifted.Plan, command.AssistantPlanDraftInput{PlanRef: drifted.Plan.Ref, Summary: drifted.Plan.Summary, Operations: drifted.Plan.Operations})
	if freshRepair.Plan == nil || freshRepair.Plan.Revision != repairPlan.Revision+1 || freshRepair.Plan.State != "DRAFT" ||
		assistantString(freshRepair.Plan.Operations[0].After, "specSha256") == assistantString(repairOperation.After, "specSha256") {
		t.Fatal("explicit STALE repair did not freeze fresh server input in a new revision")
	}
	freshValidated := validate(*freshRepair.Plan, "image-catalog-repair-fresh-validate")
	repaired := apply(freshValidated, "image-catalog-repair-fresh-apply")
	afterRepair, err := r.GetOrganization(ctx, resolved, recipeRef)
	if err != nil || afterRepair.Recipe.Generation != beforeRepair.Recipe.Generation+1 ||
		afterRepair.Recipe.SpecSHA256 != assistantString(freshRepair.Plan.Operations[0].After, "specSha256") ||
		afterRepair.Recipe.Input.SourceRevision != "revision-3" || afterRepair.Recipe.Input.ToolchainSHA256 != strings.Repeat("d", 64) ||
		len(afterRepair.Builds) != len(beforeRepair.Builds)+1 || afterRepair.Builds[0].ConfigurationRevisionRef == "" || afterRepair.Builds[0].SpecSHA256 != afterRepair.Recipe.SpecSHA256 {
		t.Fatal("confirmed catalog repair lost immutable input/generation/managed lineage")
	}
	repairReplay := execute(command.ApplyAssistantPlan, "image-catalog-repair-fresh-apply", freshValidated, command.AssistantPlanInput{PlanRef: freshValidated.Ref, Revision: freshValidated.Revision})
	if !assistantJSONEqual(repaired.PlanReceipt, repairReplay.PlanReceipt) {
		t.Fatal("repair replay changed its atomic receipt")
	}
	// Новый запрос той же спецификации не выдаётся за meaningful repair.
	_, unchangedErr := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: prefix + "-image-catalog-repair-no-op"},
		Payload:  command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: generation, Summary: repairImage.Summary, Operations: []entity.AssistantPlanOperation{repairImage}}})
	if !errors.Is(unchangedErr, errs.ErrConflict) {
		t.Fatal("unchanged catalog repair was accepted")
	}
	ownerScope, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	var effectsBefore, effectsAfter string
	if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, ownerScope.organizationID).Scan(&effectsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(ctx, queryOrganizationImageComponentOwner, ownerScope.organizationID, ownerScope.actorID, "MEMBER"); err != nil {
		t.Fatal(err)
	}
	wrongVersion := updateValidated.Version + 99
	_, err = service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "-image-update-apply", ExpectedVersion: &wrongVersion}, Payload: command.AssistantPlanInput{PlanRef: updateValidated.Ref, Revision: updateValidated.Revision}})
	if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked image owner reached OCC/receipt: %v", err)
	}
	if err := r.pool.QueryRow(ctx, queryAssistantConfigurationComponentEffects, ownerScope.organizationID).Scan(&effectsAfter); err != nil || effectsBefore != effectsAfter {
		t.Fatal("revoked owner mutated audit/receipt/state")
	}
	if _, err := r.pool.Exec(ctx, queryOrganizationImageComponentOwner, ownerScope.organizationID, ownerScope.actorID, "OWNER"); err != nil {
		t.Fatal(err)
	}
}
