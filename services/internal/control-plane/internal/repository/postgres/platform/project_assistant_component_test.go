package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/project_assistant_profile_pin_mutation.sql
var queryProjectAssistantProfilePinMutation string

//go:embed testdata/sql/project_assistant_profile_readback.sql
var queryProjectAssistantProfileReadback string

//go:embed testdata/sql/project_assistant_purge_graph.sql
var queryProjectAssistantPurgeGraph string

//go:embed testdata/sql/project_assistant_purge_prepare.sql
var queryProjectAssistantPurgePrepare string

//go:embed testdata/sql/project_assistant_purge_execute.sql
var queryProjectAssistantPurgeExecute string

//go:embed testdata/sql/project_assistant_purge_readback.sql
var queryProjectAssistantPurgeReadback string

//go:embed testdata/sql/project_assistant_archived_fixture.sql
var queryProjectAssistantArchivedFixture string

func TestProjectAssistantProfilesComponent(t *testing.T) {
	dsn := isolatedAssistantComponentDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var graphNodes, graphEdges int
	var graphNodeDigest, graphEdgeDigest string
	if err := pool.QueryRow(ctx, queryProjectAssistantPurgeGraph).Scan(&graphNodes, &graphNodeDigest, &graphEdges, &graphEdgeDigest); err != nil {
		t.Fatal("read project purge graph")
	}
	t.Logf("project purge graph: nodes=%d digest=%s edges=%d digest=%s", graphNodes, graphNodeDigest, graphEdges, graphEdgeDigest)
	// Точный FK-граф после immutable revisions и prepared content ledger:
	// owner closure включает revision, ledger и его immutable plan bindings.
	// Подмена состава графа по-прежнему закрыто отклоняется.
	if graphNodes != 104 || graphNodeDigest != "5cbc6bd6bb4a8508ffd75ed4b1dd6189" || graphEdges != 276 || graphEdgeDigest != "75343e68e299c44be699718099c79527" {
		t.Fatal("project assistant purge graph changed")
	}
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1", ContentSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles",
		DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, key string, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-" + key}, Payload: payload})
		if err != nil {
			t.Fatalf("execute %s (%s): %v", kind, key, err)
		}
		return result
	}
	firstProject := execute(command.CreateProject, "first-project", command.ProjectInput{Name: "First assistant project", Language: "en"}).Project
	secondProject := execute(command.CreateProject, "second-project", command.ProjectInput{Name: "Second assistant project", Language: "en"}).Project
	if !contains(firstProject.NextActions, "CREATE_PROJECT_ASSISTANT") || !contains(secondProject.NextActions, "CREATE_PROJECT_ASSISTANT") {
		t.Fatal("project management did not expose the bounded assistant creation action")
	}
	createProfile := func(projectRef, key string) entity.ProjectAssistantProfile {
		t.Helper()
		return *execute(command.CreateProjectAssistant, key, command.ProjectAssistantInput{
			ProjectRef: projectRef, Name: key, Purpose: "Configure this project", Instructions: "Use only explicitly configured project resources.",
		}).ProjectAssistant
	}
	first := createProfile(firstProject.Ref, "first-profile")
	second := createProfile(secondProject.Ref, "second-profile")
	freshProject, err := service.GetProject(ctx, owner, firstProject.Ref)
	if err != nil || contains(freshProject.NextActions, "CREATE_PROJECT_ASSISTANT") {
		t.Fatalf("assigned assistant still exposes an impossible create action: %v", err)
	}
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	ownerScope, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal(err)
	}
	readOnly, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	authorizeErr := repository.authorizeCommand(ctx, readOnly, ownerScope, command.Command{
		Kind: command.CreateAssistantConversation, Payload: command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: firstProject.Ref},
	})
	_ = readOnly.Rollback(ctx)
	if authorizeErr != nil {
		t.Fatalf("assistant profile authorization unexpectedly mutates a read-only transaction: %v", authorizeErr)
	}
	if first.Ref == second.Ref || first.AgentRef == second.AgentRef || first.ProjectRef != firstProject.Ref || second.ProjectRef != secondProject.Ref {
		t.Fatal("project assistant profiles are not isolated")
	}
	readback, err := service.GetProjectAssistant(ctx, owner, firstProject.Ref)
	if err != nil || readback.Ref != first.Ref {
		t.Fatalf("profile readback: %v", err)
	}
	replay := execute(command.CreateProjectAssistant, "first-profile", command.ProjectAssistantInput{
		ProjectRef: firstProject.Ref, Name: "first-profile", Purpose: "Configure this project", Instructions: "Use only explicitly configured project resources.",
	})
	if replay.ProjectAssistant == nil || replay.ProjectAssistant.Ref != first.Ref {
		t.Fatal("profile create replay changed assignment")
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CreateProjectAssistant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "project-assistant-duplicate"}, Payload: command.ProjectAssistantInput{
			ProjectRef: firstProject.Ref, Name: "Duplicate", Purpose: "Configure this project", Instructions: "Do not create a second assistant."},
	}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("second project assignment accepted: %v", err)
	}
	configuration, err := service.GetAgentRuntimeConfiguration(ctx, owner, first.AgentRef)
	if err != nil || configuration.Environment.ProjectRef != firstProject.Ref ||
		len(configuration.Environment.CurrentVersion.SecretDescriptors) != 0 {
		t.Fatalf("project assistant inherited foreign environment: %v", err)
	}
	createConversation := func(projectRef, key string) entity.AssistantConversation {
		t.Helper()
		return *execute(command.CreateAssistantConversation, key, command.AssistantConversationInput{
			AssistantScope: "PROJECT", ProjectRef: projectRef,
		}).Conversation
	}
	firstConversation := createConversation(firstProject.Ref, "first-conversation")
	secondConversation := createConversation(secondProject.Ref, "second-conversation")
	t.Run("assistant assignment survives disabled and historical archived states", func(t *testing.T) {
		agent, err := service.GetAgent(ctx, owner, second.AgentRef)
		if err != nil || contains(agent.NextActions, "ARCHIVE") || contains(agent.NextActions, "DELETE") {
			t.Fatalf("assistant exposes generic destruction: %v", err)
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.ArchiveAgent, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-deny-archive", ExpectedVersion: &agent.Version}, Payload: command.AgentInput{Ref: agent.Ref}}); !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("assistant generic archive accepted: %v", err)
		}
		disabled, err := service.Execute(ctx, command.Command{Kind: command.SetAgentEnabled, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-disable", ExpectedVersion: &agent.Version}, Payload: command.AgentInput{Ref: agent.Ref, Enabled: false}})
		if err != nil || disabled.Agent == nil {
			t.Fatalf("disable project assistant: %v", err)
		}
		profile, err := service.GetProjectAssistant(ctx, owner, secondProject.Ref)
		if err != nil || profile.State != "DISABLED" || profile.Ref != second.Ref {
			t.Fatalf("disabled assistant assignment: %v", err)
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-disabled-create"}, Payload: command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: secondProject.Ref}}); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("disabled profile opened new conversation: %v", err)
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.AddAssistantTurn, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-disabled-turn", ExpectedVersion: &secondConversation.Version}, Payload: command.AssistantTurnInput{ConversationRef: secondConversation.Ref, Content: "Disabled message", DeliveryMode: "QUEUE"}}); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("disabled profile accepted turn: %v", err)
		}
		// Оснастка воспроизводит историческое состояние, которое новая команда
		// архивирования теперь запрещает, без изменения live-данных.
		if _, err := pool.Exec(ctx, queryProjectAssistantArchivedFixture, second.AgentRef, "ARCHIVED", false); err != nil {
			t.Fatal(err)
		}
		profile, err = service.GetProjectAssistant(ctx, owner, secondProject.Ref)
		if err != nil || profile.State != "ARCHIVED" || profile.Ref != second.Ref {
			t.Fatalf("historical archived profile: %v", err)
		}
		project, err := service.GetProject(ctx, owner, secondProject.Ref)
		if err != nil || contains(project.NextActions, "CREATE_PROJECT_ASSISTANT") {
			t.Fatalf("archived assignment exposed replacement: %v", err)
		}
		if _, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-archived-create"}, Payload: command.AssistantConversationInput{AssistantScope: "PROJECT", ProjectRef: secondProject.Ref}}); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("archived profile opened conversation: %v", err)
		}
		if _, err := pool.Exec(ctx, queryProjectAssistantArchivedFixture, second.AgentRef, "READY", true); err != nil {
			t.Fatal(err)
		}
		history, _, err := service.ListAssistantConversations(ctx, owner, query.AssistantConversationFilter{AssistantScope: "PROJECT", AssistantRef: second.AgentRef})
		if err != nil || len(history) != 1 || history[0].AssistantProfileRef != second.Ref {
			t.Fatalf("profile lifecycle lost historical pin: %v", err)
		}
	})
	if firstConversation.AssistantRef != first.AgentRef || firstConversation.AssistantProfileRef != first.Ref || firstConversation.AssistantScope != "PROJECT" ||
		secondConversation.AssistantRef != second.AgentRef || firstConversation.SessionRef == secondConversation.SessionRef {
		t.Fatal("conversation lost the immutable assistant profile")
	}
	var targetRef, pinRef, scopeKind string
	if err := pool.QueryRow(ctx, queryProjectAssistantProfileReadback, firstConversation.Ref).Scan(&targetRef, &pinRef, &scopeKind); err != nil ||
		targetRef != first.AgentRef || pinRef != first.AgentRef || scopeKind != "PROJECT" {
		t.Fatalf("stored profile pin: %v", err)
	}
	if _, err := pool.Exec(ctx, queryProjectAssistantProfilePinMutation, firstConversation.Ref, second.AgentRef); err == nil {
		t.Fatal("conversation profile reassignment was accepted")
	}
	// Глобальный warm не наблюдался: project assistant не зависит от его heartbeat.
	turn := execute(command.AddAssistantTurn, "first-turn", command.AssistantTurnInput{
		ConversationRef: firstConversation.Ref, Content: "Synthetic project message", DeliveryMode: "QUEUE",
	}).Conversation
	if turn.AssistantRef != first.AgentRef || turn.AssistantScope != "PROJECT" {
		t.Fatal("turn changed assistant profile")
	}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "project-assistant-first-claim"},
		Payload:  command.LeaseInput{WorkloadInstance: "project-assistant-fixture", Limit: 10}})
	if err != nil {
		t.Fatalf("claim project assistant: %v", err)
	}
	var lease map[string]any
	for _, item := range claimed.RuntimeItems {
		if stringMap(item, "sessionRef") == firstConversation.SessionRef {
			lease = item
		}
	}
	if lease == nil || stringMap(lease, "assistantScope") != "PROJECT" || stringMap(lease, "assistantProfileRef") != first.Ref ||
		stringMap(lease, "agentRef") != first.AgentRef || stringMap(lease, "projectRef") != firstProject.Ref {
		t.Fatalf("fresh runtime lost project assistant pin: %#v", lease)
	}
	expectedTarget, err := service.GetAgentRuntimeConfiguration(ctx, owner, first.AgentRef)
	if err != nil {
		t.Fatal(err)
	}
	projectRun, err := service.GetRun(ctx, owner, stringMap(lease, "runRef"))
	if err != nil || projectRun.AssistantPin == nil {
		t.Fatalf("project run authoritative pin: %v", err)
	}
	pin := *projectRun.AssistantPin
	if pin.Scope != "PROJECT" || pin.OrganizationRef != expectedTarget.Environment.OrganizationRef || pin.ConversationRef != firstConversation.Ref ||
		pin.AssistantRef != first.AgentRef || pin.ProjectRef != firstProject.Ref || pin.ProfileRef != first.Ref ||
		projectRun.Target.Type != "SYSTEM_ASSISTANT" || projectRun.Target.Ref != first.AgentRef || projectRun.Target.Version != expectedTarget.AgentVersion ||
		expectedTarget.AgentVersion < 1 {
		t.Fatal("project run lost exact assistant owner/profile/target version")
	}
	searchReader := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.assistant.resources.search",
	}, "runtime-controller")
	results, _, err := service.SearchAssistantResources(ctx, searchReader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), secondProject.Name)
	if err != nil || len(results) != 0 {
		t.Fatalf("project assistant search escaped project scope: results=%#v err=%v", results, err)
	}
	t.Run("project helper model configuration and safe discovery", func(t *testing.T) {
		testAssistantConfigurationPipeline(t, ctx, repository, service, owner, worker, searchReader, lease, "PROJECT")
	})
	plan := executeWorkerAssistantPlan(t, ctx, service, worker, lease, "project-assistant-self-instructions", entity.AssistantPlanOperation{
		Key: "own-instructions", Type: "CREATE_INSTRUCTION_DRAFT", Title: "Prepare assistant instructions", Summary: "Prepare own instructions for human review",
		Parameters: map[string]any{"agentRef": first.AgentRef, "instructions": "Use only this project and ask for human publication."},
	})
	if plan.Plan == nil || plan.Plan.State != "DRAFT" || plan.Plan.Operations[0].Target.Ref != first.AgentRef {
		t.Fatal("project self-configuration did not prepare an exact draft")
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "project-assistant-deny-profile-create"}, Payload: command.ProposeAssistantPlanInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
			Summary: "Rejected project profile mutation", Operations: []entity.AssistantPlanOperation{{Key: "new-profile", Type: "CREATE_PROJECT_ASSISTANT", Title: "Denied",
				Parameters: map[string]any{"projectRef": secondProject.Ref, "name": "Denied", "purpose": "Denied", "instructions": "Denied"}}},
		}}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("project assistant was allowed to create another assistant profile: %v", err)
	}
	items, _, err := service.ListAssistantConversations(ctx, owner, query.AssistantConversationFilter{
		Filter: query.Filter{Page: query.Page{Size: 10}}, AssistantScope: "PROJECT", AssistantRef: first.AgentRef,
	})
	if err != nil || len(items) != 1 || items[0].Ref != firstConversation.Ref {
		t.Fatalf("scoped conversation list: %v", err)
	}
	t.Run("artifact metadata discovery", func(t *testing.T) {
		testAssistantArtifactSearch(t, ctx, repository, service, owner, worker, searchReader, lease, secondProject.Ref)
	})
	if _, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "project-assistant-missing-scope"}, Payload: command.AssistantConversationInput{},
	}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("implicit legacy scope accepted: %v", err)
	}
	t.Run("system assistant prepares an isolated project profile for human publication", func(t *testing.T) {
		prepareObservedWarmFixture(t, ctx, repository)
		system, err := service.GetSystemAssistant(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		warmWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
			ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report",
		}, "runtime-controller")
		if _, err := service.ReportWarmRuntime(ctx, warmWorker, command.WarmRuntimeInput{
			WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: system.DesiredRuntimeRevision, State: "READY",
		}); err != nil {
			t.Fatal(err)
		}
		project := execute(command.CreateProject, "planned-project", command.ProjectInput{Name: "Planned assistant project", Language: "en"}).Project
		conversation := execute(command.CreateAssistantConversation, "system-planner", command.AssistantConversationInput{
			AssistantScope: "SYSTEM",
		}).Conversation
		turn := execute(command.AddAssistantTurn, "system-planner-turn", command.AssistantTurnInput{
			ConversationRef: conversation.Ref, Content: "Synthetic isolated assistant setup", DeliveryMode: "QUEUE",
		}).Conversation
		claims, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-system-planner-claim"},
			Payload:  command.LeaseInput{WorkloadInstance: "project-assistant-system-planner", Limit: 10}})
		if err != nil {
			t.Fatal(err)
		}
		var lease map[string]any
		for _, item := range claims.RuntimeItems {
			if stringMap(item, "sessionRef") == turn.SessionRef {
				lease = item
			}
		}
		if lease == nil {
			t.Fatal("synthetic system planner was not admitted")
		}
		plan := executeWorkerAssistantPlan(t, ctx, service, worker, lease, "project-assistant-create-plan", entity.AssistantPlanOperation{
			Key: "create-project-profile", Type: "CREATE_PROJECT_ASSISTANT", Title: "Prepare project assistant", Summary: "Prepare an isolated assistant for this project",
			Parameters: map[string]any{"projectRef": project.Ref, "name": "Planned assistant", "purpose": "Configure this project", "instructions": "Use only explicitly selected project resources."},
		}).Plan
		if _, err := service.GetProjectAssistant(ctx, owner, project.Ref); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("model proposal executed without human publication: %v", err)
		}
		validated, err := service.Execute(ctx, command.Command{Kind: command.ValidateAssistantPlan, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-validate-plan", ExpectedVersion: &plan.Version},
			Payload:  command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision}})
		if err != nil || validated.Plan == nil || validated.Plan.State != "VALID" {
			t.Fatalf("validate project assistant plan: %#v %v", validated.Plan, err)
		}
		applied, err := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-apply-plan", ExpectedVersion: &validated.Plan.Version},
			Payload:  command.AssistantPlanInput{PlanRef: plan.Ref, Revision: validated.Plan.Revision}})
		if err != nil || applied.Plan == nil || applied.Plan.State != "APPLIED" {
			t.Fatalf("publish isolated assistant plan: %#v %v", applied.Plan, err)
		}
		profile, err := service.GetProjectAssistant(ctx, owner, project.Ref)
		if err != nil || profile.ProjectRef != project.Ref || profile.AgentRef == system.Ref {
			t.Fatalf("published profile reused system identity: %#v %v", profile, err)
		}
		t.Run("system helper image and model configuration", func(t *testing.T) {
			testAssistantConfigurationPipeline(t, ctx, repository, service, owner, worker, searchReader, lease, "SYSTEM")
		})
		t.Run("system project context remains distinct from helper target", func(t *testing.T) {
			source := execute(command.CreateProject, "system-source-project", command.ProjectInput{Name: "Separate system source project", Language: "en"}).Project
			conversation := execute(command.CreateAssistantConversation, "system-source-conversation", command.AssistantConversationInput{AssistantScope: "SYSTEM", ProjectRef: source.Ref, Context: entity.AssistantContextDescriptor{EntityKind: "PROJECT", EntityRef: source.Ref}}).Conversation
			turn := execute(command.AddAssistantTurn, "system-source-turn", command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic cross-project helper configuration", DeliveryMode: "QUEUE"}).Conversation
			claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "system-source-claim"}, Payload: command.LeaseInput{WorkloadInstance: "system-source-claim", Limit: 10}})
			if err != nil {
				t.Fatal(err)
			}
			var sourceLease map[string]any
			for _, item := range claimed.RuntimeItems {
				if stringMap(item, "sessionRef") == turn.SessionRef {
					sourceLease = item
				}
			}
			if sourceLease == nil {
				t.Fatal("system source project claim missing")
			}
			testAssistantProjectConfiguration(t, ctx, repository, service, owner, worker, searchReader, sourceLease, "SYSTEM", source.Ref)
		})
	})
	t.Run("project purge closes assistant graph without foreign deletion", func(t *testing.T) {
		project := execute(command.CreateProject, "purge-project", command.ProjectInput{Name: "Assistant purge project", Language: "en"}).Project
		profile := createProfile(project.Ref, "purge-profile")
		conversation := createConversation(project.Ref, "purge-conversation")
		active := execute(command.AddAssistantTurn, "purge-turn", command.AssistantTurnInput{ConversationRef: conversation.Ref, Content: "Synthetic queued turn", DeliveryMode: "QUEUE"}).Conversation
		if _, err := service.Execute(ctx, command.Command{Kind: command.PurgeProject, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-deny-active-purge", ExpectedVersion: &project.Version}, Payload: command.ProjectLifecycleInput{Ref: project.Ref}}); !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("active project accepted purge: %v", err)
		}
		trashed, err := service.Execute(ctx, command.Command{Kind: command.TrashProject, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-trash", ExpectedVersion: &project.Version}, Payload: command.ProjectLifecycleInput{Ref: project.Ref}})
		if err != nil || trashed.Project == nil {
			t.Fatalf("trash assistant project: %v", err)
		}
		pending, err := service.Execute(ctx, command.Command{Kind: command.PurgeProject, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "project-assistant-purge", ExpectedVersion: &trashed.Project.Version}, Payload: command.ProjectLifecycleInput{Ref: project.Ref}})
		if err != nil || pending.Project == nil || pending.Project.Lifecycle != "PURGE_PENDING" {
			t.Fatalf("request assistant purge: %v", err)
		}
		var digest string
		if err := pool.QueryRow(ctx, queryProjectAssistantPurgePrepare, project.Ref).Scan(&digest); err != nil {
			t.Fatalf("prepare synthetic cleanup receipt: %v", err)
		}
		var deleted int
		if err := pool.QueryRow(ctx, queryProjectAssistantPurgeExecute, project.Ref, digest).Scan(&deleted); err != nil || deleted == 0 {
			t.Fatalf("purge assistant graph: rows=%d err=%v", deleted, err)
		}
		var profiles, conversations, sessions, foreignProfiles, foreignConversations int
		if err := pool.QueryRow(ctx, queryProjectAssistantPurgeReadback, profile.Ref, conversation.Ref, active.SessionRef, first.Ref, firstConversation.Ref).Scan(&profiles, &conversations, &sessions, &foreignProfiles, &foreignConversations); err != nil || profiles != 0 || conversations != 0 || sessions != 0 || foreignProfiles != 1 || foreignConversations != 1 {
			t.Fatalf("purge crossed profile boundary: %v", err)
		}
	})
}

func executeWorkerAssistantPlan(t *testing.T, ctx context.Context, service *platformservice.Service, worker value.Principal, lease map[string]any, key string, operation entity.AssistantPlanOperation) command.Result {
	t.Helper()
	result, err := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.ProposeAssistantPlanInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
			Summary: operation.Summary, Operations: []entity.AssistantPlanOperation{operation},
		}})
	if err != nil {
		stage, category, index, _ := errs.AssistantPlanDiagnostic(err)
		t.Fatalf("prepare synthetic assistant plan %s: %v stage=%s category=%s index=%d", key, err, stage, category, index)
	}
	return result
}
