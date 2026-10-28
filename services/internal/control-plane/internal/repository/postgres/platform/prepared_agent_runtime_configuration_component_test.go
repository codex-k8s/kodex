package platform

import (
	"context"
	_ "embed"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"testing"
)

// queryPreparedAgentRuntimeOverlayFault installs a publication-only fault inside the exact isolated disposable database.
// queryPreparedAgentRuntimeOverlayFault устанавливает сбой только публикации в точной изолированной disposable-базе.
//
//go:embed testdata/sql/prepared_agent_runtime_overlay_fault.sql
var queryPreparedAgentRuntimeOverlayFault string

// queryPreparedAgentRuntimeOverlayFaultDrop removes the fixture fault before subsequent owner commands.
// queryPreparedAgentRuntimeOverlayFaultDrop снимает тестовый сбой перед следующими owner-командами.
//
//go:embed testdata/sql/prepared_agent_runtime_overlay_fault_drop.sql
var queryPreparedAgentRuntimeOverlayFaultDrop string

// queryPreparedAgentRuntimePermissionSwap removes only employee mutation permission from the exact disposable project membership while retaining plan management.
// queryPreparedAgentRuntimePermissionSwap отзывает лишь изменение сотрудника у точного проектного участника disposable-базы, сохраняя управление планами.
//
//go:embed testdata/sql/prepared_agent_runtime_permission_swap.sql
var queryPreparedAgentRuntimePermissionSwap string

// queryPreparedAgentRuntimePermissionRestore restores the original disposable role version after revocation assertions.
// queryPreparedAgentRuntimePermissionRestore возвращает исходную версию роли disposable-базы после проверки отзыва.
//
//go:embed testdata/sql/prepared_agent_runtime_permission_restore.sql
var queryPreparedAgentRuntimePermissionRestore string

// testPreparedAgentRuntimeConfigurationPipeline checks real project lease authority, ordinary target policy, owner confirmation, replay, drift, revocation and atomic publication without provider calls.
// testPreparedAgentRuntimeConfigurationPipeline проверяет полномочия реального проектного lease, обычную цель, подтверждение владельца, повторы, дрейф, отзыв доступа и атомарную публикацию без вызовов провайдера.
func testPreparedAgentRuntimeConfigurationPipeline(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner, worker, reader value.Principal, lease map[string]any, foreignProjectRef string) {
	t.Helper()
	const prefix = "prepared-agent-runtime-"
	create := func(projectRef, key string) entity.Agent {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: command.CreateAgent, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + key}, Payload: command.AgentInput{ProjectRef: projectRef, Name: key, Purpose: "Synthetic runtime configuration", RoleDescription: "Synthetic employee", Instructions: "Use only explicitly configured project resources."}})
		if err != nil || result.Agent == nil {
			t.Fatalf("create ordinary fixture: %v", err)
		}
		return *result.Agent
	}
	agent := create(stringMap(lease, "projectRef"), "employee")
	foreign := create(foreignProjectRef, "foreign")
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	initialOverlay, err := runtimecontract.ParseConfigOverlay(view.PublishedOverlay.Content)
	if err != nil {
		t.Fatal(err)
	}
	proposedEffort, editedEffort := "high", "medium"
	if initialOverlay.ModelReasoningEffort == proposedEffort {
		proposedEffort = "low"
	}
	if initialOverlay.ModelReasoningEffort == editedEffort {
		editedEffort = "low"
	}
	operation := entity.AssistantPlanOperation{Key: "model", Type: prepareAgentRuntimeConfiguration, Title: "Configure employee model", Summary: "Synthetic ordinary model", Parameters: map[string]any{"agentRef": agent.Ref, "runtimeProfileRef": view.Configuration.RuntimeProfileRef, "model": "gpt-5", "reasoningEffort": proposedEffort, "providerPolicyMode": view.Configuration.ProviderPolicy.Mode, "providerAccounts": minimalAssistantRuntimeAccounts(view.Configuration.ProviderPolicy.AccountCandidates)}}
	propose := func(op entity.AssistantPlanOperation, key string) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker, Mutation: value.Mutation{IdempotencyKey: prefix + key}, Payload: command.ProposeAssistantPlanInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Summary: op.Summary, Operations: []entity.AssistantPlanOperation{op}}})
	}
	draft := func(op entity.AssistantPlanOperation, key string) entity.AssistantPlan {
		t.Helper()
		result, err := propose(op, key)
		if err != nil || result.Plan == nil {
			t.Fatalf("propose %s: %v", key, err)
		}
		return *result.Plan
	}
	execute := func(kind command.Kind, plan entity.AssistantPlan, key string) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + key, ExpectedVersion: &plan.Version}, Payload: command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision}})
		if err != nil {
			t.Fatalf("%s %s: %v", kind, key, err)
		}
		return result
	}
	validate := func(plan entity.AssistantPlan, key string) entity.AssistantPlan {
		t.Helper()
		result := execute(command.ValidateAssistantPlan, plan, key)
		if result.Plan == nil || result.Plan.State != "VALID" {
			t.Fatalf("invalid ordinary plan: %+v", result.Plan)
		}
		return *result.Plan
	}
	for _, ref := range []string{foreign.Ref, stringMap(lease, "agentRef")} {
		denied := operation
		denied.Parameters = cloneAssistantFields(operation.Parameters)
		denied.Parameters["agentRef"] = ref
		if _, err := propose(denied, "deny-"+ref); !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("ordinary policy accepted foreign or assistant target: %v", err)
		}
	}
	old := operation
	old.Type = "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION"
	if _, err := propose(old, "old-self-denied"); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("self command accepted employee: %v", err)
	}
	seedAssistantAgentRuntimeInventoryFixture(t, ctx, r, service, owner, agent.Ref)
	catalog, err := service.ListAssistantConfigurationCatalog(ctx, reader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), entity.AssistantConfigurationCatalogRequest{Kind: "AGENT_RUNTIME_CONFIGURATION", AssistantRef: stringMap(lease, "agentRef"), EntityKind: "AGENT", EntityRef: agent.Ref})
	if err != nil || catalog.AgentRuntimeConfiguration == nil || catalog.ProjectRef != stringMap(lease, "projectRef") {
		t.Fatalf("safe employee discovery: %v", err)
	}
	plan := draft(operation, "draft")
	op := plan.Operations[0]
	if op.Parameters["targetKind"] != "AGENT" || op.Parameters["projectRef"] != stringMap(lease, "projectRef") || op.Parameters["assistantScope"] != nil || op.Parameters["assistantProfileRef"] != nil || op.Target.Ref != agent.Ref {
		t.Fatal("ordinary plan lost exact owner tuple")
	}
	unchanged, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil || !assistantJSONEqual(unchanged, view) {
		t.Fatal("proposal changed published state")
	}
	edited := op
	edited.Parameters = cloneAssistantFields(op.Parameters)
	edited.Parameters["reasoningEffort"] = editedEffort
	update, err := service.Execute(ctx, command.Command{Kind: command.UpdateAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "edit", ExpectedVersion: &plan.Version}, Payload: command.AssistantPlanDraftInput{PlanRef: plan.Ref, Summary: plan.Summary, Operations: []entity.AssistantPlanOperation{edited}}})
	if err != nil || update.Plan == nil {
		t.Fatalf("edit employee settings: %v", err)
	}
	valid := validate(*update.Plan, "validate")
	applied := execute(command.ApplyAssistantPlan, valid, "apply")
	changed, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil || applied.Plan == nil || applied.Plan.State != "APPLIED" || changed.Configuration.Ref == view.Configuration.Ref || changed.PublishedOverlay.Digest == view.PublishedOverlay.Digest {
		t.Fatalf("atomic model and overlay publication: %v", err)
	}
	overlay, err := runtimecontract.ParseConfigOverlay(changed.PublishedOverlay.Content)
	if err != nil || overlay.ModelReasoningEffort != editedEffort {
		t.Fatal("employee effort not published")
	}
	replay := execute(command.ApplyAssistantPlan, valid, "apply")
	if !assistantJSONEqual(replay.PlanReceipt, applied.PlanReceipt) {
		t.Fatal("lost ACK duplicated employee publication")
	}
	faultPlan := validate(draft(operation, "fault-draft"), "fault-validate")
	adminConfig, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
	applicationConfig := r.pool.Config().ConnConfig
	if err != nil || adminConfig.User != "postgres" || adminConfig.Database != "postgres" || adminConfig.Host != applicationConfig.Host || adminConfig.Port != applicationConfig.Port || !strings.HasPrefix(applicationConfig.Database, "kodex_assistant_test_") {
		t.Fatal("fault injection requires the exact disposable fixture administrator")
	}
	adminConfig.Database = applicationConfig.Database
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("connect exact disposable fault fixture")
	}
	defer func() { _ = admin.Close(context.WithoutCancel(ctx)) }()
	if _, err := admin.Exec(ctx, queryPreparedAgentRuntimeOverlayFault); err != nil {
		t.Fatal(err)
	}
	faultResult, faultErr := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "fault-apply", ExpectedVersion: &faultPlan.Version}, Payload: command.AssistantPlanInput{PlanRef: faultPlan.Ref, Revision: faultPlan.Revision}})
	if _, err := admin.Exec(ctx, queryPreparedAgentRuntimeOverlayFaultDrop); err != nil {
		t.Fatal(err)
	}
	afterFault, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil || !errors.Is(faultErr, errs.ErrUnavailable) || faultResult.Plan != nil || !assistantJSONEqual(afterFault, changed) {
		t.Fatal("overlay failure retained a partial runtime publication")
	}
	recovered := execute(command.ApplyAssistantPlan, faultPlan, "fault-apply")
	if recovered.Plan == nil || recovered.Plan.State != "APPLIED" {
		t.Fatal("transient overlay failure left an unrecoverable receipt")
	}
	changed, err = service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	operation.Parameters["reasoningEffort"] = editedEffort
	stale := validate(draft(operation, "stale-draft"), "stale-validate")
	if _, err := r.pool.Exec(ctx, queryAssistantConfigurationComponentProfileBump, changed.Configuration.RuntimeProfileRef); err != nil {
		t.Fatal(err)
	}
	staleResult := execute(command.ApplyAssistantPlan, stale, "stale-apply")
	afterStale, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil || staleResult.Plan == nil || staleResult.Plan.State != "STALE" || !assistantJSONEqual(afterStale, changed) {
		t.Fatal("stale dependency partially published employee runtime")
	}
	revoked := validate(draft(operation, "revoke-draft"), "revoke-validate")
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	actorScope, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	planManager := createRoleImageAccessRole(t, ctx, service, owner, prefix+"plan-manager", "Plan manager without employee mutation", []string{"project.view", "project.manage", "agent.view"}, []string{"PROJECT"})
	var previousProjectRole string
	if err := r.pool.QueryRow(ctx, queryPreparedAgentRuntimePermissionSwap, actorScope.organizationID, actorScope.actorID, stringMap(lease, "projectRef"), planManager.CurrentVersion.Ref).Scan(&previousProjectRole); err != nil {
		t.Fatal("swap exact disposable project permission", err)
	}
	if _, err := r.pool.Exec(ctx, queryOrganizationImageComponentOwner, actorScope.organizationID, actorScope.actorID, "MEMBER"); err != nil {
		t.Fatal(err)
	}
	_, replayRevokedErr := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "apply", ExpectedVersion: &valid.Version}, Payload: command.AssistantPlanInput{PlanRef: valid.Ref, Revision: valid.Revision}})
	_, validateRevokedErr := service.Execute(ctx, command.Command{Kind: command.ValidateAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "revoked-validate", ExpectedVersion: &revoked.Version}, Payload: command.AssistantPlanInput{PlanRef: revoked.Ref, Revision: revoked.Revision}})
	_, revokedErr := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "revoked-apply", ExpectedVersion: &revoked.Version}, Payload: command.AssistantPlanInput{PlanRef: revoked.Ref, Revision: revoked.Revision}})
	if _, err := r.pool.Exec(ctx, queryOrganizationImageComponentOwner, actorScope.organizationID, actorScope.actorID, "OWNER"); err != nil {
		t.Fatal(err)
	}
	if tag, err := r.pool.Exec(ctx, queryPreparedAgentRuntimePermissionRestore, actorScope.organizationID, actorScope.actorID, stringMap(lease, "projectRef"), previousProjectRole); err != nil || tag.RowsAffected() != 1 {
		t.Fatal("restore exact disposable project permission")
	}
	for _, deniedErr := range []error{replayRevokedErr, validateRevokedErr} {
		if !errors.Is(deniedErr, errs.ErrForbidden) && !errors.Is(deniedErr, errs.ErrNotFound) {
			t.Fatalf("revoked owner reached validation or replay: %v", deniedErr)
		}
	}
	if !errors.Is(revokedErr, errs.ErrForbidden) && !errors.Is(revokedErr, errs.ErrNotFound) {
		t.Fatalf("revoked owner applied employee runtime: %v", revokedErr)
	}
	beforeDraft, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.CreateConfigOverlayDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: prefix + "manual-overlay", ExpectedVersion: &beforeDraft.AgentVersion}, Payload: command.ConfigOverlayInput{AgentRef: agent.Ref, Content: "model_reasoning_effort = \"low\"\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := propose(operation, "deny-draft"); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("employee proposal overwrote manual draft: %v", err)
	}
}
