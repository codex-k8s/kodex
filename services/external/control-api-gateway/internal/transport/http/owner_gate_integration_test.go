package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/usertext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestOwnerGateConsequencesLocalizeWithoutChangingIntent(t *testing.T) {
	texts, err := usertext.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"ru", "en"} {
		for _, suffix := range []string{"CONTINUE", "EXTERNAL_EFFECT", "REJECT_RUN", "REJECT_EFFECT", "CANCEL_RUN", "CANCEL_EFFECT", "REQUEST_CHANGES"} {
			id := "GATE_CONSEQUENCE_" + suffix
			literal := "i18n:" + id
			intent := map[string]any{"effectPreview": map[string]any{"value": literal}}
			consequence := map[string]any{"safeSummary": literal, "terminalForRun": false}
			value := map[string]any{"integrationIntent": intent, "decisionConsequences": []any{consequence}}
			LocalizeSafeErrors(value, func(id string) string { return texts.Localize(locale, id, nil) })
			if got := consequence["safeSummary"]; got == literal || got == id || got == "" {
				t.Fatalf("unresolved consequence %s/%s", locale, id)
			}
			if intent["effectPreview"].(map[string]any)["value"] != literal || consequence["terminalForRun"] != false {
				t.Fatal("owner data changed during localization")
			}
		}
	}
}

func integrationGateFixture() *cp.OwnerGate {
	preview, _ := structpb.NewStruct(map[string]any{
		"inputDigest": strings.Repeat("c", 64), "inputBytes": 64, "risk": "WRITE", "approvalPolicy": "HUMAN_EACH_EFFECT", "contentComplete": true,
		"fields": []any{map[string]any{"key": "body", "type": "STRING", "bytes": 13, "opaque": false, "value": "TYPE_original", "truncated": false}},
	})
	return &cp.OwnerGate{Ref: "gate_fixture01", Version: 4, ProjectRef: "prj_fixture01", State: cp.OwnerGateState_OWNER_GATE_STATE_APPROVED, Decision: cp.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE,
		SourceAttachmentSetRef: "aset_source01", ResolutionAttachmentSetRef: "aset_resolution01",
		AllowedDecisions: []cp.OwnerGateDecision{cp.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE, cp.OwnerGateDecision_OWNER_GATE_DECISION_REJECT},
		DecisionConsequences: []*cp.OwnerGateDecisionConsequence{
			{Decision: cp.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE, SafeSummary: "TYPE_preserve", ExecutesExternalEffect: true},
			{Decision: cp.OwnerGateDecision_OWNER_GATE_DECISION_REJECT},
		},
		IntegrationIntent: &cp.IntegrationIntent{ConnectionRef: "icn_fixture01", ConnectionName: "TYPE_connection", DefinitionKey: "github", CapabilityKey: "github.issue.create", Operation: "create", EffectKey: strings.Repeat("a", 64),
			ResourceScope: &cp.IntegrationResourceScope{Kind: cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_GITHUB_REPOSITORY, Values: map[string]string{"repository": "TYPE_repository", "owner": "i18n:literal"}, Digest: strings.Repeat("b", 64)}, EffectPreview: preview},
	}
}

func gateProjectionResponse(t *testing.T, kind string, gate *cp.OwnerGate) (int, map[string]any) {
	t.Helper()
	var response proto.Message
	method, path, body := "GET", "/api/v1/owner-gates/gate_fixture01", ""
	switch kind {
	case "list":
		response = &cp.ListOwnerGatesResponse{Gates: []*cp.OwnerGate{gate}, Total: 1}
		path = "/api/v1/owner-gates"
	case "get":
		response = &cp.GetOwnerGateResponse{Gate: gate}
	case "resolve":
		response = &cp.ResolveOwnerGateResponse{Gate: gate}
		method, path, body = "POST", "/api/v1/owner-gates/gate_fixture01/resolution", `{"decision":"APPROVE"}`
	}
	client := &catalogRPCRecorder{response: response}
	handler := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(client), Command: cp.NewPlatformCommandServiceClient(client)}})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, managedTestRequest(method, path, body))
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code == 200 {
		switch kind {
		case "list":
			result = result["items"].([]any)[0].(map[string]any)
		case "resolve":
			result = result["gate"].(map[string]any)
		}
	}
	return w.Code, result
}

func TestOwnerGateIntegrationProjectionParity(t *testing.T) {
	var expected map[string]any
	for _, kind := range []string{"get", "list", "resolve"} {
		code, result := gateProjectionResponse(t, kind, integrationGateFixture())
		if code != 200 {
			t.Fatalf("%s projection status: %d", kind, code)
		}
		if expected == nil {
			expected = result
		} else if !reflect.DeepEqual(expected, result) {
			t.Fatal("owner gate read paths disagree")
		}
		intent := result["integrationIntent"].(map[string]any)
		preview := intent["effectPreview"].(map[string]any)
		if intent["connectionName"] != "TYPE_connection" || preview["fields"].([]any)[0].(map[string]any)["value"] != "TYPE_original" || preview["contentComplete"] != true || intent["resourceScope"].(map[string]any)["values"].(map[string]any)["repository"] != "TYPE_repository" {
			t.Fatal("owner intent data changed")
		}
		consequences := result["decisionConsequences"].([]any)
		if consequences[0].(map[string]any)["safeSummary"] != "TYPE_preserve" || consequences[1].(map[string]any)["executesExternalEffect"] != false || consequences[1].(map[string]any)["terminalForRun"] != false {
			t.Fatal("consequence defaults or literal text lost")
		}
		if result["sourceAttachmentSetRef"] != "aset_source01" || result["resolutionAttachmentSetRef"] != "aset_resolution01" {
			t.Fatal("attachment lineage lost")
		}
		LocalizeSafeErrors(result, func(string) string { return "translated" })
		if intent["resourceScope"].(map[string]any)["values"].(map[string]any)["owner"] != "i18n:literal" {
			t.Fatal("literal scope was localized")
		}
	}
}

func TestOwnerGateIntegrationRejectsMalformedProjectionOnEveryPath(t *testing.T) {
	for name, change := range map[string]func(*cp.OwnerGate){
		"unknown state":       func(g *cp.OwnerGate) { g.State = cp.OwnerGateState(999) },
		"bad attachment pin":  func(g *cp.OwnerGate) { g.SourceAttachmentSetRef = "invalid/ref" },
		"missing scope":       func(g *cp.OwnerGate) { g.IntegrationIntent.ResourceScope = nil },
		"unknown kind":        func(g *cp.OwnerGate) { g.IntegrationIntent.ResourceScope.Kind = cp.IntegrationResourceKind(999) },
		"missing kind":        func(g *cp.OwnerGate) { g.IntegrationIntent.ResourceScope.Kind = 0 },
		"bad scope digest":    func(g *cp.OwnerGate) { g.IntegrationIntent.ResourceScope.Digest = "invalid" },
		"bad effect key":      func(g *cp.OwnerGate) { g.IntegrationIntent.EffectKey = "invalid" },
		"missing preview":     func(g *cp.OwnerGate) { g.IntegrationIntent.EffectPreview = nil },
		"missing connection":  func(g *cp.OwnerGate) { g.IntegrationIntent.ConnectionRef = "" },
		"unknown decision":    func(g *cp.OwnerGate) { g.DecisionConsequences[0].Decision = cp.OwnerGateDecision(999) },
		"missing consequence": func(g *cp.OwnerGate) { g.DecisionConsequences = g.DecisionConsequences[:1] },
		"duplicate consequence": func(g *cp.OwnerGate) {
			g.DecisionConsequences[1] = proto.Clone(g.DecisionConsequences[0]).(*cp.OwnerGateDecisionConsequence)
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, kind := range []string{"get", "list", "resolve"} {
				gate := integrationGateFixture()
				change(gate)
				code, result := gateProjectionResponse(t, kind, gate)
				if code < 500 || result["integrationIntent"] != nil || result["gate"] != nil || result["items"] != nil {
					t.Fatalf("%s exposed malformed projection", kind)
				}
			}
		})
	}
}

func TestOwnerGateReadPathsRejectNilGate(t *testing.T) {
	for _, kind := range []string{"get", "list", "resolve"} {
		code, _ := gateProjectionResponse(t, kind, nil)
		if code != 502 {
			t.Fatalf("%s accepted absent owner gate", kind)
		}
	}
}

func TestOrdinaryOwnerGateDoesNotInventIntegrationIntent(t *testing.T) {
	for _, kind := range []string{"get", "list", "resolve"} {
		gate := integrationGateFixture()
		gate.IntegrationIntent = nil
		code, result := gateProjectionResponse(t, kind, gate)
		if code != 200 || result["integrationIntent"] != nil {
			t.Fatal("ordinary gate projection changed")
		}
	}
}
