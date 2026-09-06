package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

func TestWorkflowSaveAndReadPreserveDraftAlongsidePublication(t *testing.T) {
	workflow := &cp.Workflow{Ref: "wfl_fixture01", Version: 4, State: cp.WorkflowState_WORKFLOW_STATE_DRAFT,
		DraftVersion: &cp.WorkflowVersion{Ref: "wfv_draft01", Version: 3, Revision: 3, State: cp.WorkflowState_WORKFLOW_STATE_DRAFT,
			Steps: []*cp.WorkflowStep{{Ref: "stage_saved", Purpose: "saved purpose"}}, InputFields: []*cp.WorkflowInputField{{Key: "draft_input"}}},
		PublishedVersion: &cp.WorkflowVersion{Ref: "wfv_published01", Version: 2, Revision: 2, State: cp.WorkflowState_WORKFLOW_STATE_PUBLISHED,
			Steps: []*cp.WorkflowStep{{Ref: "stage_published", Purpose: "published purpose"}}},
	}
	workflowReadinessFixture(workflow)
	workflow.LaunchReadiness.Reason = "UNPUBLISHED"
	for _, test := range []struct {
		method, path, body string
		response           proto.Message
	}{
		{"PATCH", "/api/v1/workflows/wfl_fixture01", `{"name":"Workflow","purpose":"Purpose","coordinatorAgentRef":"agt_fixture01"}`, &cp.UpdateWorkflowDraftResponse{Workflow: workflow}},
		{"GET", "/api/v1/workflows/wfl_fixture01", "", &cp.GetWorkflowResponse{Workflow: workflow}},
	} {
		t.Run(test.method, func(t *testing.T) {
			client := &catalogRPCRecorder{response: test.response}
			handler := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(client), Command: cp.NewPlatformCommandServiceClient(client)}})
			writer := httptest.NewRecorder()
			handler.ServeHTTP(writer, managedTestRequest(test.method, test.path, test.body))
			if writer.Code != 200 {
				t.Fatalf("workflow status: %d", writer.Code)
			}
			var result generated.Workflow
			if err := json.Unmarshal(writer.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Draft == nil || result.Draft.Ref != "wfv_draft01" || result.DraftRevisionRef == nil || *result.DraftRevisionRef != result.Draft.Ref || result.Draft.Version != 3 || result.Draft.Revision != 3 || result.Draft.Steps[0].Ref != "stage_saved" || result.Draft.InputFields[0].Key != "draft_input" {
				t.Fatal("saved draft body or pins lost")
			}
			if result.PublishedRevisionRef == nil || result.RevisionRef == nil || *result.RevisionRef != *result.PublishedRevisionRef || *result.RevisionRef != "wfv_published01" || result.Steps[0].Ref != "stage_published" {
				t.Fatal("published body or pin changed")
			}
			if result.LaunchReadiness.AllowedToSubmit || result.LaunchReadiness.Reason != "UNPUBLISHED" {
				t.Fatal("saved draft inherited published launch admission")
			}
		})
	}
}

func TestWorkflowReadRejectsMalformedDraftBesideValidPublication(t *testing.T) {
	for _, draft := range []*cp.WorkflowVersion{
		{Ref: "bad/ref", Version: 3, Revision: 3, State: cp.WorkflowState_WORKFLOW_STATE_DRAFT},
		{Ref: "wfv_draft01", Version: 0, Revision: 3, State: cp.WorkflowState_WORKFLOW_STATE_DRAFT},
		{Ref: "wfv_draft01", Version: 3, Revision: 0, State: cp.WorkflowState_WORKFLOW_STATE_DRAFT},
		{Ref: "wfv_draft01", Version: 3, Revision: 3, State: cp.WorkflowState(999)},
	} {
		workflow := &cp.Workflow{Ref: "wfl_fixture01", Version: 4, DraftVersion: draft,
			PublishedVersion: &cp.WorkflowVersion{Ref: "wfv_published01", Version: 2, Revision: 2, State: cp.WorkflowState_WORKFLOW_STATE_PUBLISHED},
		}
		workflowReadinessFixture(workflow)
		client := &catalogRPCRecorder{response: &cp.GetWorkflowResponse{Workflow: workflow}}
		writer := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(writer, managedTestRequest("GET", "/api/v1/workflows/wfl_fixture01", ""))
		if writer.Code < 500 {
			t.Fatal("malformed draft was hidden by valid publication")
		}
	}
}
