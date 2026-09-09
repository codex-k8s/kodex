package workload

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"k8s.io/client-go/kubernetes/fake"
	"os"
	"strings"
	"testing"
)

// Та же полная fixture проверяется owner digest и настоящим CP caster;
// controller не пересчитывает ей expected digest через sealTestTurnExecution.
func TestRuntimeGrantSnapshotBuildTurnInput(t *testing.T) {
	raw, err := os.ReadFile("../../../control-plane/testdata/runtime-snapshot/claim.json")
	if err != nil {
		t.Fatal(err)
	}
	claim := &cp.ClaimedExecution{}
	if err := protojson.Unmarshal(raw, claim); err != nil {
		t.Fatal(err)
	}
	wire, err := proto.Marshal(claim)
	if err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(wire, claim); err != nil {
		t.Fatal(err)
	}
	manager := newTestManager(t, fake.NewSimpleClientset())
	input, _, err := manager.BuildTurnInput(claim)
	if err != nil {
		t.Fatalf("complete owner snapshot rejected: %v", err)
	}
	if len(input.IntegrationGrants) != 1 || input.IntegrationGrants[0].Operation != "SEND" || input.ExecutionBindingDigest == "" || input.MCPBindingDigest == "" {
		t.Fatal("integration or execution binding lost")
	}
	for name, mutate := range map[string]func(*cp.ClaimedExecution){
		"definition-version": func(c *cp.ClaimedExecution) { c.Revision.IntegrationGrants[0].DefinitionVersion = "1.4.2" },
		"definition-digest": func(c *cp.ClaimedExecution) {
			c.Revision.IntegrationGrants[0].DefinitionDigest = strings.Repeat("8", 64)
		},
		"operation": func(c *cp.ClaimedExecution) { c.Revision.IntegrationGrants[0].Operation = "DELETE" },
		"schema":    func(c *cp.ClaimedExecution) { c.Revision.IntegrationGrants[0].InputSchema = "{}" },
		"schema-digest": func(c *cp.ClaimedExecution) {
			c.Revision.IntegrationGrants[0].InputSchemaSha256 = strings.Repeat("8", 64)
		},
		"disabled":           func(c *cp.ClaimedExecution) { c.Revision.IntegrationGrants[0].Enabled = false },
		"foreign-connection": func(c *cp.ClaimedExecution) { c.Revision.IntegrationGrants[0].ConnectionRef = "int_foreign1" },
		"capabilities":       func(c *cp.ClaimedExecution) { c.Revision.Capabilities = c.Revision.Capabilities[:1] },
		"image":              func(c *cp.ClaimedExecution) { c.Revision.RoleImageArtifactRef = "imgart_foreign1" },
		"environment":        func(c *cp.ClaimedExecution) { c.Revision.EnvironmentBindingDigest = strings.Repeat("8", 64) },
		"provider":           func(c *cp.ClaimedExecution) { c.Revision.ProviderCredential.SecretResourceVersion = "2" },
		"revision-digest":    func(c *cp.ClaimedExecution) { c.Revision.RevisionDigest = strings.Repeat("8", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := proto.Clone(claim).(*cp.ClaimedExecution)
			mutate(changed)
			if _, _, err := manager.BuildTurnInput(changed); err == nil {
				t.Fatal("changed immutable pin accepted")
			}
		})
	}
}
