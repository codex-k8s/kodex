package httptransport

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

func TestIntegrationGrantPublicProjectionKeepsParentPinAndNativeGrant(t *testing.T) {
	for _, recipient := range []string{"agt_systemfixture", "agt_projectfixture"} {
		t.Run(recipient, func(t *testing.T) {
			grant := &cp.IntegrationGrant{
				Ref: "igr_fixture01", Version: 1, ConnectionVersion: 12,
				AgentRef: recipient, CapabilityKey: "context7.docs.query", TargetName: "Assistant",
				Enabled: true, Risk: "READ", TypedRisk: cp.IntegrationRisk_INTEGRATION_RISK_READ,
				ApprovalPolicy: cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE,
				ResourceScope:  &cp.IntegrationResourceScope{Kind: cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_HTTPS_RESOURCE},
			}
			connection := &cp.IntegrationConnection{Ref: "con_fixture01", Version: 12, Grants: []*cp.IntegrationGrant{grant}}
			original := proto.Clone(connection)
			value, err := ProtoMap(connection)
			if err != nil {
				t.Fatal("public grant projection failed")
			}
			grants, ok := value["grants"].([]any)
			if !ok || len(grants) != 1 {
				t.Fatal("public grant list was lost")
			}
			public, ok := grants[0].(map[string]any)
			if !ok {
				t.Fatal("public grant shape changed")
			}
			if _, exists := public["connectionVersion"]; exists {
				t.Fatal("internal grant connection pin escaped public schema")
			}
			if value["version"] != float64(12) || public["version"] != float64(1) || public["agentRef"] != recipient || public["enabled"] != true || public["approvalPolicy"] != "NONE" || public["risk"] != "READ" {
				t.Fatal("public parent pin or grant semantics changed")
			}
			if !proto.Equal(original, connection) || grant.ConnectionVersion != 12 {
				t.Fatal("public projection mutated native runtime authority")
			}
		})
	}
}

func TestIntegrationGrantPublicProjectionStillRejectsUnknownRisk(t *testing.T) {
	_, err := ProtoMap(&cp.IntegrationConnection{Ref: "con_fixture01", Version: 12, Grants: []*cp.IntegrationGrant{{
		Ref: "igr_fixture01", Version: 1, ConnectionVersion: 12, AgentRef: "agt_systemfixture",
		TypedRisk: cp.IntegrationRisk(999),
	}}})
	if err == nil {
		t.Fatal("unknown grant risk was accepted")
	}
}

