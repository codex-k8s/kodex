package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestUsageCompletenessComponent drives native published self-step delegation, three actual leased
// turns and completion receipts; no business SQL writes, provider execution or forged runtime pins
// arrange aggregation. Legacy positive values retain UNKNOWN and replay never charges twice.
// TestUsageCompletenessComponent использует штатные published self-step delegation, три настоящих
// leased хода и completion receipts; агрегация не подготавливается business SQL, провайдером либо
// поддельными runtime pins. Положительный legacy UNKNOWN сохраняется, replay не удваивает расход.
func TestUsageCompletenessComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated usage fixture")
	}
	defer pool.Close()
	_, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, failure := service.Execute(ctx, command.Command{Kind: kind, Principal: actor,
			Mutation: value.Mutation{IdempotencyKey: "usage-quality-" + key, ExpectedVersion: version}, Payload: payload})
		if failure != nil {
			t.Fatalf("native usage fixture command %s %s: %s", kind, key, completionOutcomeClass(failure))
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Usage quality", Language: "ru"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "usage-quality-agent", "Исполнитель расхода")
	draft := entity.WorkflowVersion{Ref: "draft", Name: "Usage quality workflow", Purpose: "Measure a bounded stage",
		CoordinatorAgentRef: agent.Ref, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 1800,
		Instructions: "Execute the assigned self stage, then summarize its callback.", CompletionCriteria: "The stage and callback completed.", ResultSchema: map[string]any{},
		Steps: []entity.WorkflowStep{{Key: "self", Position: 1, Name: "Self stage", AgentRef: agent.Ref,
			Instructions: "Produce a bounded synthetic result.", TimeoutSeconds: 600, ExpectedResult: "Synthetic summary"}}}
	wf := execute(command.CreateWorkflow, owner, "workflow", nil, command.WorkflowInput{ProjectRef: project.Ref,
		Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: agent.Ref, Draft: &draft}).Workflow
	wf = execute(command.ValidateWorkflow, owner, "validate", &wf.Version, command.WorkflowInput{Ref: wf.Ref}).Workflow
	wf = execute(command.PublishWorkflow, owner, "publish", &wf.Version, command.WorkflowInput{Ref: wf.Ref}).Workflow
	for _, fixture := range []struct {
		name       string
		qualities  [3]runtimecontract.UsageCompleteness
		tokens     [3]int64
		want       runtimecontract.UsageCompleteness
		childFails bool
	}{
		{"legacy-positive", [3]runtimecontract.UsageCompleteness{}, [3]int64{100, 100, 100}, runtimecontract.UsageUnknown, false},
		{"complete-zero", [3]runtimecontract.UsageCompleteness{runtimecontract.UsageComplete, runtimecontract.UsageComplete, runtimecontract.UsageComplete}, [3]int64{}, runtimecontract.UsageComplete, false},
		{"complete-positive", [3]runtimecontract.UsageCompleteness{runtimecontract.UsageComplete, runtimecontract.UsageComplete, runtimecontract.UsageComplete}, [3]int64{100, 100, 100}, runtimecontract.UsageComplete, false},
		{"partial-missing", [3]runtimecontract.UsageCompleteness{runtimecontract.UsageComplete, runtimecontract.UsageUnknown, runtimecontract.UsageComplete}, [3]int64{100, 0, 0}, runtimecontract.UsagePartial, false},
		{"failed-partial", [3]runtimecontract.UsageCompleteness{runtimecontract.UsageComplete, runtimecontract.UsagePartial}, [3]int64{100, 100, 0}, runtimecontract.UsagePartial, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			prefix := fixture.name + "-"
			root := execute(command.LaunchRun, owner, prefix+"launch", nil, command.LaunchRunInput{ProjectRef: project.Ref,
				Task: "Collect synthetic measured usage.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: wf.Ref}}).Run
			if root.Usage.Completeness != runtimecontract.UsageUnknown {
				t.Fatal("unexecuted run fabricated complete usage")
			}
			claim := func(key, expectedRun string) map[string]any {
				t.Helper()
				items := execute(command.ClaimExecution, worker, prefix+key, nil, command.LeaseInput{WorkloadInstance: "usage-quality-fixture", Limit: 1}).RuntimeItems
				if len(items) != 1 || stringMap(items[0], "runRef") != expectedRun {
					t.Fatal("exact native usage lease missing")
				}
				return items[0]
			}
			parentLease := claim("parent", root.Ref)
			child := execute(command.DelegateExecution, worker, prefix+"delegate", nil, command.DelegateInput{
				LeaseRef: stringMap(parentLease, "leaseRef"), Fence: stringMap(parentLease, "fence"),
				Generation: runtimeRevisionMapInt64(parentLease, "generation"), TargetAgentRef: agent.Ref, WorkflowStepKey: "self",
				Task: "Produce the assigned synthetic result."}).Run
			childLease := claim("child", child.Ref)
			complete := func(lease map[string]any, index int, failed bool) {
				t.Helper()
				usage := entity.TokenUsage{Completeness: fixture.qualities[index], TotalTokens: fixture.tokens[index],
					InputTokens: fixture.tokens[index], ModelContextWindow: 475000}
				payload := command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"),
					Generation: runtimeRevisionMapInt64(lease, "generation"), Success: !failed, ResultSummary: "Synthetic measured result.", Usage: usage}
				if failed {
					payload.SafeErrorCode = "RUNTIME_UNAVAILABLE"
				}
				key := fmt.Sprintf("%scomplete-%d", prefix, index)
				if index == 0 {
					before, failure := service.GetRun(ctx, owner, root.Ref)
					if failure != nil {
						t.Fatal("read pre-denial root")
					}
					stale := payload
					stale.Generation++
					if _, failure := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker,
						Mutation: value.Mutation{IdempotencyKey: "usage-quality-" + key + "-stale"}, Payload: stale}); failure == nil {
						t.Fatal("stale generation changed usage")
					}
					after, failure := service.GetRun(ctx, owner, root.Ref)
					if failure != nil || after.Version != before.Version || after.EventSequence != before.EventSequence || after.Usage != before.Usage {
						t.Fatal("denied completion changed authoritative root")
					}
				}
				completed := execute(command.CompleteExecution, worker, key, nil, payload)
				beforeReplay, failure := service.GetRun(ctx, owner, root.Ref)
				if failure != nil {
					t.Fatal("read root before completion replay")
				}
				replayed := execute(command.CompleteExecution, worker, key, nil, payload)
				if completed.Run.Usage != replayed.Run.Usage {
					t.Fatal("completion replay charged usage twice")
				}
				afterReplay, failure := service.GetRun(ctx, owner, root.Ref)
				if failure != nil || afterReplay.Usage != beforeReplay.Usage || afterReplay.Version != beforeReplay.Version || afterReplay.EventSequence != beforeReplay.EventSequence {
					t.Fatal("completion replay emitted another root transition")
				}
				changed := payload
				changed.Usage.Completeness = (changed.Usage.Completeness + 1) % 3
				if _, failure := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker,
					Mutation: value.Mutation{IdempotencyKey: "usage-quality-" + key}, Payload: changed}); !errors.Is(failure, errs.ErrIdempotencyReuse) {
					t.Fatal("same completion key accepted changed measurement quality")
				}
			}
			complete(parentLease, 0, false)
			complete(childLease, 1, fixture.childFails)
			turns, state := int64(2), "FAILED"
			if !fixture.childFails {
				complete(claim("continuation", root.Ref), 2, false)
				turns, state = 3, "SUCCEEDED"
			}
			var total int64
			for index := int64(0); index < turns; index++ {
				total += fixture.tokens[index]
			}
			actual, failure := service.GetRun(ctx, owner, root.Ref)
			if failure != nil || actual.State != state || actual.Usage.Completeness != fixture.want ||
				actual.Usage.TotalTokens != total || actual.Usage.InputTokens != total || actual.Usage.ModelContextWindow != 475000 {
				t.Fatal("authoritative root lost exact numeric subtotal or aggregate completeness")
			}
		})
	}
}
