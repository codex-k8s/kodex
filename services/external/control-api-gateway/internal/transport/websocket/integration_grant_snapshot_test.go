package websockettransport

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func integrationGrantSnapshotFixture(t *testing.T, recipient string) map[string]any {
	t.Helper()
	// Та же форма, которую CP caster выдаёт после attachConnection.
	response := &cp.ListIntegrationConnectionsResponse{Connections: []*cp.IntegrationConnection{{
		Ref: "con_fixture01", Version: 12, DefinitionKey: "context7", DefinitionVersion: "1.0",
		Name: "Context7", State: cp.ConnectionState_CONNECTION_STATE_CONNECTED,
		Grants: []*cp.IntegrationGrant{{
			Ref: "igr_fixture01", Version: 1, ConnectionVersion: 12,
			AgentRef: recipient, CapabilityKey: "context7.docs.query", TargetName: "Assistant", Enabled: true,
			Risk: "READ", TypedRisk: cp.IntegrationRisk_INTEGRATION_RISK_READ,
			ApprovalPolicy: cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE,
			ResourceScope:  &cp.IntegrationResourceScope{Kind: cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_HTTPS_RESOURCE},
		}},
	}}}
	connections, err := projectSnapshotPart(response, func(value string) string { return value })
	if err != nil {
		t.Fatal("connection snapshot projection failed")
	}
	definitions, err := projectSnapshotPart(&cp.ListIntegrationDefinitionsResponse{}, func(value string) string { return value })
	if err != nil {
		t.Fatal("definition snapshot projection failed")
	}
	return map[string]any{"definitions": definitions, "connections": connections}
}

func TestIntegrationGrantSnapshotAcceptsPublicSystemAndProjectGrant(t *testing.T) {
	for _, recipient := range []string{"agt_systemfixture", "agt_projectfixture"} {
		for _, kind := range []string{"INTEGRATION_CONNECTION", "INTEGRATION_GRANT"} {
			t.Run(recipient+"/"+kind, func(t *testing.T) {
				snapshot, err := typedPlatformSnapshot(kind, integrationGrantSnapshotFixture(t, recipient))
				if err != nil {
					t.Fatalf("public grant snapshot rejected: %v", err)
				}
				if snapshot.Connections == nil || len(snapshot.Connections.Connections) != 1 {
					t.Fatal("typed connection snapshot was lost")
				}
				connection := snapshot.Connections.Connections[0]
				if connection.Version != 12 || len(connection.Grants) != 1 || connection.Grants[0].AgentRef == nil || *connection.Grants[0].AgentRef != recipient || connection.Grants[0].Version != 1 || !connection.Grants[0].Enabled {
					t.Fatal("typed parent pin or recipient identity changed")
				}
			})
		}
	}
}

func TestIntegrationGrantSnapshotStillRejectsUndeclaredNestedFields(t *testing.T) {
	for _, field := range []string{"connectionVersion", "unknownAuthority", "credentialValue"} {
		t.Run(field, func(t *testing.T) {
			value := integrationGrantSnapshotFixture(t, "agt_systemfixture")
			connections := value["connections"].(map[string]any)["connections"].([]any)
			grant := connections[0].(map[string]any)["grants"].([]any)[0].(map[string]any)
			grant[field] = "synthetic-private-marker"
			if _, err := typedPlatformSnapshot("INTEGRATION_CONNECTION", value); err == nil {
				t.Fatal("undeclared grant field escaped strict snapshot decoder")
			}
		})
	}
}

