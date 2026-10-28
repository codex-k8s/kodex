package platform

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// assertWorkflowRecoveryFeedbackTask reads the actual claimed task, preserving exact stored owner
// comments and completed manager data without treating these explanations as execution authority.
// assertWorkflowRecoveryFeedbackTask читает настоящую claimed task, сохраняя точные комментарии
// владельца и completed manager data; пояснения не считаются execution authority.
func assertWorkflowRecoveryFeedbackTask(t *testing.T, task string, expected workflowRecoveryFeedback) {
	t.Helper()
	parts := strings.SplitN(task, "Owner-approved recovery feedback (data):\n", 2)
	var actual workflowRecoveryFeedback
	if len(parts) != 2 || json.NewDecoder(strings.NewReader(parts[1])).Decode(&actual) != nil || !reflect.DeepEqual(expected, actual) {
		t.Fatal("claimed recovery task lost exact stored owner or manager feedback")
	}
}

// TestWorkflowRecoveryFeedbackComponent creates a real attempt-zero dispatch exception by disabling
// only a dependent worker after launch. Recovery uses the restored worker, stored Gate approval and
// completed manager analysis; all business writes and replay follow the existing service APIs.
// TestWorkflowRecoveryFeedbackComponent создаёт настоящий attempt-zero dispatch exception отключением
// зависимого сотрудника после запуска. Восстановление использует включённого сотрудника, сохранённое
// одобрение Gate и completed manager analysis; записи и replay идут через существующие service API.
func TestWorkflowRecoveryFeedbackComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated recovery feedback database")
	}
	defer pool.Close()
	repository, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	configureComponentArtifactScanner(t, repository)
	f := &workflowRecoveryRegression{t: t, ctx: ctx, pool: pool, repository: repository, service: service, owner: owner, worker: worker, agents: map[string]string{}}
	project := f.execute(command.CreateProject, "feedback-project", nil, command.ProjectInput{Name: "Attempt zero feedback", Language: "ru"}).Project
	f.projectRef = project.Ref
	manager := createLifecycleAgent(t, ctx, service, owner, project.Ref, "feedback-manager", "Менеджер разбора")
	f.managerRef = manager.Ref
	agents := map[string]entity.Agent{}
	draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Обратная связь первой материализации", Purpose: "Передать сохранённое решение владельца.", CoordinatorAgentRef: manager.Ref, Concurrency: 2, TimeoutSeconds: 240, ResultSchema: map[string]any{}}
	for index, key := range []string{"a", "b"} {
		agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "feedback-"+key, key)
		agent = *f.execute(command.ChangeAgentCapability, "feedback-cap-"+key, &agent.Version, command.AgentBindingInput{AgentRef: agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true}).Agent
		agents[key] = agent
		f.agents[key] = agent.Ref
		step := entity.WorkflowStep{Key: key, Position: int32(index + 1), Name: key, AgentRef: agent.Ref, Instructions: "Создать назначенный материал и точную декларацию.", TimeoutSeconds: 120, RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability}, ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: key + ".md", MediaType: "text/markdown"}}}
		if key == "b" {
			step.DependsOn = []string{"a"}
		}
		draft.Steps = append(draft.Steps, step)
	}
	workflow := f.execute(command.CreateWorkflow, "feedback-create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: manager.Ref, Draft: &draft}).Workflow
	workflow = f.execute(command.ValidateWorkflow, "feedback-validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	workflow = f.execute(command.PublishWorkflow, "feedback-publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
	f.runRef = f.execute(command.LaunchRun, "feedback-launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Создать два последовательных материала.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run.Ref
	first := f.claims("feedback-first")
	if len(first) != 1 || stringMap(first[0], "agentRef") != agents["a"].Ref {
		t.Fatal("feedback producer claim unavailable")
	}
	dependent := agents["b"]
	dependent = *f.execute(command.SetAgentEnabled, "feedback-disable", &dependent.Version, command.AgentInput{Ref: dependent.Ref, Enabled: false}).Agent
	f.complete("feedback-producer-complete", first[0], "a.md", true)
	snapshot := f.read()
	if len(snapshot.episodes) != 1 || snapshot.episodes[0].Attempt != 0 || snapshot.episodes[0].Reason != "DISPATCH_CONFIGURATION_UNAVAILABLE" || snapshot.episodes[0].State != "MANAGER_QUEUED" || len(snapshot.journals["b"]) != 0 {
		t.Fatal("native dispatch did not preserve an attempt-zero configuration episode")
	}
	analysis := f.claims("feedback-manager")
	if len(analysis) != 1 || stringMap(analysis[0], "agentRef") != manager.Ref {
		t.Fatal("attempt-zero manager claim unavailable")
	}
	managerSummary := "The dependent worker was disabled. Restore its eligibility before a fresh first materialization."
	completion := command.CompleteExecutionInput{LeaseRef: stringMap(analysis[0], "leaseRef"), Fence: stringMap(analysis[0], "fence"), Generation: runtimeRevisionMapInt64(analysis[0], "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: managerSummary}
	f.execute(command.CompleteExecution, "feedback-analysis", nil, completion)
	snapshot = f.read()
	gate := f.recoveryGate(snapshot)
	episode := snapshot.episodes[0]
	dependent = *f.execute(command.SetAgentEnabled, "feedback-enable", &dependent.Version, command.AgentInput{Ref: dependent.Ref, Enabled: true}).Agent
	approval := command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Сотрудник включён.\nСоздать именно обязательный b.md."}
	f.execute(command.ResolveOwnerGate, "feedback-approve", &gate.Version, approval)
	stored, err := service.GetOwnerGate(ctx, owner, gate.Ref)
	if err != nil || stored.State != "APPROVED" || stored.Version != gate.Version+1 {
		t.Fatal("attempt-zero stored approval unavailable")
	}
	recovered := f.read()
	f.execute(command.ResolveOwnerGate, "feedback-approve", &gate.Version, approval)
	replay := f.read()
	if !reflect.DeepEqual(recovered, replay) {
		t.Fatal("attempt-zero approval replay changed native graph")
	}
	next := f.claims("feedback-first-materialization")
	if len(next) != 1 || stringMap(next[0], "agentRef") != dependent.Ref {
		t.Fatal("owner-approved attempt-zero materialization unavailable")
	}
	expected := workflowRecoveryFeedback{Schema: "kodex.workflow-recovery-feedback.v1", Episode: episode.Episode, EpisodeVersion: episode.Version, StepKey: episode.StepKey, PreviousAttempt: 0, NextAttempt: 1, Reason: episode.Reason, OwnerDecision: integrationContinuationDecision{GateRef: stored.Ref, GateVersion: stored.Version, Decision: stored.Decision, Comment: stored.DecisionComment}, ManagerAnalysis: managerSummary}
	assertWorkflowRecoveryFeedbackTask(t, stringMap(next[0], "task"), expected)
	f.complete("feedback-dependent-complete", next[0], "b.md", true)
	terminal := f.read()
	if terminal.state != "SUCCEEDED" || len(terminal.journals["b"]) != 1 || !terminal.journals["b"][0].Accepted || terminal.journals["b"][0].Attempt != 1 || terminal.leases != 0 {
		t.Fatal("attempt-zero recovery bypassed or lost business acceptance")
	}
	f.complete("feedback-dependent-complete", next[0], "b.md", true)
	if !reflect.DeepEqual(terminal, f.read()) {
		t.Fatal("completion replay changed recovered task history")
	}
}
