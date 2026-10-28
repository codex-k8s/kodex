package httptransport

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"testing"
)

// TestWorkflowReworkPreviewSurvivesActualPublicRoutes proves real proto defaults and all endpoints.
// TestWorkflowReworkPreviewSurvivesActualPublicRoutes проверяет real proto defaults и все endpoints.
func TestWorkflowReworkPreviewSurvivesActualPublicRoutes(t *testing.T) {
	for _, route := range []string{"get", "list", "resolve"} {
		t.Run(route, func(t *testing.T) {
			gate := integrationGateFixture()
			gate.IntegrationIntent = nil
			gate.ReworkPlan = &cp.WorkflowReworkPreview{Schema: "kodex.workflow-upstream-rework-preview.v1", SourceStepKey: "review", SourceAttempt: 1, Authors: []*cp.WorkflowReworkStepPreview{{StepKey: "author", PreviousAttempt: 1, NextAttempt: 2}}, Rechecks: []*cp.WorkflowReworkStepPreview{{StepKey: "review", PreviousAttempt: 1, NextAttempt: 2}, {StepKey: "not-started", PreviousAttempt: 0, NextAttempt: 1}}}
			code, result := gateProjectionResponse(t, route, gate)
			if code != 200 {
				t.Fatalf("public rework route rejected: status=%d", code)
			}
			plan, ok := result["reworkPlan"].(map[string]any)
			if !ok {
				t.Fatal("public rework plan lost")
			}
			checks, ok := plan["rechecks"].([]any)
			if !ok || len(checks) != 2 {
				t.Fatal("public deferred checks lost")
			}
			unstarted := checks[1].(map[string]any)
			if unstarted["previousAttempt"] != float64(0) || unstarted["nextAttempt"] != float64(1) {
				t.Fatal("zero previous counter disappeared during proto mapping")
			}
		})
	}
}

// TestWorkflowSupersessionPublicShapeRejectsFalseOwnerDecision preserves closure semantics.
// TestWorkflowSupersessionPublicShapeRejectsFalseOwnerDecision сохраняет closure semantics.
func TestWorkflowSupersessionPublicShapeRejectsFalseOwnerDecision(t *testing.T) {
	gate := integrationGateFixture()
	gate.IntegrationIntent = nil
	gate.State = cp.OwnerGateState_OWNER_GATE_STATE_CANCELLED
	gate.Decision = cp.OwnerGateDecision_OWNER_GATE_DECISION_UNSPECIFIED
	gate.ResolutionKind = "SUPERSEDED"
	code, result := gateProjectionResponse(t, "get", gate)
	if code != 200 || result["resolutionKind"] != "SUPERSEDED" || result["decision"] != nil {
		t.Fatal("supersession became an owner cancel decision")
	}
	gate.Decision = cp.OwnerGateDecision_OWNER_GATE_DECISION_CANCEL
	code, _ = gateProjectionResponse(t, "get", gate)
	if code < 500 {
		t.Fatal("false owner CANCEL was accepted")
	}
}
