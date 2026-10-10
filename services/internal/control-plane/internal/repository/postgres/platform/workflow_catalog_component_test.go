package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneapi"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Каждая launch fixture получает pins тем же leased catalog, что native tool.
func workflowCatalogLaunchInput(t *testing.T, service *serviceplatform.Service, principal value.Principal, lease map[string]any, ref, task string) command.LaunchWorkflowInput {
	t.Helper()
	principal.Permission = "platform.runtime.execution.workflow.catalog"
	input := query.ExecutionWorkflowCatalog{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation")}
	for pages := 0; pages < 10; pages++ {
		result, err := service.GetExecutionWorkflowCatalog(t.Context(), principal, input)
		if err != nil {
			t.Fatal("execution workflow catalog", err)
		}
		if len(result.Items) > 10 {
			t.Fatal("catalog page exceeded limit")
		}
		for _, item := range result.Items {
			if item.WorkflowRef == ref {
				if item.PublishedRef == "" || !workflowSpecDigestPattern.MatchString(item.SpecDigest) || item.Readiness.RevisionRef != item.PublishedRef {
					t.Fatal("catalog pins missing")
				}
				return command.LaunchWorkflowInput{LeaseRef: input.LeaseRef, Fence: input.Fence, Generation: input.Generation, WorkflowRef: ref, Task: task, ExpectedPublishedRef: item.PublishedRef, ExpectedSpecDigest: item.SpecDigest, ExpectedWorkflowVersion: item.WorkflowVersion}
			}
		}
		if result.NextPageToken == "" {
			break
		}
		input.PageToken = result.NextPageToken
	}
	t.Fatal("published workflow absent from eligible catalog")
	return command.LaunchWorkflowInput{}
}

func TestExecutionWorkflowCatalogComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	reader := worker
	reader.Permission = "platform.runtime.execution.workflow.catalog"
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, payload any, version *int64) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "workflow-read-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s %s: %v", kind, key, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", command.ProjectInput{Name: "Workflow read fixture", Language: "ru"}, nil).Project
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "catalog-manager", "Manager")
	manager = *execute(command.ChangeAgentCapability, owner, "manager-cap", command.AgentBindingInput{AgentRef: manager.Ref, BindingRef: "platform.run.launch", Enabled: true}, &manager.Version).Agent
	coordinator := createLifecycleAgent(t, ctx, service, owner, project.Ref, "catalog-coordinator", "Coordinator")
	coordinator = *execute(command.ChangeAgentCapability, owner, "coordinator-cap", command.AgentBindingInput{AgentRef: coordinator.Ref, BindingRef: "platform.run.delegate", Enabled: true}, &coordinator.Version).Agent
	specialist := createLifecycleAgent(t, ctx, service, owner, project.Ref, "catalog-specialist", "Specialist")
	draft := entity.WorkflowVersion{Name: "Опубликованный процесс", Purpose: "Проверить полный защищённый snapshot", CoordinatorAgentRef: coordinator.Ref, Concurrency: 1, TimeoutSeconds: 3600, Instructions: "Общая инструкция", CompletionCriteria: "Проверенный результат", ResultSchema: map[string]any{}, Inputs: []entity.WorkflowInputField{{Key: "task", Label: "Задача", Type: "TEXT", DefaultValue: "Исходный ввод"}}, Steps: []entity.WorkflowStep{{Key: "step", Position: 1, Name: "Первый шаг", AgentRef: specialist.Ref, Instructions: "Прочитать полную конфигурацию.", ExpectedResult: "Проверенные pins", TimeoutSeconds: 900}}}
	workflow := execute(command.CreateWorkflow, owner, "workflow", command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &draft}, nil).Workflow
	workflow = execute(command.ValidateWorkflow, owner, "validate", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version).Workflow
	workflow = execute(command.PublishWorkflow, owner, "publish", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version).Workflow
	limited := contextProjectReader(t, ctx, r, service, owner, project.Ref, "WORKFLOW_CATALOG")
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: "Context reader WORKFLOW_CATALOG", Page: query.Page{Size: 20}}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatal("exact fixture subject unavailable")
	}
	bind := func(key string, permissions []string, scope entity.AccessScope) *entity.AccessBinding {
		t.Helper()
		role := execute(command.CreateAccessRole, owner, key+"-role", command.AccessRoleInput{Name: key, PermissionKeys: permissions, AllowedScopes: []string{scope.Kind}, ChangeComment: "Workflow read eligibility fixture"}, nil).AccessRole
		return execute(command.CreateAccessBinding, owner, key+"-binding", command.AccessBindingInput{SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.CurrentVersion.Ref, Scope: scope}, nil).AccessBinding
	}
	bind("catalog-origin", []string{"agent.view", "agent.launch", "workflow.view", "workflow.launch"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	visible := bind("catalog-all-runs", []string{"run.view"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref})
	parent := execute(command.LaunchRun, limited, "parent", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: manager.Ref}, Task: "Read published workflow."}, nil).Run
	leases := execute(command.ClaimExecution, worker, "claim", command.LeaseInput{WorkloadInstance: "workflow-catalog-fixture", Limit: 1}, nil).RuntimeItems
	if len(leases) != 1 || stringMap(leases[0], "runRef") != parent.Ref {
		t.Fatal("exact origin lease unavailable")
	}
	lease := leases[0]
	request := query.ExecutionWorkflowCatalog{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation")}
	catalog, err := service.GetExecutionWorkflowCatalog(ctx, reader, request)
	if err != nil || len(catalog.Items) != 1 {
		t.Fatalf("discovery: %v count=%d", err, len(catalog.Items))
	}
	entry := catalog.Items[0]
	pins := query.ExecutionWorkflowReadPins{WorkflowRef: entry.WorkflowRef, PublishedRef: entry.PublishedRef, SpecDigest: entry.SpecDigest, WorkflowVersion: entry.WorkflowVersion}
	full := request
	full.Publication = &query.ExecutionWorkflowPublicationRead{Pins: pins}
	publication, err := service.GetExecutionWorkflowCatalog(ctx, reader, full)
	if err != nil || publication.Publication == nil {
		t.Fatalf("publication: %v", err)
	}
	wire, err := controlplaneapi.DecodeWorkflowPublication(publication.Publication.ConfigurationJSON, publication.Publication.ConfigurationSHA256)
	if err != nil || wire.ProjectRef != project.Ref || wire.Steps[0].Instructions != draft.Steps[0].Instructions || wire.InputFields[0].DefaultValue != draft.Inputs[0].DefaultValue || wire.Instructions != draft.Instructions {
		t.Fatal("incomplete owner publication")
	}
	wrong := full
	wrong.Publication = &query.ExecutionWorkflowPublicationRead{Pins: pins}
	wrong.Publication.Pins.WorkflowVersion++
	if _, err = service.GetExecutionWorkflowCatalog(ctx, reader, wrong); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale pins: %v", err)
	}
	wrong.Publication.Pins = pins
	wrong.Publication.Pins.WorkflowRef = project.Ref
	if _, err = service.GetExecutionWorkflowCatalog(ctx, reader, wrong); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("unrelated resource: %v", err)
	}
	foreignProject := execute(command.CreateProject, owner, "foreign-project", command.ProjectInput{Name: "Foreign workflow scope", Language: "en"}, nil).Project
	foreignCoordinator := createLifecycleAgent(t, ctx, service, owner, foreignProject.Ref, "foreign-catalog-coordinator", "Coordinator")
	foreignCoordinator = *execute(command.ChangeAgentCapability, owner, "foreign-coordinator-cap", command.AgentBindingInput{AgentRef: foreignCoordinator.Ref, BindingRef: "platform.run.delegate", Enabled: true}, &foreignCoordinator.Version).Agent
	foreignSpecialist := createLifecycleAgent(t, ctx, service, owner, foreignProject.Ref, "foreign-catalog-specialist", "Specialist")
	foreignDraft := draft
	foreignDraft.CoordinatorAgentRef = foreignCoordinator.Ref
	foreignDraft.Steps = append([]entity.WorkflowStep{}, draft.Steps...)
	foreignDraft.Steps[0].AgentRef = foreignSpecialist.Ref
	foreignWorkflow := execute(command.CreateWorkflow, owner, "foreign-workflow", command.WorkflowInput{ProjectRef: foreignProject.Ref, Name: foreignDraft.Name, Purpose: foreignDraft.Purpose, CoordinatorAgentRef: foreignCoordinator.Ref, Draft: &foreignDraft}, nil).Workflow
	foreignWorkflow = execute(command.ValidateWorkflow, owner, "foreign-validate", command.WorkflowInput{Ref: foreignWorkflow.Ref}, &foreignWorkflow.Version).Workflow
	foreignWorkflow = execute(command.PublishWorkflow, owner, "foreign-publish", command.WorkflowInput{Ref: foreignWorkflow.Ref}, &foreignWorkflow.Version).Workflow
	wrong.Publication.Pins.WorkflowRef = foreignWorkflow.Ref
	if _, err = service.GetExecutionWorkflowCatalog(ctx, reader, wrong); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign project publication disclosed: %v", err)
	}
	var runs []*entity.Run
	for i := 0; i < 12; i++ {
		runs = append(runs, execute(command.LaunchRun, owner, fmt.Sprintf("active-%02d", i), command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Bounded advisory fixture"}, nil).Run)
	}
	active := request
	active.ActiveRuns = &query.ExecutionWorkflowActiveRunsRead{Pins: pins}
	first, err := service.GetExecutionWorkflowCatalog(ctx, reader, active)
	if err != nil || first.ActiveRuns == nil || len(first.ActiveRuns.Items) != 10 || first.ActiveRuns.NextPageToken == "" {
		t.Fatalf("active page: %v", err)
	}
	active.PageToken = first.ActiveRuns.NextPageToken
	second, err := service.GetExecutionWorkflowCatalog(ctx, reader, active)
	if err != nil || len(second.ActiveRuns.Items) != 2 || second.ActiveRuns.NextPageToken != "" {
		t.Fatalf("active EOF: %v", err)
	}
	seen := map[string]bool{}
	for _, item := range append(first.ActiveRuns.Items, second.ActiveRuns.Items...) {
		if seen[item.RunRef] || item.PublishedRef != pins.PublishedRef || item.SpecDigest != pins.SpecDigest {
			t.Fatal("active row publication mismatch")
		}
		seen[item.RunRef] = true
	}
	discovery := request
	discovery.PageToken = active.PageToken
	if _, err = service.GetExecutionWorkflowCatalog(ctx, reader, discovery); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("cross-mode cursor: %v", err)
	}
	oldPins := pins
	updated := draft
	updated.Name = "Обновлённая публикация"
	workflow = execute(command.UpdateWorkflow, owner, "update", command.WorkflowInput{Ref: workflow.Ref, Name: updated.Name, Purpose: updated.Purpose, CoordinatorAgentRef: updated.CoordinatorAgentRef, Draft: &updated}, &workflow.Version).Workflow
	workflow = execute(command.ValidateWorkflow, owner, "revalidate", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version).Workflow
	workflow = execute(command.PublishWorkflow, owner, "republish", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version).Workflow
	if _, err = service.GetExecutionWorkflowCatalog(ctx, reader, full); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("publication drift accepted: %v", err)
	}
	catalog, err = service.GetExecutionWorkflowCatalog(ctx, reader, request)
	if err != nil || len(catalog.Items) != 1 {
		t.Fatal("new current publication unavailable")
	}
	entry = catalog.Items[0]
	pins = query.ExecutionWorkflowReadPins{WorkflowRef: entry.WorkflowRef, PublishedRef: entry.PublishedRef, SpecDigest: entry.SpecDigest, WorkflowVersion: entry.WorkflowVersion}
	active.ActiveRuns = &query.ExecutionWorkflowActiveRunsRead{Pins: pins}
	if _, err = service.GetExecutionWorkflowCatalog(ctx, reader, active); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("cursor crossed publication commitment: %v", err)
	}
	active.PageToken = ""
	historical, err := service.GetExecutionWorkflowCatalog(ctx, reader, active)
	if err != nil || len(historical.ActiveRuns.Items) != 10 {
		t.Fatal("historical active roots unavailable")
	}
	for _, row := range historical.ActiveRuns.Items {
		if row.PublishedRef != oldPins.PublishedRef || row.SpecDigest != oldPins.SpecDigest {
			t.Fatal("active rows rebound to current publication")
		}
	}
	full.Publication = &query.ExecutionWorkflowPublicationRead{Pins: pins}
	execute(command.RevokeAccessBinding, owner, "revoke-all-runs", command.AccessBindingInput{BindingRef: visible.Ref}, &visible.Version)
	active.PageToken = ""
	hidden, err := service.GetExecutionWorkflowCatalog(ctx, reader, active)
	if err != nil || len(hidden.ActiveRuns.Items) != 0 || hidden.ActiveRuns.NextPageToken != "" {
		t.Fatalf("invisible runs disclosed: %v", err)
	}
	exact := bind("catalog-one-run", []string{"run.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: runs[11].Ref, ProjectRef: project.Ref})
	one, err := service.GetExecutionWorkflowCatalog(ctx, reader, active)
	if err != nil || len(one.ActiveRuns.Items) != 1 || one.ActiveRuns.Items[0].RunRef != runs[11].Ref || one.ActiveRuns.NextPageToken != "" {
		t.Fatalf("visibility must precede limit: %v", err)
	}
	execute(command.RevokeAccessBinding, owner, "revoke-one-run", command.AccessBindingInput{BindingRef: exact.Ref}, &exact.Version)
	if _, err = service.GetExecutionWorkflowCatalog(ctx, reader, full); err != nil {
		t.Fatalf("run visibility incorrectly controls publication: %v", err)
	}
	manager = *execute(command.ChangeAgentCapability, owner, "revoke-origin-cap", command.AgentBindingInput{AgentRef: manager.Ref, BindingRef: "platform.run.launch", Enabled: false}, &manager.Version).Agent
	if _, err = service.GetExecutionWorkflowCatalog(ctx, reader, full); !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked current capability: %v", err)
	}
	// Отзыв capability атомарно закрывает owner execution и его прежнюю lease.
	if _, err = service.GetExecutionWorkflowCatalog(ctx, reader, active); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("closed origin remained readable: %v", err)
	}
}
