package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestNativeSessionMCPOwnerInputsPreserveSemanticPins keeps native tool-context dependencies
// stable across a fresh identical health observation but changes them with
// credential/configuration semantics, rejecting an unknown profile without remote MCP access.
// TestNativeSessionMCPOwnerInputsPreserveSemanticPins сохраняет зависимости контекста нативных
// инструментов при свежем одинаковом наблюдении здоровья, меняя их при смене семантики credential
// и конфигурации и отклоняя неизвестный профиль без удалённого MCP-доступа.
func TestNativeSessionMCPOwnerInputsPreserveSemanticPins(t *testing.T) {
	grants := []map[string]string{}
	for index, capability := range []string{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
		inputSchema := `{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string"}},"required":["query"]}`
		digest := sha256.Sum256([]byte(inputSchema))
		grants = append(grants, map[string]string{
			"ref": []string{"igr_resolve01", "igr_query001"}[index], "grantVersion": "2", "connectionRef": "icn_context01", "connectionVersion": "4",
			"approvalPolicy": "NONE", "definitionKey": "context7", "connectionName": "Context7", "capabilityKey": capability, "capabilityName": capability,
			"risk": "READ", "definitionVersion": "1.0.0", "definitionDigest": strings.Repeat("a", 64), "operation": capability,
			"inputSchema": inputSchema, "inputSchemaSha256": hex.EncodeToString(digest[:]),
		})
	}
	health := runtimecontract.ManagedMCPHealthProof{TestRef: "ict_health001", Generation: 1, ConnectionRef: "icn_context01", ConnectionVersion: 4,
		ConfigurationSHA256: strings.Repeat("b", 64), CredentialRevisionRef: "icr_credential01", CredentialRevision: 2, CredentialSHA256: strings.Repeat("c", 64),
		DefinitionKey: "context7", DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64), CheckedAt: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), Probe: runtimecontract.ManagedMCPHealthProbe}
	projection := func(health runtimecontract.ManagedMCPHealthProof) runtimecontract.ManagedMCPHealthProof {
		t.Helper()
		profile, err := runtimecontract.DeriveContext7ManagedMCPProfile("SYSTEM", "agt_fixture01", runtimeRevisionGrants(grants), health)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(map[string]any{"agentRef": "agt_fixture01", "projectRef": "", "assistantScope": "SYSTEM", "assistantProfileRef": "", "integrationGrants": grants, "managedMCPProfiles": []runtimecontract.ManagedMCPProfile{profile}})
		if err != nil {
			t.Fatal(err)
		}
		var snapshot map[string]any
		if json.Unmarshal(raw, &snapshot) != nil {
			t.Fatal("synthetic snapshot cannot be decoded")
		}
		result, err := managedMCPInputFromOwnerSnapshot(raw)
		if err != nil {
			t.Fatal(err)
		}
		return result.ManagedMCPProfiles[0].Health
	}
	baseline := projection(health)
	refreshed := health
	refreshed.TestRef, refreshed.Generation, refreshed.CheckedAt = "ict_health002", 2, health.CheckedAt.Add(time.Minute)
	if !sameManagedMCPHealthInputs(baseline, projection(refreshed)) {
		t.Fatal("fresh health observation reset unchanged native tools context")
	}
	for _, change := range []func(*runtimecontract.ManagedMCPHealthProof){
		func(value *runtimecontract.ManagedMCPHealthProof) { value.CredentialRevisionRef = "icr_credential02" },
		func(value *runtimecontract.ManagedMCPHealthProof) { value.CredentialRevision++ },
		func(value *runtimecontract.ManagedMCPHealthProof) { value.CredentialSHA256 = strings.Repeat("d", 64) },
		func(value *runtimecontract.ManagedMCPHealthProof) {
			value.ConfigurationSHA256 = strings.Repeat("e", 64)
		},
	} {
		changed := health
		change(&changed)
		if sameManagedMCPHealthInputs(baseline, projection(changed)) {
			t.Fatal("changed MCP semantic dependency resumed native tools context")
		}
	}
	invalid, _ := json.Marshal(map[string]any{"assistantScope": "SYSTEM", "agentRef": "agt_fixture01", "managedMCPProfiles": []map[string]any{{"provider": "UNKNOWN"}}, "integrationGrants": grants})
	if _, err := managedMCPInputFromOwnerSnapshot(invalid); err == nil {
		t.Fatal("unknown managed MCP profile accepted")
	}
}
