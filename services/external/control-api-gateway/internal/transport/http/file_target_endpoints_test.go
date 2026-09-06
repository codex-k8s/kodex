package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func fileTargetFixture() *cp.ListArtifactBindingTargetsResponse {
	return &cp.ListArtifactBindingTargetsResponse{ArtifactRef: "art_fixture01", ArtifactVersion: 3, ProjectRef: "prj_fixture01", Digest: strings.Repeat("a", 64), EvaluatedAt: timestamppb.New(time.Unix(1000, 0)), Total: 12, Page: &cp.PageInfo{NextPageToken: "next"}, Items: []*cp.ArtifactBindingTarget{{AgentRef: "agt_fixture01", AgentVersion: 2, Name: "Цель", State: cp.AgentState_AGENT_STATE_ARCHIVED, Bound: true, CanUnbind: true, BindReason: cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_AGENT_ARCHIVED, UnbindReason: cp.ArtifactBindingTargetReason_ARTIFACT_BINDING_TARGET_REASON_AVAILABLE}}}
}
func TestFileTargetHTTPPreservesOwnerEligibility(t *testing.T) {
	client := &catalogRPCRecorder{response: fileTargetFixture()}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/artifacts/art_fixture01/binding-targets?query=target&pageSize=3&pageToken=first", nil))
	request, ok := client.request.(*cp.ListArtifactBindingTargetsRequest)
	if w.Code != 200 || !ok || request.ArtifactRef != "art_fixture01" || request.Query != "target" || request.GetPage().GetPageSize() != 3 || request.GetPage().GetPageToken() != "first" || !strings.Contains(w.Body.String(), `"total":12`) || !strings.Contains(w.Body.String(), `"canUnbind":true`) {
		t.Fatalf("owner projection lost: %d %s", w.Code, w.Body.String())
	}
	for name, mutate := range map[string]func(*cp.ListArtifactBindingTargetsResponse){
		"foreign":                 func(v *cp.ListArtifactBindingTargetsResponse) { v.ArtifactRef = "art_foreign01" },
		"unknown reason":          func(v *cp.ListArtifactBindingTargetsResponse) { v.Items[0].BindReason = 999 },
		"unknown state":           func(v *cp.ListArtifactBindingTargetsResponse) { v.Items[0].State = 999 },
		"contradictory authority": func(v *cp.ListArtifactBindingTargetsResponse) { v.Items[0].CanBind = true },
		"duplicate":               func(v *cp.ListArtifactBindingTargetsResponse) { v.Items = append(v.Items, v.Items[0]) },
		"unsafe version":          func(v *cp.ListArtifactBindingTargetsResponse) { v.Items[0].AgentVersion = maximumSafeJSONInteger + 1 },
		"missing observation":     func(v *cp.ListArtifactBindingTargetsResponse) { v.EvaluatedAt = nil },
		"bad digest":              func(v *cp.ListArtifactBindingTargetsResponse) { v.Digest = "private" },
	} {
		t.Run(name, func(t *testing.T) {
			v := fileTargetFixture()
			mutate(v)
			if _, ok := fileBindingTargetsView(v, "art_fixture01", 100); ok {
				t.Fatal("invalid owner projection accepted")
			}
		})
	}
}
func TestRunAttachmentEligibilityHTTPChecksExactTarget(t *testing.T) {
	fixture := func() *cp.GetRunAttachmentEligibilityResponse {
		return &cp.GetRunAttachmentEligibilityResponse{ProjectRef: "prj_fixture01", Target: targetProto("WORKFLOW", "wfl_fixture01"), WorkflowVersionRef: "wfv_fixture01", Eligible: false, Reason: cp.RunAttachmentEligibilityReason_RUN_ATTACHMENT_ELIGIBILITY_REASON_RUNTIME_NOT_READY, Digest: strings.Repeat("a", 64), EvaluatedAt: timestamppb.New(time.Unix(1000, 0))}
	}
	client := &catalogRPCRecorder{response: fixture()}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/projects/prj_fixture01/run-attachment-eligibility?targetType=WORKFLOW&targetRef=wfl_fixture01", nil))
	q, ok := client.request.(*cp.GetRunAttachmentEligibilityRequest)
	if w.Code != 200 || !ok || q.GetTarget().GetWorkflowRef() != "wfl_fixture01" || q.ProjectRef != "prj_fixture01" || !strings.Contains(w.Body.String(), `"eligible":false`) {
		t.Fatalf("aggregate projection: %d %s", w.Code, w.Body.String())
	}
	for name, mutate := range map[string]func(*cp.GetRunAttachmentEligibilityResponse){
		"wrong target":         func(v *cp.GetRunAttachmentEligibilityResponse) { v.Target = targetProto("AGENT", "wfl_fixture01") },
		"wrong project":        func(v *cp.GetRunAttachmentEligibilityResponse) { v.ProjectRef = "prj_foreign01" },
		"unknown reason":       func(v *cp.GetRunAttachmentEligibilityResponse) { v.Reason = 999 },
		"false readiness":      func(v *cp.GetRunAttachmentEligibilityResponse) { v.Eligible = true },
		"invented run version": func(v *cp.GetRunAttachmentEligibilityResponse) { v.RunVersion = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			v := fixture()
			mutate(v)
			if _, ok := runAttachmentEligibilityView(v, "prj_fixture01", "WORKFLOW", "wfl_fixture01", ""); ok {
				t.Fatal("invalid aggregate accepted")
			}
		})
	}
}
