package platform

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestWorkflowEvidenceCompletionBudgetComponent finishes twelve distinct native
// file revisions under the real five-second owner RPC budget. All rows and
// receipts come from owner commands; trusted gateway output is not live GitHub.
// It requires exactly one continuation and no second effect on callback replay.
// TestWorkflowEvidenceCompletionBudgetComponent завершает двенадцать различных
// native file revisions в настоящем пятисекундном бюджете owner RPC. Rows и
// receipts создаются owner commands; trusted gateway output не является живой
// GitHub-проверкой. Требуются единственное продолжение и отсутствие повторного
// эффекта при replay callback.
func TestWorkflowEvidenceCompletionBudgetComponent(t *testing.T) {
	for _, planner := range []string{"auto", "force_custom_plan", "force_generic_plan"} {
		t.Run(planner, func(t *testing.T) {
			fixture, lease := newWorkflowPDLCFileOwnerFixtureWithPlanner(t, planner)
			f := fixture.f
			head := strings.Repeat("a", 40)
			for index := 0; index < 12; index++ {
				operation := "github.repository.content.update"
				input := map[string]any{
					"path": "docs/requirements.md", "branch": "feature/journal",
					"message":        "Уточнить требования " + leftPad(index, 3),
					"content_base64": base64.StdEncoding.EncodeToString([]byte("Редакция требований " + leftPad(index, 3))),
					"sha":            strings.Repeat("b", 40),
				}
				if index == 0 {
					operation = "github.repository.content.create"
					delete(input, "sha")
				}
				invocation, err := fixture.resolve(lease, fixture.key("budget-file"), operation, input)
				if err != nil {
					t.Fatal("admit native file revision", completionOutcomeClass(err))
				}
				fixture.finishEffect(invocation, "github-commit:"+head, string(asJSON(map[string]any{"sha": head, "path": "docs/requirements.md"})))
			}
			payload := command.CompleteExecutionInput{
				LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"),
				Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true,
				Usage: turnUsageFixture(), ResultSummary: "Документ обновлён, передать подтверждённые результаты.",
			}
			input := command.Command{Kind: command.CompleteExecution, Principal: f.worker,
				Mutation: value.Mutation{IdempotencyKey: fixture.key("budget-complete")}, Payload: payload}
			ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
			started := time.Now()
			_, err := f.service.Execute(ctx, input)
			cancel()
			t.Logf("native completion budget: elapsed_ms=%d outcome=%s", time.Since(started).Milliseconds(), completionOutcomeClass(err))
			if err != nil {
				t.Fatal("native completion exceeded unchanged owner RPC budget", completionOutcomeClass(err))
			}
			var restored string
			if err := f.pool.QueryRow(f.ctx, queryWorkflowEvidencePlanRead).Scan(&restored); err != nil || restored != planner {
				t.Fatal("native completion leaked its evidence planning mode")
			}
			next := fixture.claimOutcome(lease, f.agents["author"])
			var facts struct {
				NativeOutcomes []json.RawMessage `json:"nativeOutcomes"`
			}
			task := stringMap(next, "task")
			if json.Unmarshal([]byte(task[strings.LastIndex(task, "\n")+1:]), &facts) != nil || len(facts.NativeOutcomes) != 12 {
				t.Fatal("bounded completion omitted native revisions")
			}
			_, before, err := f.service.GetRunGraph(f.ctx, f.owner, f.runRef)
			if err != nil {
				t.Fatal("read owner graph before completion replay")
			}
			if _, err := f.service.Execute(f.ctx, input); err != nil {
				t.Fatal("replay committed native completion", completionOutcomeClass(err))
			}
			_, after, err := f.service.GetRunGraph(f.ctx, f.owner, f.runRef)
			if err != nil || string(asJSON(before)) != string(asJSON(after)) {
				t.Fatal("completion replay changed continuation graph")
			}
			if work, err := f.service.ClaimIntegrationInvocations(f.ctx, fixture.gateway, "budget-no-repeat", 1); err != nil || len(work) != 0 {
				t.Fatal("completion replay queued another external action")
			}
		})
	}
}
