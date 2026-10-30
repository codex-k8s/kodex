package platform

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRuntimeOverloadCompletionComponent persists the trusted controller's new
// capacity code through native owner commands and exact replay, including graph
// and durable event reads. Without a published fallback policy it creates no
// successor; the fixture never contacts a provider or inserts business rows.
// TestRuntimeOverloadCompletionComponent сохраняет новый capacity code доверенного
// controller через native owner commands и точный replay, включая graph и durable
// event. Без опубликованной fallback policy successor не появляется; оснастка не
// обращается к провайдеру и не вставляет готовые бизнесовые строки.
func TestRuntimeOverloadCompletionComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated overload owner fixture")
	}
	defer pool.Close()
	_, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	execute := func(kind command.Kind, actor value.Principal, key string, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: key}, Payload: payload})
		if err != nil {
			t.Fatalf("native overload command failed: %s outcome=%s", kind, completionOutcomeClass(err))
		}
		return result
	}
	project := execute(command.CreateProject, owner, "overload-project", command.ProjectInput{Name: "Overload persistence", Language: "ru"}).Project
	if project == nil {
		t.Fatal("overload project missing")
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "overload-worker", "Capacity worker")
	run := execute(command.LaunchRun, owner, "overload-launch", command.LaunchRunInput{ProjectRef: project.Ref, Task: "Prepare a short operator note", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
	if run == nil {
		t.Fatal("overload run missing")
	}
	claims := execute(command.ClaimExecution, worker, "overload-claim", command.LeaseInput{WorkloadInstance: "overload-fixture", Limit: 1}).RuntimeItems
	if len(claims) != 1 || stringMap(claims[0], "runRef") != run.Ref {
		t.Fatal("exact overload lease missing")
	}
	claim := claims[0]
	code, summary := runtimecontract.ProviderOverloadedFailureCode, "i18n:"+runtimecontract.ProviderOverloadedFailureCode
	payload := command.CompleteExecutionInput{LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), SafeErrorCode: code, ResultSummary: summary, Usage: turnUsageFixture()}
	completed := execute(command.CompleteExecution, worker, "overload-complete", payload)
	replayed := execute(command.CompleteExecution, worker, "overload-complete", payload)
	if completed.Run == nil || replayed.Run == nil || completed.Event == nil || replayed.Event == nil ||
		completed.Run.State != "FAILED" || completed.Run.SafeErrorCode != code || completed.Run.ResultSummary != summary ||
		replayed.Run.Version != completed.Run.Version || replayed.Event.Ref != completed.Event.Ref {
		t.Fatal("overload completion or exact replay lost owner outcome")
	}
	var nodeSummary, runSummary, storedCode string
	var events, receipts int64
	if err := pool.QueryRow(ctx, queryInitializationCompletionProof, stringMap(claim, "nodeRef"), "overload-complete").Scan(&nodeSummary, &runSummary, &storedCode, &events, &receipts); err != nil ||
		nodeSummary != summary || runSummary != summary || storedCode != code || events != 1 || receipts != 1 {
		t.Fatal("overload durable summary, event or receipt changed")
	}
	read, graph, err := service.GetRunGraph(ctx, owner, run.Ref)
	if err != nil || read.SafeErrorCode != code || read.ResultSummary != summary || read.State != "FAILED" {
		t.Fatal("protected owner graph lost overload")
	}
	found := false
	for _, node := range graph.Nodes {
		if node.Ref == stringMap(claim, "nodeRef") {
			found = node.State == "FAILED" && node.SafeErrorCode == code && node.ProgressSummary == summary
		}
	}
	if !found || len(execute(command.ClaimExecution, worker, "overload-no-successor", command.LeaseInput{WorkloadInstance: "overload-fixture", Limit: 1}).RuntimeItems) != 0 {
		t.Fatal("overload changed graph or silently authorized another execution")
	}
}
