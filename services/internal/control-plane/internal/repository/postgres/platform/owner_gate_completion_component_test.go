package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// assertOwnerGateApprovalConsequence checks the public owner read without deriving its answer
// from physical node counts. Exhausted recovery gates intentionally have no APPROVE decision.
// assertOwnerGateApprovalConsequence проверяет публичный owner read без вывода ответа
// из количества физических узлов. Исчерпанные recovery gates намеренно не предлагают APPROVE.
func assertOwnerGateApprovalConsequence(t *testing.T, gate entity.OwnerGate, terminal, external bool) {
	t.Helper()
	for _, consequence := range gate.DecisionConsequences {
		if consequence.Decision == "APPROVE" && (consequence.TerminalForRun != terminal || consequence.ExecutesExternalEffect != external) {
			t.Fatal("public APPROVE consequence disagrees with the exact owner lifecycle")
		}
	}
	for _, decision := range gate.AllowedDecisions {
		if decision != "APPROVE" {
			continue
		}
		for _, consequence := range gate.DecisionConsequences {
			if consequence.Decision == decision {
				return
			}
		}
		t.Fatal("public APPROVE consequence missing")
	}
}

// TestOwnerGateServerCompletionComponent compares public predictions with actual native decisions:
// a sibling result gate keeps the root nonterminal, and only the last accepted gate finishes it.
// Closed history is read again after completion without replacing its stored outcome prediction.
// TestOwnerGateServerCompletionComponent сравнивает публичный прогноз с native decisions:
// соседний result gate оставляет root активным, последний принятый gate завершает процесс.
// Закрытая история повторно читается после завершения без подмены сохранённого исхода.
func TestOwnerGateServerCompletionComponent(t *testing.T) {
	f := newWorkflowGateRegression(t, false, true)
	leases := f.claims("result-gates-initial")
	if len(leases) != 2 {
		t.Fatal("two published result stages were not admitted")
	}
	for index, lease := range leases {
		file := "a.md"
		if stringMap(lease, "agentRef") == f.agents["b"] {
			file = "b.md"
		}
		f.complete([]string{"result-gates-a", "result-gates-b"}[index], lease, file, true)
	}
	root, _, err := f.service.GetRunGraph(f.ctx, f.owner, f.runRef)
	if err != nil || len(root.GateRefs) != 2 || root.State != "WAITING_HUMAN" {
		t.Fatal("two accepted stages did not open their exact result gates")
	}
	read := func(ref string) entity.OwnerGate {
		t.Helper()
		gate, err := f.service.GetOwnerGate(f.ctx, f.owner, ref)
		if err != nil {
			t.Fatal("read exact native result gate")
		}
		return gate
	}
	first, second := read(root.GateRefs[0]), read(root.GateRefs[1])
	assertOwnerGateApprovalConsequence(t, first, false, false)
	assertOwnerGateApprovalConsequence(t, second, false, false)
	f.execute(command.ResolveOwnerGate, "result-gates-first", &first.Version, command.GateResolutionInput{GateRef: first.Ref, Decision: "APPROVE"})
	closed := read(first.Ref)
	assertOwnerGateApprovalConsequence(t, closed, false, false)
	second = read(second.Ref)
	assertOwnerGateApprovalConsequence(t, second, true, false)
	f.execute(command.ResolveOwnerGate, "result-gates-final", &second.Version, command.GateResolutionInput{GateRef: second.Ref, Decision: "APPROVE"})
	if f.read().state != "SUCCEEDED" {
		t.Fatal("last accepted result gate did not finish the native root")
	}
	again := read(first.Ref)
	assertOwnerGateApprovalConsequence(t, again, false, false)
	assertOwnerGateApprovalConsequence(t, read(second.Ref), true, false)
}
