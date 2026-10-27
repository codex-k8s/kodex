package platform

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// assertWorkflowRetryOwnerSnapshot reads the actual HTTP/full owner paths twice and requires exact
// retained proof without changing graph/version/event state or depending on an active model lease.
// assertWorkflowRetryOwnerSnapshot дважды читает настоящие HTTP/full owner paths и требует точный
// сохранённый proof без изменения graph/version/events и без зависимости от active model lease.
func assertWorkflowRetryOwnerSnapshot(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, rootRef string, expected int) entity.RunGraph {
	t.Helper()
	root, graph, err := service.GetRunGraphSnapshot(ctx, owner, rootRef)
	if err != nil {
		t.Fatal("owner full snapshot rejected native business retry", completionOutcomeClass(err))
	}
	count := 0
	for _, edge := range graph.Edges {
		if edge.ServerWorkflowRetry != nil {
			count++
		}
	}
	if count != expected {
		t.Fatal("owner snapshot lost exact native retry read proofs")
	}
	again, next, err := service.GetRunGraphSnapshot(ctx, owner, rootRef)
	if err != nil || root.Version != again.Version || graph.Sequence != next.Sequence || !reflect.DeepEqual(graph, next) {
		t.Fatal("repeated read changed native retry history or state")
	}
	_, httpGraph, err := service.GetRunGraph(ctx, owner, rootRef)
	if err != nil {
		t.Fatal("ordinary graph read rejected native retry", completionOutcomeClass(err))
	}
	if !reflect.DeepEqual(graph.Edges, httpGraph.Edges) {
		t.Fatal("ordinary/full graph disagree on retry read proof")
	}
	return graph
}

// TestServerWorkflowRetrySnapshotComponent proves predicate-accepted output can still have an
// owner-requested rework; the approved new child and its cancelled history remain readable.
// TestServerWorkflowRetrySnapshotComponent доказывает, что predicate-accepted результат допускает
// owner-requested rework; одобренный новый child и его cancelled history остаются читаемыми.
func TestServerWorkflowRetrySnapshotComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated workflow retry snapshot database")
	}
	defer pool.Close()
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	f := &workflowRecoveryRegression{t: t, ctx: ctx, pool: pool, service: service, owner: owner, worker: worker, repository: repository, agents: map[string]string{}}
	project := f.execute(command.CreateProject, "snapshot-project", nil, command.ProjectInput{Name: "Historical retry snapshot", Language: "ru"}).Project
	f.projectRef = project.Ref
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "snapshot-manager", "Менеджер разбора")
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "snapshot-worker", "Исполнитель")
	agent = *f.execute(command.ChangeAgentCapability, "snapshot-artifact", &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
	f.agents["a"] = agent.Ref
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "История owner rework", Purpose: "Прочитать принятый результат и отдельную owner business attempt.", CoordinatorAgentRef: manager.Ref, Concurrency: 1, TimeoutSeconds: 120, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{Key: "work", Position: 1, Name: "Работа", AgentRef: agent.Ref, Instructions: "Создать a.md и точную декларацию.", TimeoutSeconds: 60, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, HumanGateAfter: true, GateDecisions: []string{"APPROVE", "REQUEST_CHANGES", "REJECT", "CANCEL"}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "a.md", MediaType: "text/markdown"}}}}}
	workflow := f.execute(command.CreateWorkflow, "snapshot-create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = f.execute(command.ValidateWorkflow, "snapshot-validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = f.execute(command.PublishWorkflow, "snapshot-publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	root := f.execute(command.LaunchRun, "snapshot-launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Подготовить точный материал."}).Run
	f.runRef = root.Ref
	initial := f.claims("snapshot-initial")
	if len(initial) != 1 || stringMap(initial[0], "agentRef") != agent.Ref {
		t.Fatal("native snapshot workflow did not claim producer")
	}
	f.complete("snapshot-accepted", initial[0], "a.md", true)
	current, err := service.GetRun(ctx, owner, root.Ref)
	if err != nil || len(current.GateRefs) != 1 {
		t.Fatal("accepted result did not open exact result gate")
	}
	gate, err := service.GetOwnerGate(ctx, owner, current.GateRefs[0])
	if err != nil || gate.State != "OPEN" {
		t.Fatal("accepted result gate unreadable")
	}
	f.execute(command.ResolveOwnerGate, "snapshot-request-changes", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "REQUEST_CHANGES", Comment: "Уточнить материал; прежняя predicate acceptance сохраняется."})
	managerWork := f.claims("snapshot-manager")
	if len(managerWork) != 1 || stringMap(managerWork[0], "agentRef") != manager.Ref {
		t.Fatal("owner changes did not dispatch exact recovery manager")
	}
	f.complete("snapshot-analysis", managerWork[0], "", false)
	recovery := f.recoveryGate(f.read())
	f.execute(command.ResolveOwnerGate, "snapshot-recover", &recovery.Version, command.GateResolutionInput{GateRef: recovery.Ref, Decision: "APPROVE", Comment: "Создать свежую вторую попытку."})
	graph := assertWorkflowRetryOwnerSnapshot(t, ctx, service, owner, root.Ref, 1)
	accepted := false
	for _, node := range graph.Nodes {
		for _, receipt := range node.WorkflowResults {
			accepted = accepted || receipt.Attempt == 1 && receipt.Accepted
		}
	}
	if !accepted {
		t.Fatal("retry read proof erased prior predicate acceptance")
	}
	current, err = service.GetRun(ctx, owner, root.Ref)
	if err != nil {
		t.Fatal("read native root before cancellation")
	}
	f.execute(command.CancelRun, "snapshot-cancel", &current.Version, command.RunCommandInput{RunRef: root.Ref})
	assertWorkflowRetryOwnerSnapshot(t, ctx, service, owner, root.Ref, 1)
	if len(f.claims("snapshot-no-resurrection")) != 0 {
		t.Fatal("historical retry proof re-admitted cancelled work")
	}
}
