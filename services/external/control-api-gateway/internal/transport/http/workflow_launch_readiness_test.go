package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

func workflowReadinessFixture(w *cp.Workflow) {
	w.CardSummary = &cp.WorkflowCardSummary{}
	w.LaunchReadiness = &cp.WorkflowLaunchReadiness{WorkflowVersion: w.Version, RevisionRef: w.GetPublishedVersion().GetRef(), ContextDigest: strings.Repeat("c", 64), Reason: "DEPENDENCY_UNAVAILABLE", OperationalState: "BLOCKED"}
	if w.PublishedVersion == nil {
		w.LaunchReadiness.Reason = "UNPUBLISHED"
	}
}

func TestWorkflowLaunchReadinessKeepsSubmissionSeparateFromOperations(t *testing.T) {
	for _, operational := range []string{"READY", "UNKNOWN", "BLOCKED"} {
		w := &cp.Workflow{Ref: "wfl_fixture01", Version: 4, State: cp.WorkflowState_WORKFLOW_STATE_PUBLISHED, PublishedVersion: &cp.WorkflowVersion{Ref: "wfv_published01", Version: 2, Revision: 2, State: cp.WorkflowState_WORKFLOW_STATE_PUBLISHED}}
		workflowReadinessFixture(w)
		w.LaunchReadiness.AllowedToSubmit = true
		w.LaunchReadiness.Reason = "READY"
		w.LaunchReadiness.OperationalState = operational
		for _, response := range []proto.Message{&cp.GetWorkflowResponse{Workflow: w}, &cp.ListWorkflowsResponse{Workflows: []*cp.Workflow{w}}, &cp.UpdateWorkflowDraftResponse{Workflow: w}} {
			if _, err := messageMap(response); err != nil {
				t.Fatalf("readiness rejected: %v", err)
			}
		}
	}
	for _, reason := range []string{"PERMISSION_REQUIRED", "UNPUBLISHED", "DEPENDENCY_UNAVAILABLE"} {
		w := &cp.Workflow{Ref: "wfl_fixture01", Version: 4}
		workflowReadinessFixture(w)
		w.LaunchReadiness.Reason = reason
		output, err := messageMap(&cp.GetWorkflowResponse{Workflow: w})
		if err != nil || output["workflow"].(map[string]any)["launchReadiness"].(map[string]any)["allowedToSubmit"] != false {
			t.Fatal("disabled reason or false lost")
		}
	}
}

func TestWorkflowLaunchReadinessRejectsStaleAndUnknownOwnerProjection(t *testing.T) {
	for _, mutate := range []func(*cp.Workflow){
		func(w *cp.Workflow) { w.LaunchReadiness = nil },
		func(w *cp.Workflow) { w.LaunchReadiness.WorkflowVersion++ },
		func(w *cp.Workflow) { w.LaunchReadiness.Reason = "guessed" },
		func(w *cp.Workflow) { w.LaunchReadiness.OperationalState = "guessed" },
		func(w *cp.Workflow) { w.LaunchReadiness.RevisionRef = "wfv_other01" },
		func(w *cp.Workflow) { w.LaunchReadiness.AllowedToSubmit = true },
	} {
		w := &cp.Workflow{Ref: "wfl_fixture01", Version: 4}
		workflowReadinessFixture(w)
		mutate(w)
		client := &catalogRPCRecorder{response: &cp.GetWorkflowResponse{Workflow: w}}
		recorder := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(recorder, managedTestRequest("GET", "/api/v1/workflows/wfl_fixture01", ""))
		if recorder.Code != 502 {
			t.Fatalf("malformed readiness status %d", recorder.Code)
		}
	}
}
