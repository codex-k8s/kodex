package platform

import (
	"encoding/json"
	"strings"
	"testing"

	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
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

// TestWorkflowStageTaskSeparatesFinalAndCaptureShapes checks the actual task consumed by initial
// and recovery attempts, preserving both distinct parsers and the bounded assembled-task limit.
// TestWorkflowStageTaskSeparatesFinalAndCaptureShapes проверяет реальную задачу первой и повторной
// попытки, сохраняя разные формы финального ответа и capture, а также предел размера инструкции.
func TestWorkflowStageTaskSeparatesFinalAndCaptureShapes(t *testing.T) {
	root := &serverWorkflowRoot{Version: entity.WorkflowVersion{ExecutionMode: workflowservice.ServerDAGV1}}
	step := entity.WorkflowStep{ResultRequirements: []entity.WorkflowResultRequirement{{Key: "analysis_file", Kind: "ARTIFACT", FileName: "release-scope.md", MediaType: "text/markdown"}}}
	task, err := workflowStageTask(root, step, nil)
	if err != nil {
		t.Fatal(err)
	}
	finalCount, captureCount := 0, 0
	for _, line := range strings.Split(task, "\n") {
		if strings.HasPrefix(line, `{"schema":"kodex.workflow-result.v1"`) {
			finalCount++
			if _, err := workflowservice.DecodeDeclaration(line, step.ResultRequirements); err != nil {
				t.Fatal("actual stage task contains invalid final example")
			}
		}
		if strings.HasPrefix(line, `{"schema":"kodex.workflow-capture-progress.v1"`) {
			captureCount++
			if _, err := workflowservice.DecodeCaptureProgress(line, root.Version.ExecutionMode, step.ResultRequirements); err != nil {
				t.Fatal("actual stage task contains invalid capture example")
			}
		}
	}
	if finalCount != 1 || captureCount != 1 {
		t.Fatal("final and capture examples must be distinct and complete")
	}
	root.Task = strings.Repeat("x", 64<<10)
	if task, err := workflowStageTask(root, step, nil); err == nil || task != "" {
		t.Fatal("oversized assembled task accepted")
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
