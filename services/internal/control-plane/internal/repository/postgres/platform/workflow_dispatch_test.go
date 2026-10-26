package platform

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestServerWorkflowAcceptanceRequiresLatestEvidence prevents technical success or an older accepted
// attempt from dispatching dependents, and requires approval of the exact published result gate.
// TestServerWorkflowAcceptanceRequiresLatestEvidence запрещает dispatch по техническому успеху либо
// старой принятой попытке и требует одобрения точного опубликованного result gate.
func TestServerWorkflowAcceptanceRequiresLatestEvidence(t *testing.T) {
	step := entity.WorkflowStep{Key: "developer", HumanGateAfter: true}
	base := serverWorkflowAnchor{ID: "origin", Key: step.Key, State: "SUCCEEDED", AssignedAttempt: 1, LatestAttempt: 1, GateApproved: true, Receipts: []entity.WorkflowAttemptResult{{Attempt: 1, Accepted: true, Provenance: &entity.WorkflowCompletionProvenance{OriginNodeID: "origin"}}}}
	if _, ok := acceptedServerWorkflowAttempt(base, step); !ok {
		t.Fatal("exact accepted gated attempt rejected")
	}
	cases := map[string]func(*serverWorkflowAnchor){
		"technical success only": func(n *serverWorkflowAnchor) { n.Receipts = nil },
		"retry not accepted":     func(n *serverWorkflowAnchor) { n.LatestAttempt = 2 },
		"business rejected":      func(n *serverWorkflowAnchor) { n.Receipts[0].Accepted = false },
		"wrong receipt attempt":  func(n *serverWorkflowAnchor) { n.Receipts[0].Attempt = 2 },
		"missing provenance":     func(n *serverWorkflowAnchor) { n.Receipts[0].Provenance = nil },
		"foreign origin": func(n *serverWorkflowAnchor) {
			n.Receipts[0].Provenance = &entity.WorkflowCompletionProvenance{OriginNodeID: "other"}
		},
		"missing gate approval": func(n *serverWorkflowAnchor) { n.GateApproved = false },
		"changes requested":     func(n *serverWorkflowAnchor) { n.GateBlocked = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			candidate := base
			candidate.Receipts = append([]entity.WorkflowAttemptResult(nil), base.Receipts...)
			mutate(&candidate)
			if _, ok := acceptedServerWorkflowAttempt(candidate, step); ok {
				t.Fatal("unaccepted business result enabled a dependent")
			}
		})
	}
	step.HumanGateAfter = false
	base.GateApproved = false
	if _, ok := acceptedServerWorkflowAttempt(base, step); !ok {
		t.Fatal("ungated accepted attempt unexpectedly required a gate")
	}
}

// TestWorkflowPromptInputsExcludePrivateExecutionPins keeps transferable file evidence while
// preventing internal row, lease and revision identities from becoming model-visible authority.
// TestWorkflowPromptInputsExcludePrivateExecutionPins сохраняет передаваемые доказательства файлов,
// исключая внутренние row/lease/revision identity из доступного модели prompt.
func TestWorkflowPromptInputsExcludePrivateExecutionPins(t *testing.T) {
	inputs := []workflowInputReceipt{{StepKey: "developer", SourceNodeID: "private-anchor-canary", Receipt: entity.WorkflowAttemptResult{Attempt: 1, RunRef: "run_public", RuntimeRevisionID: "private-revision-canary", Accepted: true, Provenance: &entity.WorkflowCompletionProvenance{LeaseID: "private-lease-canary", NodeID: "private-node-canary"}, Results: []entity.WorkflowAcceptedResult{{Key: "design", Kind: "ARTIFACT", ArtifactRef: "art_public", ArtifactRevision: "arv_public", Digest: "digest-public"}}}}}
	raw, err := json.Marshal(publicWorkflowInputs(inputs))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"developer", "run_public", "art_public", "arv_public", "digest-public"} {
		if !strings.Contains(string(raw), required) {
			t.Fatal("public accepted evidence missing")
		}
	}
	for _, forbidden := range []string{"private-", "runtimeRevision", "provenance", "sourceNodeId"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("private execution provenance leaked into prompt")
		}
	}
}
