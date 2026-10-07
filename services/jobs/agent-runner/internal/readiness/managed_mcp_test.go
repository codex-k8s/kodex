package readiness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func managedMCPReadinessFixture(t *testing.T) (runtimecontract.RunnerInput, []byte) {
	t.Helper()
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeNone, AgentRef: "agt_agent001", ProjectRef: "prj_project01"}
	for index, capability := range []string{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
		field := "library_name"
		if index == 1 {
			field = "library_id"
		}
		schema := `{"type":"object","additionalProperties":false,"properties":{"` + field + `":{"type":"string"},"query":{"type":"string"}},"required":["` + field + `","query"]}`
		digest := sha256.Sum256([]byte(schema))
		input.IntegrationGrants = append(input.IntegrationGrants, runtimecontract.RunnerIntegrationGrant{Ref: []string{"igr_resolve01", "igr_query001"}[index], GrantVersion: 1, ConnectionRef: "icn_context01", ConnectionVersion: 2, ApprovalPolicy: "NONE",
			DefinitionKey: "context7", DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64), ConnectionName: "Context7", CapabilityKey: capability, CapabilityName: capability, Risk: "READ", Operation: capability, InputSchema: schema, InputSchemaSHA256: hex.EncodeToString(digest[:])})
	}
	health := runtimecontract.ManagedMCPHealthProof{TestRef: "ict_health001", Generation: 1, ConnectionRef: "icn_context01", ConnectionVersion: 2,
		ConfigurationSHA256: strings.Repeat("b", 64), CredentialRevisionRef: "icr_credentials01", CredentialRevision: 1, CredentialSHA256: strings.Repeat("c", 64), DefinitionKey: "context7", DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64), CheckedAt: time.Now().UTC(), Probe: runtimecontract.ManagedMCPHealthProbe}
	profile, err := runtimecontract.DeriveContext7ManagedMCPProfile("AGENT", input.AgentRef, input.IntegrationGrants, health)
	if err != nil {
		t.Fatal(err)
	}
	input.ManagedMCPProfiles = []runtimecontract.ManagedMCPProfile{profile}
	schemas, err := runtimecontract.ManagedMCPToolSchemas(input)
	if err != nil {
		t.Fatal(err)
	}
	tools := []map[string]any{}
	for _, name := range runtimecontract.RuntimeMCPToolNames(input) {
		schema := json.RawMessage(`{"type":"object","additionalProperties":false}`)
		if exact, ok := schemas[name]; ok {
			schema = json.RawMessage(exact)
		}
		tools = append(tools, map[string]any{"name": name, "description": "Synthetic closed tool", "inputSchema": schema})
	}
	catalog, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "agent-runner-tools", "result": map[string]any{"tools": tools}})
	if err != nil {
		t.Fatal(err)
	}
	return input, catalog
}

func TestManagedMCPRequiredCatalogSchemaAndHealth(t *testing.T) {
	input, catalog := managedMCPReadinessFixture(t)
	names := runtimecontract.RuntimeMCPToolNames(input)
	if err := checkCatalogFixture(t, catalog, names, input); err != nil {
		t.Fatal(err)
	}
	for name, changed := range map[string][]byte{
		"schema widened": []byte(strings.ReplaceAll(string(catalog), `"additionalProperties":false`, `"additionalProperties":true`)),
		"duplicate key":  []byte(strings.ReplaceAll(string(catalog), `"library_name":{"type":"string"}`, `"library_name":{"type":"string","type":"string"}`)),
		"missing alias":  []byte(strings.ReplaceAll(string(catalog), runtimecontract.Context7ResolveTool, "foreign_resolver")),
	} {
		t.Run(name, func(t *testing.T) {
			if checkCatalogFixture(t, changed, names, input) == nil {
				t.Fatal("unbound required MCP catalog accepted")
			}
		})
	}
	if checkCatalogFixture(t, catalog, names[:len(names)-1], input) == nil {
		t.Fatal("required alias removed from consumer profile")
	}
	for _, age := range []time.Duration{6 * time.Minute, -time.Minute} {
		changed := input
		health := input.ManagedMCPProfiles[0].Health
		health.CheckedAt = time.Now().UTC().Add(-age)
		profile, err := runtimecontract.DeriveContext7ManagedMCPProfile("AGENT", input.AgentRef, input.IntegrationGrants, health)
		if err != nil {
			t.Fatal(err)
		}
		changed.ManagedMCPProfiles = []runtimecontract.ManagedMCPProfile{profile}
		if checkCatalogFixture(t, catalog, names, changed) == nil {
			t.Fatal("stale or future health proof accepted")
		}
	}
}

func TestManagedMCPMissingOwnerProofFailsBeforeNetworkOrSocket(t *testing.T) {
	input, _ := managedMCPReadinessFixture(t)
	input.ManagedMCPProfiles = nil
	if _, err := StartMCPProxy(t.Context(), input, "synthetic", []string{"propose_run_metadata"}); err == nil || FailureStage(err) != "CONFIGURATION" {
		t.Fatal("missing owner proof reached startup")
	}
}
