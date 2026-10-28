package platform

import (
	"testing"

	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// freshnessAnchorFixture creates completed owner history with an exact physical producer marker.
// freshnessAnchorFixture создаёт completed owner history с exact physical producer marker.
func freshnessAnchorFixture(key string, inputs []workflowInputReceipt) serverWorkflowAnchor {
	return serverWorkflowAnchor{ID: key, Key: key, AssignedAttempt: 1, LatestAttempt: 1, Inputs: inputs, Receipts: []entity.WorkflowAttemptResult{{Attempt: 1, Accepted: true, Provenance: &entity.WorkflowCompletionProvenance{OriginNodeID: key, NodeID: key + "-first"}}}}
}

// TestEffectiveWorkflowResultsInvalidateEveryDependentPASS preserves immutable old verdicts.
// Only fresh receipts can restore acceptance; an unrelated branch stays current throughout.
// TestEffectiveWorkflowResultsInvalidateEveryDependentPASS сохраняет immutable old verdicts.
// Только fresh receipts восстанавливают приёмку; независимая ветвь остаётся current.
func TestEffectiveWorkflowResultsInvalidateEveryDependentPASS(t *testing.T) {
	root := &serverWorkflowRoot{Version: entity.WorkflowVersion{ReworkPolicy: workflowservice.OwnerApprovedUpstreamV1, Steps: []entity.WorkflowStep{{Key: "author"}, {Key: "r", DependsOn: []string{"author"}}, {Key: "q", DependsOn: []string{"author"}}, {Key: "independent"}}}}
	author := freshnessAnchorFixture("author", nil)
	input := []workflowInputReceipt{{StepKey: "author", SourceNodeID: author.ID, Receipt: author.Receipts[0]}}
	anchors := map[string]serverWorkflowAnchor{"author": author, "r": freshnessAnchorFixture("r", input), "q": freshnessAnchorFixture("q", input), "independent": freshnessAnchorFixture("independent", nil)}
	if len(effectiveServerWorkflowResults(root, anchors)) != 4 {
		t.Fatal("original complete graph rejected")
	}
	author.LatestAttempt = 2
	anchors["author"] = author
	effective := effectiveServerWorkflowResults(root, anchors)
	if len(effective) != 1 || effective["independent"].Attempt != 1 {
		t.Fatal("queued author retry left a dependent PASS effective")
	}
	author.Receipts = append(author.Receipts, entity.WorkflowAttemptResult{Attempt: 2, Accepted: true, Provenance: &entity.WorkflowCompletionProvenance{OriginNodeID: author.ID, NodeID: "author-second"}})
	anchors["author"] = author
	if len(effectiveServerWorkflowResults(root, anchors)) != 2 {
		t.Fatal("new author incorrectly accepted stale reviews")
	}
	for _, key := range []string{"r", "q"} {
		next := anchors[key]
		next.LatestAttempt = 2
		next.Receipts = append(next.Receipts, entity.WorkflowAttemptResult{Attempt: 2, Accepted: true, Provenance: &entity.WorkflowCompletionProvenance{OriginNodeID: next.ID, NodeID: key + "-second"}})
		next.Inputs = []workflowInputReceipt{{StepKey: "author", SourceNodeID: author.ID, Receipt: author.Receipts[1]}}
		anchors[key] = next
	}
	if len(effectiveServerWorkflowResults(root, anchors)) != 4 {
		t.Fatal("fresh real review attempts did not restore acceptance")
	}
	if anchors["r"].Receipts[0].Accepted != true || anchors["q"].Receipts[0].Accepted != true {
		t.Fatal("historical acceptance was rewritten")
	}
}

// TestLegacyWorkflowFreshnessDoesNotRetroactivelyReinterpretPublishedInputs preserves old policy.
// TestLegacyWorkflowFreshnessDoesNotRetroactivelyReinterpretPublishedInputs сохраняет старую policy.
func TestLegacyWorkflowFreshnessDoesNotRetroactivelyReinterpretPublishedInputs(t *testing.T) {
	root := &serverWorkflowRoot{Version: entity.WorkflowVersion{Steps: []entity.WorkflowStep{{Key: "a"}, {Key: "b", DependsOn: []string{"a"}}}}}
	anchors := map[string]serverWorkflowAnchor{"a": freshnessAnchorFixture("a", nil), "b": freshnessAnchorFixture("b", nil)}
	if len(effectiveServerWorkflowResults(root, anchors)) != 2 {
		t.Fatal("legacy accepted result was reinterpreted")
	}
	root.Version.ReworkPolicy = workflowservice.OwnerApprovedUpstreamV1
	if len(effectiveServerWorkflowResults(root, anchors)) != 1 {
		t.Fatal("explicit new profile ignored missing pinned inputs")
	}
}
