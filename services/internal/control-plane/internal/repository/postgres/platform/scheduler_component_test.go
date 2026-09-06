package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testAutomationScheduler(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	seedObservedCatalogFixture(t, ctx, repository)
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.schedules.create",
	}, "control-api-gateway")
	worker := func(operation string) value.Principal {
		return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
			ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
			CallerWorkload: "automation-scheduler", Operation: "platform.runtime.schedules." + operation,
		}, "automation-scheduler")
	}
	claimPrincipal, renewPrincipal, materializePrincipal, failPrincipal := worker("claim"), worker("renew"), worker("materialize"), worker("fail")
	execute := func(kind command.Kind, principal value.Principal, key string, payload any, version *int64) command.Result {
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: principal,
			Mutation: value.Mutation{IdempotencyKey: "scheduler-unit-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", command.ProjectInput{Name: "Scheduler unit", Purpose: "Synthetic schedule lifecycle", Language: "en"}, nil).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "scheduler-unit-agent", "Scheduler unit agent")
	newSchedule := func(key, policy string, target entity.RunTarget, input map[string]any, options ...func(*command.ScheduleInput)) *entity.Schedule {
		payload := command.ScheduleInput{
			ProjectRef: project.Ref, Name: key, Target: target, Preset: "CUSTOM", CronExpression: "0 * * * *", Timezone: "UTC",
			Input: input, AutomationText: "Prepare the immutable automation report.", PromptInputs: map[string]any{"format": "compact"},
			SessionPolicy: policy, NotificationPolicy: "CONTROL_CENTER_ONLY",
		}
		for _, option := range options {
			option(&payload)
		}
		return execute(command.CreateSchedule, owner, key, payload, nil).Schedule
	}
	makeDue := func(schedule *entity.Schedule, days int) {
		if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.schedules SET next_run_at = next_run_at - $2::integer * interval '1 day' WHERE ref = $1`, schedule.Ref, days); err != nil {
			t.Fatal(err)
		}
	}
	claim := func() map[string]any {
		claims, err := service.ClaimDueSchedules(ctx, claimPrincipal, "scheduler-unit", 1)
		if err != nil || len(claims) != 1 {
			t.Fatalf("claim: count=%d err=%v", len(claims), err)
		}
		return claims[0]
	}
	leaseInput := func(claim map[string]any) command.OccurrenceInput {
		return command.OccurrenceInput{OccurrenceRef: stringMap(claim, "occurrenceRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64)}
	}
	disable := func(schedule *entity.Schedule, key string) {
		current, err := service.GetSchedule(ctx, owner, schedule.Ref)
		if err != nil {
			t.Fatal(err)
		}
		execute(command.SetScheduleEnabled, owner, key, command.ScheduleInput{Ref: schedule.Ref, Enabled: false}, &current.Version)
	}
	schedule := newSchedule("continued", "CONTINUE_ONE", entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, map[string]any{})
	testScheduleMaterializedPreview(t, ctx, repository, service, owner, schedule, false)
	eventCount := func() int {
		var count int
		if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.outbox_events WHERE convert_from(payload,'UTF8')::jsonb->>'aggregateRef'=$1 AND convert_from(payload,'UTF8')::jsonb->>'eventName'='SCHEDULE_CHANGED'`, schedule.Ref).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	initialEvents := eventCount()
	makeDue(schedule, 7)
	var wait sync.WaitGroup
	var results [2][]map[string]any
	var failures [2]error
	for index := range results {
		wait.Go(func() {
			results[index], failures[index] = service.ClaimDueSchedules(ctx, claimPrincipal, fmt.Sprintf("replica-%d", index), 1)
		})
	}
	wait.Wait()
	if failures[0] != nil || failures[1] != nil || len(results[0])+len(results[1]) != 1 {
		t.Fatalf("race did not elect one winner: counts=%d,%d errors=%v", len(results[0]), len(results[1]), failures)
	}
	first := append(results[0], results[1]...)[0]
	if eventCount() != initialEvents+1 {
		t.Fatal("claim race did not emit exactly one atomic event")
	}
	if _, err := service.RenewScheduleOccurrence(ctx, owner, leaseInput(first)); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("browser renewed a scheduler lease: %v", err)
	}
	if _, err := service.RenewScheduleOccurrence(ctx, renewPrincipal, leaseInput(first)); err != nil {
		t.Fatal(err)
	}
	rotated := renewPrincipal
	rotated.CredentialRevision++
	if _, err := service.RenewScheduleOccurrence(ctx, rotated, leaseInput(first)); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("rotated credential renewed old lease: %v", err)
	}
	failedInput := leaseInput(first)
	failedInput.Retryable, failedInput.SafeErrorCode = true, "SCHEDULE_MATERIALIZATION_FAILED"
	failed := execute(command.FailScheduleOccurrence, failPrincipal, "fail-first", failedInput, nil)
	if stringMap(failed.Runtime, "state") != "RETRY_WAIT" {
		t.Fatal("retryable failure was not queued")
	}
	execute(command.FailScheduleOccurrence, failPrincipal, "fail-first", failedInput, nil)
	if eventCount() != initialEvents+2 {
		t.Fatal("failure replay duplicated event")
	}
	second := claim()
	if eventCount() != initialEvents+3 {
		t.Fatal("retry attempt has no atomic event")
	}
	if second["attempt"].(int32) != 2 || second["generation"].(int64) != 2 || stringMap(second, "occurrenceRef") != stringMap(first, "occurrenceRef") {
		t.Fatal("retry did not create a fresh attempt of the same occurrence")
	}
	if _, err := service.RenewScheduleOccurrence(ctx, renewPrincipal, leaseInput(first)); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("old lease renewed after retry: %v", err)
	}
	materialize := command.Command{Kind: command.MaterializeOccurrence, Principal: materializePrincipal,
		Mutation: value.Mutation{IdempotencyKey: "scheduler-unit-materialize"}, Payload: leaseInput(second)}
	var materialized [2]command.Result
	for index := range materialized {
		wait.Go(func() { materialized[index], failures[index] = service.Execute(ctx, materialize) })
	}
	wait.Wait()
	if failures[0] != nil || failures[1] != nil || materialized[0].Run == nil || materialized[1].Run == nil || materialized[0].Run.Ref != materialized[1].Run.Ref {
		t.Fatalf("materialize replay failed: %v", failures)
	}
	run := materialized[0].Run
	var count int
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.schedule_occurrence_attempts attempt JOIN control_plane.runs run ON run.id=attempt.run_id JOIN control_plane.session_turns turn ON turn.id=attempt.turn_id AND turn.session_id=attempt.session_id WHERE run.ref=$1 AND attempt.state='MATERIALIZED'`, run.Ref).Scan(&count); err != nil || count != 1 {
		t.Fatalf("exact attempt/run/turn binding: count=%d err=%v", count, err)
	}
	runtimeWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	runtime := execute(command.ClaimExecution, runtimeWorker, "runtime", command.LeaseInput{WorkloadInstance: "scheduler-runtime", Limit: 1}, nil)
	if len(runtime.RuntimeItems) != 1 || stringMap(runtime.RuntimeItems[0], "runRef") != run.Ref {
		t.Fatal("runtime claim did not resolve scheduled run")
	}
	snapshot := runtime.RuntimeItems[0]
	if !strings.Contains(stringMap(snapshot, "instructions"), schedule.AutomationText) || stringMap(snapshot, "promptMaterializationDigest") == "" || stringMap(snapshot, "runtimeRevisionRef") == "" {
		t.Fatal("automation did not pass through the shared prompt renderer")
	}
	input, ok := snapshot["input"].(map[string]any)
	if !ok {
		t.Fatal("runtime input is absent")
	}
	automation, ok := input["automation"].(map[string]any)
	if !ok || automation["scheduleRevisionDigest"] != schedule.CurrentRevision.Digest || automation["text"] != schedule.AutomationText || automation["occurrenceRef"] != stringMap(first, "occurrenceRef") {
		t.Fatal("runtime snapshot lost exact schedule provenance")
	}
	completeClaimedExecution(t, ctx, service, runtimeWorker, snapshot, "scheduler-runtime", false)
	testScheduleMaterializedPreview(t, ctx, repository, service, owner, schedule, true)
	var state string
	if err := repository.pool.QueryRow(ctx, `SELECT state FROM control_plane.schedule_occurrences WHERE ref=$1`, stringMap(first, "occurrenceRef")).Scan(&state); err != nil || state != "COMPLETED" {
		t.Fatalf("terminal graph: %s %v", state, err)
	}
	if _, err := service.RenewScheduleOccurrence(ctx, renewPrincipal, leaseInput(second)); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("terminal lease retained authority: %v", err)
	}
	makeDue(schedule, 6)
	continuedClaim := claim()
	continued := execute(command.MaterializeOccurrence, materializePrincipal, "continued-run", leaseInput(continuedClaim), nil).Run
	if continued.SessionRef != run.SessionRef {
		t.Fatal("CONTINUE_ONE created another session")
	}
	cancelled := execute(command.CancelRun, owner, "cancel-continued", command.RunCommandInput{RunRef: continued.Ref, Reason: "Synthetic cancellation"}, &continued.Version).Run
	retried := execute(command.RetryRun, owner, "retry-continued", command.RunCommandInput{RunRef: cancelled.Ref, Reason: "Synthetic retry"}, &cancelled.Version).Run
	retryRuntime := execute(command.ClaimExecution, runtimeWorker, "retry-runtime", command.LeaseInput{WorkloadInstance: "scheduler-runtime", Limit: 1}, nil)
	if len(retryRuntime.RuntimeItems) != 1 {
		t.Fatal("retry runtime is absent")
	}
	retrySnapshot := retryRuntime.RuntimeItems[0]
	retryInput, ok := retrySnapshot["input"].(map[string]any)
	if !ok {
		t.Fatal("retry input is absent")
	}
	retryAutomation, ok := retryInput["automation"].(map[string]any)
	if !ok || retryAutomation["occurrenceRef"] != stringMap(continuedClaim, "occurrenceRef") ||
		stringMap(retrySnapshot, "runRef") != retried.Ref || stringMap(retrySnapshot, "runtimeRevisionRef") == stringMap(snapshot, "runtimeRevisionRef") ||
		!strings.Contains(stringMap(retrySnapshot, "instructions"), schedule.AutomationText) {
		t.Fatal("retry lost immutable schedule provenance or reused runtime revision")
	}
	completeClaimedExecution(t, ctx, service, runtimeWorker, retrySnapshot, "scheduler-retry", false)
	disable(schedule, "disable-continued")

	expiring := newSchedule("expiring", "NEW_EACH_RUN", entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, map[string]any{})
	makeDue(expiring, 7)
	for attempt := int32(1); attempt <= 3; attempt++ {
		current := claim()
		if current["attempt"].(int32) != attempt {
			t.Fatal("expiry did not advance attempt")
		}
		if _, err := repository.pool.Exec(ctx, bootstrapComponentExpireScheduleClaimQuery, stringMap(current, "occurrenceRef")); err != nil {
			t.Fatal(err)
		}
		_, err := service.Execute(ctx, command.Command{Kind: command.MaterializeOccurrence, Principal: materializePrincipal,
			Mutation: value.Mutation{IdempotencyKey: fmt.Sprintf("scheduler-unit-late-%d", attempt)}, Payload: leaseInput(current)})
		if !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("late completion accepted: %v", err)
		}
	}
	if claims, err := service.ClaimDueSchedules(ctx, claimPrincipal, "expired", 1); err != nil || len(claims) != 0 {
		t.Fatalf("dead letter was reclaimed: %v", err)
	}
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.schedule_occurrences o JOIN control_plane.schedules s ON s.id=o.schedule_id WHERE s.ref=$1 AND o.state='DEAD_LETTER'`, expiring.Ref).Scan(&count); err != nil || count != 1 {
		t.Fatalf("dead letter readback: %d %v", count, err)
	}
	disable(expiring, "disable-expiring")
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.schedule_occurrence_attempts a JOIN control_plane.schedule_occurrences o ON o.id=a.occurrence_id JOIN control_plane.schedules s ON s.id=o.schedule_id WHERE s.ref=$1 AND a.state='CLAIMED'`, expiring.Ref).Scan(&count); err != nil || count != 0 {
		t.Fatal("cancel left a live attempt")
	}
	if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.schedule_occurrence_attempts SET input_digest=repeat('a',64) WHERE ref=(SELECT ref FROM control_plane.schedule_occurrence_attempts LIMIT 1)`); err == nil {
		t.Fatal("attempt history is mutable")
	}

	skipped := newSchedule("skipped", "NEW_EACH_RUN", entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, map[string]any{}, func(input *command.ScheduleInput) { input.MisfirePolicy = "SKIP" })
	makeDue(skipped, 7)
	if claims, err := service.ClaimDueSchedules(ctx, claimPrincipal, "skip", 1); err != nil || len(claims) != 0 {
		t.Fatalf("misfire SKIP launched work: %v", err)
	}
	readback, err := service.GetSchedule(ctx, owner, skipped.Ref)
	if err != nil || readback.LastOutcome != "SKIPPED" {
		t.Fatalf("skip outcome is not readable: %v", err)
	}
	disable(skipped, "disable-skipped")
	for _, overlap := range []string{"FORBID", "ALLOW"} {
		overlapping := newSchedule("overlap-"+overlap, "NEW_EACH_RUN", entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, map[string]any{}, func(input *command.ScheduleInput) { input.OverlapPolicy = overlap })
		makeDue(overlapping, 7)
		claim()
		makeDue(overlapping, 6)
		claims, err := service.ClaimDueSchedules(ctx, claimPrincipal, "overlap", 1)
		want := 0
		if overlap == "ALLOW" {
			want = 1
		}
		if err != nil || len(claims) != want {
			t.Fatalf("overlap %s: count=%d err=%v", overlap, len(claims), err)
		}
		disable(overlapping, "disable-overlap-"+overlap)
	}

	draft := entity.WorkflowVersion{Ref: "draft", Name: "Scheduled process", Purpose: "Coordinate an automation",
		CoordinatorAgentRef: agent.Ref, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600,
		CompletionCriteria: "A report is prepared", ResultSchema: map[string]any{},
		Inputs: []entity.WorkflowInputField{{Key: "record", Label: "Record", Type: "TEXT", Required: true}},
		Steps: []entity.WorkflowStep{{Key: "report", Position: 1, Name: "Report", AgentRef: agent.Ref,
			Instructions: "Prepare the report.", TimeoutSeconds: 900, ExpectedResult: "A bounded report"}},
	}
	workflow := execute(command.CreateWorkflow, owner, "workflow", command.WorkflowInput{ProjectRef: project.Ref,
		Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}, nil).Workflow
	workflow = execute(command.ValidateWorkflow, owner, "workflow-valid", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version).Workflow
	workflow = execute(command.PublishWorkflow, owner, "workflow-publish", command.WorkflowInput{Ref: workflow.Ref}, &workflow.Version).Workflow
	replayedWorkflow := execute(command.CreateWorkflow, owner, "workflow", command.WorkflowInput{ProjectRef: project.Ref,
		Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}, nil).Workflow
	if replayedWorkflow.Version != workflow.Version || replayedWorkflow.Published == nil || replayedWorkflow.LaunchReadiness == nil || replayedWorkflow.LaunchReadiness.WorkflowVersion != workflow.Version || replayedWorkflow.LaunchReadiness.RevisionRef != workflow.Published.Ref {
		t.Fatal("historical Workflow receipt mixed current readiness with stale body")
	}
	testWorkflowLaunchReadiness(t, ctx, repository, service, owner, workflow)
	processSchedule := newSchedule("process", "CONTINUE_ONE", entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, map[string]any{"record": "synthetic"})
	testScheduleMaterializedPreview(t, ctx, repository, service, owner, processSchedule, false)
	makeDue(processSchedule, 7)
	processClaim := claim()
	process := execute(command.MaterializeOccurrence, materializePrincipal, "process-run", leaseInput(processClaim), nil).Run
	if len(process.Input) != 1 || process.Input["record"] != "synthetic" {
		t.Fatal("scheduler changed the workflow input schema")
	}
	processRuntime := execute(command.ClaimExecution, runtimeWorker, "process-runtime", command.LeaseInput{WorkloadInstance: "scheduler-runtime", Limit: 1}, nil)
	if len(processRuntime.RuntimeItems) != 1 {
		t.Fatal("process coordinator was not claimable")
	}
	coordinator := processRuntime.RuntimeItems[0]
	if stringMap(coordinator, "runRef") != process.Ref || !strings.Contains(stringMap(coordinator, "instructions"), processSchedule.AutomationText) ||
		!strings.Contains(stringMap(coordinator, "task"), "Workflow coordination contract") {
		t.Fatal("automation task did not reach coordinator context")
	}
	completeClaimedExecution(t, ctx, service, runtimeWorker, coordinator, "scheduler-process", false)
	testScheduleMaterializedPreview(t, ctx, repository, service, owner, processSchedule, true)
	disable(processSchedule, "disable-process")
}
