package platform

import (
	"encoding/json"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"strings"
	"testing"
)

// TestWorkflowReworkPreviewNeverPublishesPrivatePins validates actual serialized public bytes.
// TestWorkflowReworkPreviewNeverPublishesPrivatePins проверяет actual serialized public bytes.
func TestWorkflowReworkPreviewNeverPublishesPrivatePins(t *testing.T) {
	root, anchors := reworkPlanFixture()
	plan, err := buildWorkflowUpstreamPlan(root, anchors, "r", 1, []workflowReviewRejection{{"review", "author", "spec"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	plan.Targets[0].OriginNodeID = "private-node-pin"
	gate := entity.OwnerGate{State: "OPEN"}
	if projectWorkflowReworkPreview(&gate, asJSON(plan)) != nil || gate.ReworkPlan == nil {
		t.Fatal("bounded public plan missing")
	}
	raw, _ := json.Marshal(gate.ReworkPlan)
	if strings.Contains(string(raw), "private-node-pin") || strings.Contains(string(raw), "originNodeId") || gate.ReworkPlan.Authors[0].NextAttempt != 2 {
		t.Fatal("private authority leaked or counter lost")
	}
	plan.BlockReason = "WORKFLOW_ATTEMPTS_EXHAUSTED"
	plan.Targets[0].PreviousAttempt = 4
	plan.Targets[0].NextAttempt = 5
	if projectWorkflowReworkPreview(&gate, asJSON(plan)) != nil || gate.ReworkPlan.Authors[0].NextAttempt != 0 {
		t.Fatal("attempt five was advertised")
	}
}

// TestWorkflowSupersededProjectionCannotMasqueradeAsOwnerCancel preserves distinct closed history.
// TestWorkflowSupersededProjectionCannotMasqueradeAsOwnerCancel сохраняет различие closed history.
func TestWorkflowSupersededProjectionCannotMasqueradeAsOwnerCancel(t *testing.T) {
	gate := entity.OwnerGate{State: "CANCELLED", ResolutionKind: "SUPERSEDED"}
	if projectWorkflowReworkPreview(&gate, nil) != nil {
		t.Fatal("legitimate supersession rejected")
	}
	for _, mutate := range []func(*entity.OwnerGate){func(g *entity.OwnerGate) { g.Decision = "CANCEL" }, func(g *entity.OwnerGate) { g.State = "EXPIRED" }, func(g *entity.OwnerGate) { g.ResolutionKind = "CANCELLED" }} {
		candidate := gate
		mutate(&candidate)
		if projectWorkflowReworkPreview(&candidate, nil) == nil {
			t.Fatal("false human or expiry history accepted")
		}
	}
}
