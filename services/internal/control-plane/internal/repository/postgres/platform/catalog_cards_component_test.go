package platform

import (
	"context"
	_ "embed"
	"reflect"
	"testing"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

//go:embed testdata/sql/catalog_card_connection.sql
var queryCardConnection string

//go:embed testdata/sql/catalog_card_connection_state.sql
var queryCardConnectionState string

func testCatalogCardProjections(t *testing.T, ctx context.Context, repository *Repository) {
	seedObservedCatalogFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, key string, payload any, version *int64) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "cards-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("card %s: %v", key, err)
		}
		return result
	}
	created := execute(command.CreateProject, "project", command.ProjectInput{Name: "Authoritative card projections", Language: "en"}, nil)
	project := *created.Project
	if project.LastActivityAt != nil || project.IntegrationState != "NONE" || project.AgentCount != 0 {
		t.Fatalf("empty project has invented activity: %#v", project)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "cards-agent-one", "Card agent one")
	second := createLifecycleAgent(t, ctx, service, owner, project.Ref, "cards-agent-two", "Card agent two")
	execute(command.ChangeAgentCapability, "delegate", command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: "platform.run.delegate", Enabled: true}, &agent.Version)
	draft := entity.WorkflowVersion{Name: "Card workflow", Purpose: "Card projection", CoordinatorAgentRef: agent.Ref, Concurrency: 2, TimeoutSeconds: 3600, CompletionCriteria: "Report completed", ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{
		{Key: "first", Position: 1, Name: "First", AgentRef: agent.Ref, Instructions: "Produce first result", TimeoutSeconds: 900, Parallel: true, ParallelGroup: 1},
		{Key: "second", Position: 2, Name: "Second", AgentRef: second.Ref, Instructions: "Produce second result", TimeoutSeconds: 900, Parallel: true, ParallelGroup: 1, HumanGateAfter: true, GateDecisions: []string{"APPROVE", "REJECT"}},
	}}
	createdWorkflow := execute(command.CreateWorkflow, "workflow", command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}, nil)
	workflow := *createdWorkflow.Workflow
	if workflow.CardSummary == nil || workflow.CardSummary.StageCount != 2 || workflow.CardSummary.UniqueAgentCount != 2 || workflow.CardSummary.ParallelGroupCount != 1 || !workflow.CardSummary.HasHumanGate || workflow.CardSummary.LastActivityAt != nil {
		t.Fatalf("draft summary: %#v", workflow.CardSummary)
	}
	validated := execute(command.ValidateWorkflow, "validate", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version)
	published := execute(command.PublishWorkflow, "publish", command.WorkflowInput{Ref: workflow.Ref}, &validated.Workflow.Version)
	draft.Steps = draft.Steps[:1]
	updated := execute(command.UpdateWorkflow, "new-draft", command.WorkflowInput{Ref: workflow.Ref, ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}, &published.Workflow.Version)
	if updated.Workflow.CardSummary.StageCount != 2 {
		t.Fatal("new draft replaced published card summary")
	}
	launched := execute(command.LaunchRun, "run", command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Verify card current work"}, nil)
	readAgent, err := service.GetAgent(ctx, owner, agent.Ref)
	if err != nil || readAgent.CurrentRunRef != launched.Run.Ref {
		t.Fatalf("active agent card: %q %v", readAgent.CurrentRunRef, err)
	}
	for _, projectFilter := range []string{"", project.Ref} {
		agents, _, err := service.ListAgents(ctx, owner, query.Filter{ProjectRef: projectFilter, Query: agent.Name, Page: query.Page{Size: 100}})
		if err != nil || len(agents) != 1 || agents[0].CurrentRunRef != readAgent.CurrentRunRef {
			t.Fatalf("agent catalog parity: %v", err)
		}
		workflows, _, err := service.ListWorkflows(ctx, owner, query.Filter{ProjectRef: projectFilter, Query: workflow.Name, Page: query.Page{Size: 100}})
		if err != nil || len(workflows) != 1 || !reflect.DeepEqual(workflows[0].CardSummary, updated.Workflow.CardSummary) {
			t.Fatalf("workflow catalog parity: %v", err)
		}
	}
	current, err := service.GetProject(ctx, owner, project.Ref)
	if err != nil || current.AgentCount != 2 || current.WorkflowCount != 1 || current.ActiveRunCount != 1 || current.LastActivityAt == nil {
		t.Fatalf("project visible counts: %#v %v", current, err)
	}
	replayed := execute(command.CreateProject, "project", command.ProjectInput{Name: project.Name, Language: "en"}, nil)
	if replayed.Project.AgentCount != 2 || replayed.Project.ActiveRunCount != 1 || replayed.Project.LastActivityAt == nil {
		t.Fatal("project receipt returned stale card")
	}
	projects, _, _, err := service.ListProjects(ctx, owner, query.Filter{Query: project.Name, Page: query.Page{Size: 100}})
	if err != nil || len(projects) != 1 || !reflect.DeepEqual(projects[0], current) {
		t.Fatalf("project list/get parity: %v", err)
	}
	if _, err := repository.pool.Exec(ctx, queryCardConnection, second.Ref); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ state, want string }{{"TESTING", "UNKNOWN"}, {"CONNECTED", "READY"}, {"DEGRADED", "DEGRADED"}, {"DISABLED", "DEGRADED"}} {
		if _, err := repository.pool.Exec(ctx, queryCardConnectionState, test.state); err != nil {
			t.Fatal(err)
		}
		card, err := service.GetProject(ctx, owner, project.Ref)
		if err != nil || card.IntegrationState != test.want {
			t.Fatalf("integration card %s: %q %v", test.state, card.IntegrationState, err)
		}
	}
	reader := contextProjectReader(t, ctx, repository, service, owner, project.Ref, "CARD_PROJECTIONS")
	hidden, err := service.GetProject(ctx, reader, project.Ref)
	if err != nil || hidden.AgentCount != 0 || hidden.WorkflowCount != 0 || hidden.ActiveRunCount != 0 || hidden.LastActivityAt != nil || hidden.IntegrationState != "NONE" {
		t.Fatalf("hidden children leaked through project card: %#v %v", hidden, err)
	}
	hiddenList, _, _, err := service.ListProjects(ctx, reader, query.Filter{Query: project.Name, Page: query.Page{Size: 100}})
	if err != nil || len(hiddenList) != 1 || !reflect.DeepEqual(hiddenList[0], hidden) {
		t.Fatalf("hidden project catalog parity: %v", err)
	}
	actor, err := repository.ResolvePrincipal(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	receiptAccessBinding(t, ctx, service, owner, actor.ActorID, "cards-reader-agent", []string{"agent.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: agent.Ref, ProjectRef: project.Ref})
	receiptAccessBinding(t, ctx, service, owner, actor.ActorID, "cards-reader-workflow", []string{"workflow.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "WORKFLOW", ResourceRef: workflow.Ref, ProjectRef: project.Ref})
	reader = receiptFreshPrincipal(t, ctx, repository, project.Ref)
	hiddenRun, err := service.GetAgent(ctx, reader, agent.Ref)
	if err != nil || hiddenRun.CurrentRunRef != "" {
		t.Fatalf("hidden run ref leaked: %q %v", hiddenRun.CurrentRunRef, err)
	}
	partialWorkflow, err := service.GetWorkflow(ctx, reader, workflow.Ref)
	if err != nil || partialWorkflow.CardSummary.UniqueAgentCount != 1 {
		t.Fatalf("hidden agent count leaked: %#v %v", partialWorkflow.CardSummary, err)
	}
	execute(command.CancelRun, "cancel", command.RunCommandInput{RunRef: launched.Run.Ref}, &launched.Run.Version)
	after, err := service.GetAgent(ctx, owner, agent.Ref)
	if err != nil || after.CurrentRunRef != "" {
		t.Fatalf("terminal work link retained: %q %v", after.CurrentRunRef, err)
	}
}
