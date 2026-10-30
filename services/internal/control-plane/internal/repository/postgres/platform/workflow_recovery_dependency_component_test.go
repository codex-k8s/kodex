package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// TestWorkflowRecoveredPredecessorUnblocksDependencyComponent reproduces a physical initialization
// failure, native owner-approved retry and downstream claim without changing terminal history.
// TestWorkflowRecoveredPredecessorUnblocksDependencyComponent воспроизводит физический отказ
// инициализации, штатный согласованный повтор и claim зависимого этапа без изменения истории.
func TestWorkflowRecoveredPredecessorUnblocksDependencyComponent(t *testing.T) {
	f := newWorkflowRecoveryGraphRegression(t, false, false, true)
	initial := f.claims("dependency-initial")
	if len(initial) != 1 || stringMap(initial[0], "agentRef") != f.agents["a"] {
		t.Fatal("dependent stage ran before its predecessor")
	}
	first := initial[0]
	f.execute(command.CompleteExecution, "dependency-initialization-failure", nil, command.CompleteExecutionInput{
		LeaseRef: stringMap(first, "leaseRef"), Fence: stringMap(first, "fence"), Generation: runtimeRevisionMapInt64(first, "generation"),
		SafeErrorCode: runtimecontract.InitializationFailureCode,
		ResultSummary: runtimecontract.InitializationFailureSummary("ARTIFACT_TRANSFER", "TRANSPORT_UNAVAILABLE"),
	})
	manager := f.claims("dependency-manager")
	if len(manager) != 1 || stringMap(manager[0], "agentRef") != f.managerRef {
		t.Fatal("runtime failure did not reach the exact exception manager")
	}
	f.complete("dependency-analysis", manager[0], "", false)
	gate := f.recoveryGate(f.read())
	if len(f.claims("dependency-before-approval")) != 0 {
		t.Fatal("an open recovery gate admitted the dependent stage")
	}
	f.execute(command.ResolveOwnerGate, "dependency-approve", &gate.Version,
		command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Повторить только отказавший этап."})
	retried := f.claims("dependency-retry")
	if len(retried) != 1 || stringMap(retried[0], "agentRef") != f.agents["a"] ||
		stringMap(retried[0], "runRef") == stringMap(first, "runRef") {
		t.Fatal("recovery did not create exactly one fresh predecessor attempt")
	}
	if len(f.claims("dependency-before-result")) != 0 {
		t.Fatal("an unaccepted retry admitted the dependent stage")
	}
	f.complete("dependency-retry-success", retried[0], "a.md", true)
	next := f.claims("dependency-after-result")
	if len(next) != 1 || stringMap(next[0], "agentRef") != f.agents["b"] {
		t.Fatal("accepted recovery left the dependent stage queued behind its failed logical anchor")
	}
	_, graph, err := f.service.GetRunGraph(f.ctx, f.owner, f.runRef)
	if err != nil {
		t.Fatal("read recovery graph", completionOutcomeClass(err))
	}
	states := map[string]string{}
	for _, node := range graph.Nodes {
		states[node.Ref] = node.State
	}
	if states[stringMap(first, "nodeRef")] != "FAILED" || states[stringMap(retried[0], "nodeRef")] != "SUCCEEDED" {
		t.Fatal("dependency admission rewrote physical execution history")
	}
	f.complete("dependency-final-success", next[0], "b.md", true)
	final := f.read()
	if final.state != "SUCCEEDED" || final.managers != 1 || final.leases != 0 || len(final.episodes) != 1 ||
		len(final.journals["a"]) != 2 || final.journals["a"][0].Accepted || !final.journals["a"][1].Accepted ||
		len(final.journals["b"]) != 1 || !final.journals["b"][0].Accepted || len(f.claims("dependency-terminal")) != 0 {
		t.Fatal("recovered dependency changed receipts or left the root unfinished")
	}
}
