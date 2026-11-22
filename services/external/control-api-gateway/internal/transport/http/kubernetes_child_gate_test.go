package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// kubernetesPublicGateFixture describes submitted scale-to-zero without raw body or provider state.
// kubernetesPublicGateFixture описывает submitted scale-to-zero без raw body либо provider state.
func kubernetesPublicGateFixture() *cp.OwnerGate {
	gate := gateCatalogFixture().Gates[0]
	gate.State = cp.OwnerGateState_OWNER_GATE_STATE_OPEN
	gate.AllowedDecisions = []cp.OwnerGateDecision{cp.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE, cp.OwnerGateDecision_OWNER_GATE_DECISION_REJECT, cp.OwnerGateDecision_OWNER_GATE_DECISION_CANCEL}
	for _, decision := range gate.AllowedDecisions {
		gate.DecisionConsequences = append(gate.DecisionConsequences, &cp.OwnerGateDecisionConsequence{Decision: decision, SafeSummary: "i18n:KUBERNETES_CHILD_GATE_APPROVE", ExecutesExternalEffect: decision == cp.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE})
	}
	gate.KubernetesChild = &cp.KubernetesOwnerGateChild{PlanRef: "kpl_fixture01", ChildRef: "kch_fixture01", Operation: "kubernetes.patch", PlanDigest: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), TargetRef: "ktg_fixture01", TargetRevision: 3,
		Resource: &cp.KubernetesOwnerGateResource{Group: "apps", Version: "v1", Resource: "deployments", Namespace: "example", Name: "web", Uid: "30000000-0000-4000-8000-000000000001", ResourceVersion: "17", Namespaced: true},
		Preview:  &cp.KubernetesOwnerGatePreview{InputDigest: strings.Repeat("b", 64), Changes: []*cp.KubernetesOwnerGatePreviewChange{{Operation: "replace", Path: "/spec/replicas", ValueKind: "NUMBER", ValueJson: "0"}}}}
	return gate
}

// TestKubernetesChildGateHTTPProjection exercises the actual Get/List generated router and normalizer.
// Zero values and empty declared arrays remain explicit, while unknown/mixed previews fail closed.
// TestKubernetesChildGateHTTPProjection проверяет actual Get/List generated router и normalizer.
// Zero values и empty declared arrays явны, unknown/mixed previews закрыто отклоняются.
func TestKubernetesChildGateHTTPProjection(t *testing.T) {
	for _, list := range []bool{false, true} {
		for _, mode := range []string{"valid", "digest", "preview", "hidden-value", "unknown-kind", "mixed", "decision"} {
			gate := kubernetesPublicGateFixture()
			switch mode {
			case "digest":
				gate.KubernetesChild.Preview.InputDigest = strings.Repeat("c", 64)
			case "preview":
				gate.KubernetesChild.Preview = nil
			case "hidden-value":
				gate.KubernetesChild.Preview.Changes[0].Redacted = true
				gate.KubernetesChild.Preview.Changes[0].ValueJson = `"private-sentinel"`
			case "unknown-kind":
				gate.KubernetesChild.Preview.Changes[0].ValueKind = "UNTRUSTED"
			case "mixed":
				gate.IntegrationIntent = &cp.IntegrationIntent{}
			case "decision":
				gate.AllowedDecisions = []cp.OwnerGateDecision{cp.OwnerGateDecision_OWNER_GATE_DECISION_REQUEST_CHANGES}
			}
			var response proto.Message = &cp.GetOwnerGateResponse{Gate: gate}
			path := "/api/v1/owner-gates/" + gate.Ref
			if list {
				response = &cp.ListOwnerGatesResponse{Gates: []*cp.OwnerGate{gate}, Total: 1}
				path = "/api/v1/owner-gates"
			}
			client := &catalogRPCRecorder{response: response}
			recorder := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
			if mode != "valid" {
				if recorder.Code != 502 || strings.Contains(recorder.Body.String(), "private-sentinel") {
					t.Fatalf("invalid child read escaped: list=%v mode=%s status=%d", list, mode, recorder.Code)
				}
				continue
			}
			if recorder.Code != 200 {
				t.Fatalf("valid child refused: list=%v status=%d", list, recorder.Code)
			}
			var body map[string]any
			if json.Unmarshal(recorder.Body.Bytes(), &body) != nil {
				t.Fatal("invalid HTTP JSON")
			}
			if list {
				body = body["items"].([]any)[0].(map[string]any)
			}
			child := body["kubernetesChild"].(map[string]any)
			preview := child["preview"].(map[string]any)
			if len(child) != 11 || len(preview["options"].([]any)) != 0 || len(preview["limitations"].([]any)) != 0 || preview["secretMaterialRef"] != "" || child["intendedName"] != "" {
				t.Fatal("declared public zeros lost")
			}
			change := preview["changes"].([]any)[0].(map[string]any)
			if change["valueJson"] != "0" || change["from"] != "" || change["redacted"] != false {
				t.Fatal("submitted zero or native patch semantics changed")
			}
		}
	}
}
