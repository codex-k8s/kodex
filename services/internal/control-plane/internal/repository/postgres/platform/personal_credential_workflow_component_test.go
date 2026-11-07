package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assertPersonalPublishedWorkflowPreflight publishes actual workflow versions before testing
// the normal LaunchRun command. Required personal capabilities yield the complete two-key
// refusal before session/run creation, while an unrelated coordinator grant does not become
// a step requirement. All observations belong to a disposable owner database; no model,
// provider API, delegation continuation or live authentication is exercised here.
// assertPersonalPublishedWorkflowPreflight публикует настоящие workflow versions и вызывает
// обычный LaunchRun. Обязательные personal capabilities возвращают полный отказ по двум
// ключам до создания session/run; посторонний coordinator grant не становится требованием
// шага. Проверка использует disposable owner DB без модели, provider API, continuation
// делегирования либо живой authentication.
func assertPersonalPublishedWorkflowPreflight(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, connectionRef, projectRef, readerRef string) {
	t.Helper()
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "personal-workflow-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("personal workflow fixture command %s: %v", kind, err)
		}
		return result
	}
	coordinator := createLifecycleAgent(t, ctx, service, owner, projectRef, "personal-workflow-coordinator", "Personal workflow coordinator")
	plain := createLifecycleAgent(t, ctx, service, owner, projectRef, "personal-workflow-plain", "Workflow reader without integrations")
	for index, ref := range []string{readerRef, plain.Ref} {
		current, err := service.GetAgent(ctx, owner, ref)
		if err != nil {
			t.Fatal(err)
		}
		execute(command.ChangeAgentCapability, []string{"reader-artifact", "plain-artifact"}[index], &current.Version,
			command.AgentBindingInput{AgentRef: ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true})
	}
	connection, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.ChangeIntegrationGrant, "coordinator-grant", &connection.Version,
		command.IntegrationGrantInput{ConnectionRef: connectionRef, AgentRef: coordinator.Ref,
			CapabilityKey: "gitlab.project.metadata.read", ApprovalPolicy: "NONE", Enabled: true})
	publish := func(key, agent string, required []string) *entity.Workflow {
		t.Helper()
		draft := entity.WorkflowVersion{ExecutionMode: "SERVER_DAG_V1", Name: "Personal workflow " + key,
			Purpose: "Qualify published personal launch requirements", CoordinatorAgentRef: coordinator.Ref,
			Concurrency: 1, TimeoutSeconds: 120, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{{
				Key: "read", Position: 1, Name: "Read", AgentRef: agent, Instructions: "Read synthetic metadata",
				ExpectedResult: "Synthetic read-only result", TimeoutSeconds: 60, RequiredCapabilityKeys: append([]string{runtimecontract.ArtifactCapability}, required...),
				ResultRequirements: []entity.WorkflowResultRequirement{{Key: "report", Kind: "ARTIFACT", FileName: "report.md", MediaType: "text/markdown"}}}}}
		workflow := execute(command.CreateWorkflow, key+"-create", nil, command.WorkflowInput{
			ProjectRef: projectRef, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &draft}).Workflow
		if workflow == nil {
			t.Fatal("missing created workflow")
		}
		workflow = execute(command.ValidateWorkflow, key+"-validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
		if workflow == nil {
			t.Fatal("missing validated workflow")
		}
		workflow = execute(command.PublishWorkflow, key+"-publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
		if workflow == nil {
			t.Fatal("missing published workflow")
		}
		return workflow
	}
	workflow := publish("required", readerRef, []string{"gitlab.project.metadata.read"})
	scheduleInput := command.ScheduleInput{ProjectRef: projectRef, Name: "Synthetic workflow schedule", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Preset: "CUSTOM", CronExpression: "0 * * * *", Timezone: "UTC", Input: map[string]any{}, AutomationText: "Read synthetic workflow metadata", PromptInputs: map[string]any{}, SessionPolicy: "NEW_EACH_RUN", NotificationPolicy: "CONTROL_CENTER_ONLY"}
	var schedulesBefore, schedulesAfter int
	if err := repository.pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.schedules").Scan(&schedulesBefore); err != nil {
		t.Fatal(err)
	}
	_, scheduleErr := service.Execute(ctx, command.Command{Kind: command.CreateSchedule, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "personal-workflow-schedule-refused"}, Payload: scheduleInput})
	var scheduleProblem *platformservice.IntegrationCredentialPreflightError
	if !errors.As(scheduleErr, &scheduleProblem) || len(scheduleProblem.Issues) != 2 {
		t.Fatal("published workflow schedule lost its complete personal refusal", scheduleErr)
	}
	for _, issue := range scheduleProblem.Issues {
		if issue.Problem != entity.IntegrationCredentialInitiatorMissing {
			t.Fatal("schedule author became a workflow initiator")
		}
	}
	if err := repository.pool.QueryRow(ctx, "SELECT count(*) FROM control_plane.schedules").Scan(&schedulesAfter); err != nil || schedulesBefore != schedulesAfter {
		t.Fatal("refused personal workflow persisted a schedule", err)
	}
	var sessionsBefore, runsBefore, sessionsAfter, runsAfter int
	if err := repository.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.sessions),(SELECT count(*) FROM control_plane.runs)").Scan(&sessionsBefore, &runsBefore); err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "personal-workflow-refused"}, Payload: command.LaunchRunInput{
			ProjectRef: projectRef, Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Synthetic personal workflow"}})
	var preflight *platformservice.IntegrationCredentialPreflightError
	if !errors.As(err, &preflight) || len(preflight.Issues) != 2 {
		t.Fatal("published workflow lost its complete personal preflight", err)
	}
	seen := map[string]bool{}
	for _, issue := range preflight.Issues {
		if issue.CapabilityKey != "gitlab.project.metadata.read" || seen[issue.ConnectionRef] {
			t.Fatal("unexpected or duplicate published personal requirement")
		}
		seen[issue.ConnectionRef] = true
	}
	if !seen[connectionRef] {
		t.Fatal("published workflow omitted its revoked credential")
	}
	if err := repository.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.sessions),(SELECT count(*) FROM control_plane.runs)").Scan(&sessionsAfter, &runsAfter); err != nil || sessionsBefore != sessionsAfter || runsBefore != runsAfter {
		t.Fatal("refused published workflow created execution state", err)
	}
	independent := publish("no-required-integration", plain.Ref, nil)
	scheduleInput.Target.Ref = independent.Ref
	schedule := execute(command.CreateSchedule, "independent-schedule", nil, scheduleInput).Schedule
	if schedule == nil {
		t.Fatal("coordinator-only personal grant blocked a shared-only workflow schedule")
	}
	execute(command.ArchiveSchedule, "independent-schedule-archive", &schedule.Version, command.ScheduleInput{Ref: schedule.Ref})
	run := execute(command.LaunchRun, "independent-launch", nil, command.LaunchRunInput{
		ProjectRef: projectRef, Target: entity.RunTarget{Type: "WORKFLOW", Ref: independent.Ref}, Task: "Synthetic workflow without an integration requirement"}).Run
	if run == nil {
		t.Fatal("coordinator-only personal grant blocked an independent workflow")
	}
	execute(command.CancelRun, "independent-cancel", &run.Version, command.RunCommandInput{RunRef: run.Ref})
}
