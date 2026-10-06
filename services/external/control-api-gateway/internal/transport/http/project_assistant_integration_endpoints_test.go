package httptransport

import (
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

func TestProjectAssistantGrantCandidateProjectionRequiresExactProfile(t *testing.T) {
	base := &cp.GetProjectAssistantIntegrationGrantCandidatesResponse{
		ScopeKind:       cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION,
		OrganizationRef: "org_fixture01", AssistantRef: "agt_fixture01", AssistantVersion: 2,
		ProjectRef: "prj_fixture01", AssistantProfileRef: "asstp_fixture01", ProfileVersion: 1,
		ConnectionRef: "icon_fixture01", ConnectionVersion: 3, DefinitionVersion: "3.1.0", DefinitionDigest: strings.Repeat("a", 64),
		Items: []*cp.SystemAssistantIntegrationGrantCandidate{{Capability: &cp.IntegrationCapability{
			Key: "context7.query-docs", Name: "Query", TypedRisk: cp.IntegrationRisk_INTEGRATION_RISK_READ,
			ApprovalPolicy:          cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE,
			AllowedApprovalPolicies: []cp.IntegrationApprovalPolicy{cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE}},
			Grantable: true, Reason: cp.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_READY}}, Total: 1, Page: &cp.PageInfo{},
	}
	view, ok := projectAssistantGrantCandidatePage(base, base.ProjectRef, base.ConnectionRef, 50)
	if !ok || view["scopeKind"] != "ORGANIZATION" || view["items"].([]map[string]any)[0]["currentGrantEnabled"] != false {
		t.Fatal("exact organization candidate projection was rejected")
	}
	if _, present := view["nextPageToken"]; present {
		t.Fatal("terminal candidate page must omit the optional cursor")
	}
	continued := proto.Clone(base).(*cp.GetProjectAssistantIntegrationGrantCandidatesResponse)
	continued.Page.NextPageToken = "fixture_cursor"
	continuedView, continuedOK := projectAssistantGrantCandidatePage(continued, base.ProjectRef, base.ConnectionRef, 50)
	if !continuedOK || continuedView["nextPageToken"] != "fixture_cursor" {
		t.Fatal("candidate continuation cursor was lost")
	}
	for name, mutate := range map[string]func(*cp.GetProjectAssistantIntegrationGrantCandidatesResponse){
		"foreign project":         func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) { v.ProjectRef = "prj_foreign01" },
		"missing profile":         func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) { v.AssistantProfileRef = "" },
		"missing profile version": func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) { v.ProfileVersion = 0 },
		"unknown scope":           func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) { v.ScopeKind = 0 },
		"project scope": func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) {
			v.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
		},
		"foreign connection": func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) { v.ConnectionRef = "icon_foreign01" },
		"missing owner":      func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) { v.AssistantRef = "" },
		"unsafe version": func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) {
			v.ConnectionVersion = maximumSafeJSONInteger + 1
		},
		"missing digest": func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) { v.DefinitionDigest = "" },
		"duplicate": func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) {
			v.Items = append(v.Items, v.Items[0])
			v.Total = 2
		},
		"missing policy set": func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) {
			v.Items[0].Capability.AllowedApprovalPolicies = nil
		},
		"false ready": func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) { v.Items[0].Grantable = false },
		"incomplete grant": func(v *cp.GetProjectAssistantIntegrationGrantCandidatesResponse) {
			v.Items[0].CurrentGrantRef = "igr_fixture01"
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := proto.Clone(base).(*cp.GetProjectAssistantIntegrationGrantCandidatesResponse)
			mutate(v)
			if _, ok := projectAssistantGrantCandidatePage(v, base.ProjectRef, base.ConnectionRef, 50); ok {
				t.Fatal("malformed candidate projection was accepted")
			}
		})
	}
}
