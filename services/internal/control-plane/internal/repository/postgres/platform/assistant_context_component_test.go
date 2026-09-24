package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func testAssistantContextAuthority(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	prepareObservedWarmFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.conversations.create"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "assistant-context-project"}, Payload: command.ProjectInput{Name: "Context authority", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create context project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "assistant-context-agent", "Private context agent")
	reader := contextProjectReader(t, ctx, repository, service, owner, project.Project.Ref, "ASSISTANT_CONTEXT")
	create := command.Command{Kind: command.CreateAssistantConversation, Principal: reader, Mutation: value.Mutation{IdempotencyKey: "assistant-context-create"}, Payload: command.AssistantConversationInput{ProjectRef: project.Project.Ref, Context: entity.AssistantContextDescriptor{EntityKind: "AGENT", EntityRef: agent.Ref, EntityName: "Forged context name", AllowedOperations: []string{"ARCHIVE_AGENT"}}}}
	if _, err := service.Execute(ctx, create); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("project reader opened private agent context: %v", err)
	}
	if _, err := service.GetAgent(ctx, reader, agent.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("single agent read bypassed context boundary: %v", err)
	}
	role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "assistant-context-view-role"}, Payload: command.AccessRoleInput{Name: "Context exact agent reader", PermissionKeys: []string{"agent.view"}, AllowedScopes: []string{"RESOURCE_INSTANCE"}, ChangeComment: "Exact context reader fixture"}})
	if err != nil || role.AccessRole == nil {
		t.Fatal(err)
	}
	resolvedReader, err := repository.ResolvePrincipal(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "assistant-context-view-binding"}, Payload: command.AccessBindingInput{SubjectKind: "USER", SubjectRef: resolvedReader.ActorID, RoleVersionRef: role.AccessRole.CurrentVersion.Ref, Scope: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: agent.Ref, ProjectRef: project.Project.Ref}}})
	if err != nil || binding.AccessBinding == nil {
		t.Fatalf("bind context reader: %v", err)
	}
	created, err := service.Execute(ctx, create)
	if err != nil || created.Conversation == nil {
		t.Fatalf("create authorized context: %v", err)
	}
	if created.Conversation.Context.EntityName != agent.Name || created.Conversation.Context.EntityVersion == nil || *created.Conversation.Context.EntityVersion != agent.Version || len(created.Conversation.Context.AllowedOperations) != 0 {
		t.Fatalf("context trusted caller metadata or operations: %#v", created.Conversation.Context)
	}
	listed, _, err := service.ListAssistantConversations(ctx, reader, query.Filter{ProjectRef: project.Project.Ref, Page: query.Page{Size: 1}})
	if err != nil || len(listed) != 1 || listed[0].Context.EntityName != agent.Name {
		t.Fatalf("context list: count=%d err=%v", len(listed), err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "assistant-context-revoke", ExpectedVersion: &binding.AccessBinding.Version}, Payload: command.AccessBindingInput{BindingRef: binding.AccessBinding.Ref}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, create); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked context receipt replay: %v", err)
	}
	listed, _, err = service.ListAssistantConversations(ctx, reader, query.Filter{ProjectRef: project.Project.Ref, Page: query.Page{Size: 1}})
	if err != nil || len(listed) != 0 {
		t.Fatalf("revoked context remained before pagination: count=%d err=%v", len(listed), err)
	}
	unknown := create
	unknown.Mutation.IdempotencyKey = "assistant-context-unknown"
	unknown.Payload = command.AssistantConversationInput{ProjectRef: project.Project.Ref, Context: entity.AssistantContextDescriptor{EntityKind: "UNKNOWN", EntityRef: agent.Ref}}
	if _, err := service.Execute(ctx, unknown); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("unknown context kind: %v", err)
	}
	configuration, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	file, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "context-file-upload"}, platformrepo.ArtifactUpload{ProjectRef: project.Project.Ref, FileName: "context.txt", MediaType: "text/plain", SizeBytes: 7, Reader: strings.NewReader("fixture")})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "context-connection"}, Payload: command.ConnectionInput{DefinitionKey: "synthetic", Name: "Context connection", PublicConfiguration: map[string]any{"journal": "context-connection"}}})
	if err != nil || connection.Connection == nil {
		t.Fatal(err)
	}
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	ownerScope, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal(err)
	}
	connectionTx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	connectionUpdate, err := repository.hydrateAssistantConnectionOperation(ctx, connectionTx, ownerScope,
		entity.AssistantPlanOperation{Type: "UPDATE_INTEGRATION_CONNECTION", Key: "context-connection-update", Title: "Update connection",
			Parameters: map[string]any{"connectionRef": connection.Connection.Ref, "name": "Renamed connection"}})
	if err != nil || connectionUpdate.Target.Ref != connection.Connection.Ref ||
		assistantString(connectionUpdate.Before, "name") != connection.Connection.Name ||
		assistantString(connectionUpdate.After, "name") != "Renamed connection" {
		_ = connectionTx.Rollback(ctx)
		t.Fatalf("hydrate exact connection update: %#v %v", connectionUpdate, err)
	}
	if err := connectionTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	draft := entity.WorkflowVersion{Name: "Context workflow", Purpose: "Context fixture", CoordinatorAgentRef: agent.Ref, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "Bounded result", ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "step", Position: 1, Name: "Step", AgentRef: agent.Ref, Instructions: "Complete fixture.", ExpectedResult: "Fixture result", TimeoutSeconds: 900}}}
	workflow, err := service.Execute(ctx, command.Command{Kind: command.CreateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "context-workflow"}, Payload: command.WorkflowInput{ProjectRef: project.Project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}})
	if err != nil || workflow.Workflow == nil {
		t.Fatal(err)
	}
	workflowTx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	workflowUpdate, err := repository.hydrateAssistantWorkflowOperation(ctx, workflowTx, ownerScope,
		project.Project.Ref, entity.AssistantPlanOperation{Type: "UPDATE_WORKFLOW", Key: "context-workflow-update",
			Title: "Update workflow", Summary: "Update workflow", Parameters: map[string]any{
				"workflowRef": workflow.Workflow.Ref, "name": "Renamed workflow"}})
	if err != nil || workflowUpdate.Target.Ref != workflow.Workflow.Ref ||
		assistantString(workflowUpdate.Before, "name") != workflow.Workflow.Name ||
		assistantString(workflowUpdate.After, "name") != "Renamed workflow" {
		_ = workflowTx.Rollback(ctx)
		t.Fatalf("hydrate exact workflow update: %#v %v", workflowUpdate, err)
	}
	matchingWorkflow, err := repository.assistantWorkflowUpdateSnapshotMatches(ctx, workflowTx, ownerScope,
		project.Project.Ref, workflowUpdate)
	if err != nil || !matchingWorkflow {
		_ = workflowTx.Rollback(ctx)
		t.Fatalf("workflow update snapshot mismatch: matching=%v err=%v", matchingWorkflow, err)
	}
	if err := workflowTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	schedule, err := service.Execute(ctx, command.Command{Kind: command.CreateSchedule, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-context-schedule"}, Payload: command.ScheduleInput{
			ProjectRef: project.Project.Ref, Name: "Context schedule", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref},
			Preset: "DAILY", TimeOfDay: "12:00", Timezone: "UTC", Input: map[string]any{},
			AutomationText: "Check context every day", SessionPolicy: "NEW_EACH_RUN", NotificationPolicy: "CONTROL_CENTER_ONLY",
		}})
	if err != nil || schedule.Schedule == nil {
		t.Fatalf("create context schedule: %v", err)
	}
	scheduleTx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	scheduleUpdate, err := repository.hydrateAssistantScheduleOperation(ctx, scheduleTx, ownerScope,
		project.Project.Ref, entity.AssistantPlanOperation{Type: "UPDATE_SCHEDULE", Key: "context-schedule-update",
			Title: "Update schedule", Parameters: map[string]any{"scheduleRef": schedule.Schedule.Ref, "name": "Renamed schedule"}})
	if err != nil || scheduleUpdate.Target.Ref != schedule.Schedule.Ref ||
		assistantString(scheduleUpdate.Before, "name") != schedule.Schedule.Name ||
		assistantString(scheduleUpdate.After, "name") != "Renamed schedule" {
		_ = scheduleTx.Rollback(ctx)
		t.Fatalf("hydrate exact schedule update: %#v %v", scheduleUpdate, err)
	}
	matching, err := repository.assistantScheduleUpdateSnapshotMatches(ctx, scheduleTx, ownerScope, project.Project.Ref, scheduleUpdate)
	if err != nil || !matching {
		_ = scheduleTx.Rollback(ctx)
		t.Fatalf("schedule update snapshot mismatch: matching=%v err=%v", matching, err)
	}
	if err := scheduleTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "context-run"}, Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Context metadata fixture."}})
	if err != nil || launched.Run == nil {
		t.Fatal(err)
	}
	cancelled, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "context-run-cancel", ExpectedVersion: &launched.Run.Version}, Payload: command.RunCommandInput{RunRef: launched.Run.Ref}})
	if err != nil || cancelled.Run == nil {
		t.Fatal(err)
	}
	for _, resource := range []struct {
		kind, ref, name string
		version         int64
	}{
		{"PROJECT", project.Project.Ref, project.Project.Name, project.Project.Version},
		{"AGENT", agent.Ref, agent.Name, agent.Version},
		{"WORKFLOW", workflow.Workflow.Ref, workflow.Workflow.Name, workflow.Workflow.Version},
		{"RUN", cancelled.Run.Ref, cancelled.Run.Title, cancelled.Run.Version},
		{"FILE", file.Ref, file.FileName, file.Version},
		{"ENVIRONMENT", configuration.Environment.Ref, configuration.Environment.Name, configuration.Environment.Version},
		{"INTEGRATION_CONNECTION", connection.Connection.Ref, connection.Connection.Name, connection.Connection.Version},
		{"SCHEDULE", schedule.Schedule.Ref, schedule.Schedule.Name, schedule.Schedule.Version},
	} {
		input := command.Command{Kind: command.CreateAssistantConversation, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "context-kind-" + resource.kind}, Payload: command.AssistantConversationInput{Context: entity.AssistantContextDescriptor{EntityKind: resource.kind, EntityRef: resource.ref, EntityName: "Forged", AllowedOperations: []string{"FORGED"}}}}
		var projection entity.AssistantContextDescriptor
		if resource.kind == "SCHEDULE" {
			input.Payload = command.AssistantConversationInput{ProjectRef: project.Project.Ref,
				Context: input.Payload.(command.AssistantConversationInput).Context}
			checkTx, beginErr := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if beginErr != nil {
				t.Fatal(beginErr)
			}
			checkErr := repository.authorizeCommand(ctx, checkTx, ownerScope, input)
			_ = checkTx.Rollback(ctx)
			if checkErr != nil {
				t.Fatalf("schedule conversation authorization: %v", checkErr)
			}
		}
		if resource.kind == "FILE" {
			checkTx, beginErr := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if beginErr != nil {
				t.Fatal(beginErr)
			}
			var checkErr error
			projection, checkErr = repository.resolveAssistantContext(ctx, checkTx, ownerScope,
				input.Payload.(command.AssistantConversationInput).Context, project.Project.Ref)
			_ = checkTx.Rollback(ctx)
			if checkErr != nil {
				t.Fatalf("direct file context: %v", checkErr)
			}
		} else {
			result, err := service.Execute(ctx, input)
			if err != nil || result.Conversation == nil {
				t.Fatalf("context %s: %v", resource.kind, err)
			}
			projection = result.Conversation.Context
		}
		if projection.EntityName != resource.name || projection.EntityVersion == nil || *projection.EntityVersion != resource.version || contains(projection.AllowedOperations, "FORGED") {
			t.Fatalf("context %s lost authoritative metadata", resource.kind)
		}
		if (resource.kind == "FILE" || resource.kind == "RUN") && len(projection.AllowedOperations) != 0 {
			t.Fatalf("context %s invented a mutating operation", resource.kind)
		}
		if resource.kind == "ENVIRONMENT" && (len(projection.AllowedOperations) != 1 ||
			!contains(projection.AllowedOperations, "PREPARE_RUNTIME_ENVIRONMENT_REVISION")) {
			t.Fatalf("environment context did not publish its exact revision capability: %#v", projection.AllowedOperations)
		}
		if resource.kind == "AGENT" && (!contains(projection.AllowedOperations, "UPDATE_AGENT") ||
			!contains(projection.AllowedOperations, "BIND_AGENT_RUNTIME_ENVIRONMENT")) {
			t.Fatal("agent context did not publish its exact update and binding capabilities")
		}
		if resource.kind == "WORKFLOW" && !contains(projection.AllowedOperations, "UPDATE_WORKFLOW") {
			t.Fatal("workflow context did not publish its exact update capability")
		}
		if resource.kind == "INTEGRATION_CONNECTION" && !contains(projection.AllowedOperations, "UPDATE_INTEGRATION_CONNECTION") {
			t.Fatal("connection context did not publish its exact update capability")
		}
		if resource.kind == "SCHEDULE" && !contains(projection.AllowedOperations, "UPDATE_SCHEDULE") {
			t.Fatal("schedule context did not publish its exact update capability")
		}
		if resource.kind == "PROJECT" && !contains(projection.AllowedOperations, "CREATE_RUNTIME_ENVIRONMENT_DRAFT") {
			t.Fatal("project context did not publish its environment draft capability")
		}
		if resource.kind == "PROJECT" && (!contains(projection.AllowedOperations, "CREATE_ROLE_IMAGE_RECIPE") ||
			!contains(projection.AllowedOperations, "UPDATE_ROLE_IMAGE_RECIPE")) {
			t.Fatal("project context did not publish its role image capability")
		}
		resolved, err := repository.ResolvePrincipal(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		current, err := repository.resolveScope(ctx, resolved)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		foreign := current
		foreign.organizationID = "90000000-0000-4000-8000-000000000098"
		_, readErr := repository.resolveAssistantContext(ctx, tx, foreign, projection, "")
		if !errors.Is(readErr, errs.ErrNotFound) {
			_ = tx.Rollback(ctx)
			t.Fatalf("foreign tenant context %s: %v", resource.kind, readErr)
		}
		if resource.kind != "ENVIRONMENT" && resource.kind != "INTEGRATION_CONNECTION" {
			foreign = current
			foreign.authorityProjectID = "90000000-0000-4000-8000-000000000097"
			_, readErr = repository.resolveAssistantContext(ctx, tx, foreign, projection, "")
			if !errors.Is(readErr, errs.ErrNotFound) {
				_ = tx.Rollback(ctx)
				t.Fatalf("foreign signed project context %s: %v", resource.kind, readErr)
			}
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
