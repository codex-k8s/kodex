package httptransport

import (
	"encoding/json"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"net/http/httptest"
	"strings"
	"testing"
)

func fixtureCandidatePins(c *cp.IntegrationGrantCandidateContext) *cp.IntegrationGrantCandidatePins {
	p := &cp.IntegrationGrantCandidatePins{ContextDigest: strings.Repeat("a", 64)}
	if c.ConnectionRef != "" {
		p.ConnectionVersion = 2
		p.DefinitionVersion = "package-v2"
		p.DefinitionDigest = strings.Repeat("b", 64)
	}
	if c.ProjectRef != "" {
		p.ProjectVersion = 3
	}
	if c.RecipientRef != "" {
		p.RecipientVersion = 4
	}
	if c.WorkflowRef != "" {
		p.WorkflowRevisionRef = "wfr_fixture01"
	}
	return p
}
func TestIntegrationCandidateRoutesReachExactOwnerContext(t *testing.T) {
	agent := cp.IntegrationGrantRecipientKind_INTEGRATION_GRANT_RECIPIENT_KIND_AGENT
	contexts := []*cp.IntegrationGrantCandidateContext{{}, {ProjectRef: "prj_fixture01", RecipientKind: agent, RecipientRef: "agt_fixture01", CapabilityKey: "github.read", WorkflowRef: "wfl_fixture01", StepKey: "review"}, {ConnectionRef: "con_fixture01"}, {ConnectionRef: "con_fixture01", ProjectRef: "prj_fixture01", RecipientKind: agent}, {ConnectionRef: "con_fixture01", ProjectRef: "prj_fixture01", RecipientKind: agent, RecipientRef: "agt_fixture01"}}
	paths := []string{"connections?purpose=GRANT", "connections?purpose=USE&projectRef=prj_fixture01&recipientKind=AGENT&recipientRef=agt_fixture01&capabilityKey=github.read&workflowRef=wfl_fixture01&stepKey=review", "projects?connectionRef=con_fixture01", "recipients?connectionRef=con_fixture01&projectRef=prj_fixture01&recipientKind=AGENT", "capabilities?connectionRef=con_fixture01&projectRef=prj_fixture01&recipientKind=AGENT&recipientRef=agt_fixture01"}
	for n, path := range paths {
		t.Run(path, func(t *testing.T) {
			c := contexts[n]
			pins := fixtureCandidatePins(c)
			var response proto.Message
			switch n {
			case 0, 1:
				response = &cp.ListIntegrationGrantConnectionCandidatesResponse{Context: c, Pins: pins, ContextDigest: pins.ContextDigest}
			case 2:
				response = &cp.ListIntegrationGrantProjectCandidatesResponse{Context: c, Pins: pins, ContextDigest: pins.ContextDigest}
			case 3:
				response = &cp.ListIntegrationGrantRecipientCandidatesResponse{Context: c, Pins: pins, ContextDigest: pins.ContextDigest}
			case 4:
				response = &cp.ListIntegrationGrantCapabilityCandidatesResponse{Context: c, Pins: pins, ContextDigest: pins.ContextDigest}
			}
			client := &catalogRPCRecorder{response: response}
			handler := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(client)}})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/integration-grant-candidates/"+path+"&query=literal%25&pageSize=2&pageToken=owner-cursor", nil))
			if w.Code != 200 || client.request == nil {
				t.Fatalf("query: %d %s", w.Code, w.Body.String())
			}
			req := client.request.ProtoReflect()
			got := req.Get(req.Descriptor().Fields().ByName("context")).Message().Interface()
			if !proto.Equal(got, c) || req.Get(req.Descriptor().Fields().ByName("query")).String() != "literal%" {
				t.Fatal("owner context lost")
			}
			if n < 2 {
				want := cp.IntegrationCandidatePurpose_INTEGRATION_CANDIDATE_PURPOSE_GRANT
				if n == 1 {
					want = cp.IntegrationCandidatePurpose_INTEGRATION_CANDIDATE_PURPOSE_USE
				}
				if client.request.(*cp.ListIntegrationGrantConnectionCandidatesRequest).Purpose != want {
					t.Fatal("purpose lost")
				}
			}
			var body map[string]any
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || body["total"] != float64(0) || body["items"] == nil || body["context"] == nil || body["pins"] == nil {
				t.Fatal("empty authoritative page lost")
			}
		})
	}
}
func TestIntegrationCandidateRejectsInvalidPrefixBeforeRPC(t *testing.T) {
	for _, path := range []string{
		"connections", "connections?purpose=UNKNOWN", "connections?purpose=GRANT&projectRef=prj_fixture01", "connections?purpose=GRANT&recipientKind=UNKNOWN",
		"connections?purpose=USE", "connections?purpose=USE&projectRef=prj_fixture01&recipientKind=WORKFLOW&recipientRef=wfl_fixture01&capabilityKey=github.read",
		"connections?purpose=USE&projectRef=prj_fixture01&recipientKind=AGENT&recipientRef=agt_fixture01&capabilityKey=github.read&workflowRef=wfl_fixture01",
		"connections?purpose=GRANT&purpose=USE", "connections?purpose=GRANT&connectionRef=con_fixture01", "connections?purpose=GRANT&pageSize=101",
		"connections?purpose=GRANT&query=" + strings.Repeat("x", 201), "projects?connectionRef=con_fixture01&projectRef=prj_fixture01",
		"recipients?connectionRef=con_fixture01", "capabilities?connectionRef=con_fixture01&projectRef=prj_fixture01&recipientKind=UNKNOWN&recipientRef=agt_fixture01",
	} {
		t.Run(path, func(t *testing.T) {
			client := &catalogRPCRecorder{}
			handler := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(client)}})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/integration-grant-candidates/"+path, nil))
			if w.Code != 400 || client.request != nil {
				t.Fatalf("invalid prefix reached owner: %d", w.Code)
			}
		})
	}
}
func fixtureConnectionCandidatePage() *cp.ListIntegrationGrantConnectionCandidatesResponse {
	c := &cp.IntegrationGrantCandidateContext{}
	itemContext := &cp.IntegrationGrantCandidateContext{ConnectionRef: "con_fixture01"}
	return &cp.ListIntegrationGrantConnectionCandidatesResponse{Context: c, Pins: fixtureCandidatePins(c), ContextDigest: strings.Repeat("a", 64), Total: 1, Items: []*cp.IntegrationGrantConnectionCandidate{{ConnectionRef: itemContext.ConnectionRef, Name: "Connection", DefinitionKey: "github", ProviderName: "GitHub", Grantable: true, Reason: cp.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_READY, Pins: fixtureCandidatePins(itemContext)}}}
}
func TestIntegrationCandidateResponseRejectsTampering(t *testing.T) {
	for name, mutate := range map[string]func(*cp.ListIntegrationGrantConnectionCandidatesResponse){
		"foreign context": func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) { v.Context.ProjectRef = "prj_foreign01" },
		"missing context": func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) { v.Context = nil },
		"foreign digest": func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) {
			v.ContextDigest = strings.Repeat("c", 64)
		},
		"missing pins": func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) { v.Items[0].Pins = nil },
		"unsafe version": func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) {
			v.Items[0].Pins.ConnectionVersion = maximumSafeJSONInteger + 1
		},
		"unknown reason": func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) {
			v.Items[0].Reason = cp.IntegrationCandidateReason(99)
		},
		"both grants":             func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) { v.Items[0].Usable = true },
		"false ready":             func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) { v.Items[0].Grantable = false },
		"unknown credential kind": func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) { v.Items[0].CredentialKind = "API_KEY" },
		"premature resource scope": func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) {
			v.Items[0].ResourceScope = map[string]string{"repo": "private"}
		},
		"duplicate": func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) {
			v.Items = append(v.Items, v.Items[0])
			v.Total = 2
		},
		"wrong total": func(v *cp.ListIntegrationGrantConnectionCandidatesResponse) { v.Total = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			v := fixtureConnectionCandidatePage()
			mutate(v)
			w := httptest.NewRecorder()
			writeCandidatePage(w, v, &cp.IntegrationGrantCandidateContext{}, "GRANT", 0, 100)
			if w.Code != 502 {
				t.Fatalf("tamper accepted: %d", w.Code)
			}
		})
	}
	v := fixtureConnectionCandidatePage()
	out, ok := candidatePageView(v, &cp.IntegrationGrantCandidateContext{}, "GRANT", 0, 100)
	if !ok {
		t.Fatal("valid candidate rejected")
	}
	item := out["items"].([]any)[0].(map[string]any)
	if item["usable"] != false || item["resourceScope"] == nil {
		t.Fatal("false/empty contract lost")
	}
}
func TestIntegrationCandidateUseCannotBecomeGrant(t *testing.T) {
	c := &cp.IntegrationGrantCandidateContext{ProjectRef: "prj_fixture01", RecipientKind: cp.IntegrationGrantRecipientKind_INTEGRATION_GRANT_RECIPIENT_KIND_AGENT, RecipientRef: "agt_fixture01", CapabilityKey: "github.read"}
	v := fixtureConnectionCandidatePage()
	v.Context = c
	v.Pins = fixtureCandidatePins(c)
	i := v.Items[0]
	i.Grantable = false
	i.Usable = true
	ic := proto.Clone(c).(*cp.IntegrationGrantCandidateContext)
	ic.ConnectionRef = i.ConnectionRef
	i.Pins = fixtureCandidatePins(ic)
	if _, ok := candidatePageView(v, c, "USE", 0, 100); !ok {
		t.Fatal("valid use rejected")
	}
	i.Pins.RecipientVersion++
	if _, ok := candidatePageView(v, c, "USE", 0, 100); ok {
		t.Fatal("different selected recipient version accepted")
	}
	i.Pins.RecipientVersion--
	i.Grantable = true
	if _, ok := candidatePageView(v, c, "USE", 0, 100); ok {
		t.Fatal("use expanded to grant")
	}
}

func TestIntegrationCandidateDependentRowsKeepSelectedPins(t *testing.T) {
	c := &cp.IntegrationGrantCandidateContext{ConnectionRef: "con_fixture01"}
	ic := proto.Clone(c).(*cp.IntegrationGrantCandidateContext)
	ic.ProjectRef = "prj_fixture01"
	p := &cp.ListIntegrationGrantProjectCandidatesResponse{Context: c, Pins: fixtureCandidatePins(c), ContextDigest: strings.Repeat("a", 64), Total: 1, Items: []*cp.IntegrationGrantProjectCandidate{{ProjectRef: ic.ProjectRef, Name: "Project", Grantable: true, Reason: cp.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_READY, Pins: fixtureCandidatePins(ic)}}}
	if _, ok := candidatePageView(p, c, "GRANT", 1, 100); !ok {
		t.Fatal("valid project rejected")
	}
	p.Items[0].Pins.DefinitionVersion = "other-version"
	if _, ok := candidatePageView(p, c, "GRANT", 1, 100); ok {
		t.Fatal("selected package changed")
	}
	c = ic
	c.RecipientKind = cp.IntegrationGrantRecipientKind_INTEGRATION_GRANT_RECIPIENT_KIND_WORKFLOW
	ic = proto.Clone(c).(*cp.IntegrationGrantCandidateContext)
	ic.RecipientKind = cp.IntegrationGrantRecipientKind_INTEGRATION_GRANT_RECIPIENT_KIND_WORKFLOW
	ic.RecipientRef = "wfl_fixture01"
	r := &cp.ListIntegrationGrantRecipientCandidatesResponse{Context: c, Pins: fixtureCandidatePins(c), ContextDigest: strings.Repeat("a", 64), Total: 1, Items: []*cp.IntegrationGrantRecipientCandidate{{RecipientRef: ic.RecipientRef, RecipientKind: ic.RecipientKind, ProjectRef: c.ProjectRef, Name: "Workflow", Grantable: true, Reason: cp.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_READY, Pins: fixtureCandidatePins(ic)}}}
	if _, ok := candidatePageView(r, c, "GRANT", 2, 100); !ok {
		t.Fatal("valid workflow recipient rejected")
	}
	r.Items[0].ProjectRef = "prj_foreign01"
	if _, ok := candidatePageView(r, c, "GRANT", 2, 100); ok {
		t.Fatal("foreign recipient project accepted")
	}
	c = ic
	capability := &cp.IntegrationCapability{Key: "github.read", Name: "Read", Operation: "github.read", TypedRisk: cp.IntegrationRisk_INTEGRATION_RISK_READ, ApprovalPolicy: cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE, ResourceKind: cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_GITHUB_REPOSITORY}
	capability.InputFields = []*cp.IntegrationConfigurationField{{Key: "limit", ValueType: "INTEGER", HasMinimum: true, Minimum: 0}}
	v := &cp.ListIntegrationGrantCapabilityCandidatesResponse{Context: c, Pins: fixtureCandidatePins(c), ContextDigest: strings.Repeat("a", 64), Total: 1, Items: []*cp.IntegrationGrantCapabilityCandidate{{Capability: capability, Grantable: true, Reason: cp.IntegrationCandidateReason_INTEGRATION_CANDIDATE_REASON_READY, CurrentGrantRef: "igr_fixture01", CurrentGrantVersion: 3, Pins: fixtureCandidatePins(c)}}}
	result, ok := candidatePageView(v, c, "GRANT", 3, 100)
	if !ok {
		t.Fatal("valid capability rejected")
	}
	capView := result["items"].([]any)[0].(map[string]any)["capability"].(map[string]any)
	field := capView["inputFields"].([]any)[0].(map[string]any)
	if capView["approvalRequired"] != false || field["minimum"] != float64(0) || field["label"] != "" || field["help"] != "" || field["required"] != false {
		t.Fatal("capability constraints or required zero values lost")
	}
	v.Items[0].CurrentGrantVersion = 0
	if _, ok := candidatePageView(v, c, "GRANT", 3, 100); ok {
		t.Fatal("unversioned current grant accepted")
	}
	v.Items[0].CurrentGrantVersion = 3
	capability.ApprovalPolicy = 0
	if _, ok := candidatePageView(v, c, "GRANT", 3, 100); ok {
		t.Fatal("unspecified approval policy accepted")
	}
}
