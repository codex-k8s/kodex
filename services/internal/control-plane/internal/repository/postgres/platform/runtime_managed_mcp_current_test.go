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

func managedMCPOwnerSnapshotFixture(t *testing.T) (map[string]any, runtimecontract.ManagedMCPProfile) {
	t.Helper()
	grants := []map[string]string{}
	for i, capability := range []string{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
		schema := `{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string"}},"required":["query"]}`
		digest := sha256.Sum256([]byte(schema))
		grants = append(grants, map[string]string{"ref": []string{"igr_resolve01", "igr_query001"}[i], "grantVersion": "2",
			"connectionRef": "icn_context01", "connectionVersion": "4", "approvalPolicy": "NONE", "definitionKey": "context7",
			"connectionName": "Context7", "capabilityKey": capability, "capabilityName": capability, "risk": "READ",
			"definitionVersion": "1.0.0", "definitionDigest": strings.Repeat("a", 64), "operation": capability,
			"inputSchema": schema, "inputSchemaSha256": hex.EncodeToString(digest[:])})
	}
	health := runtimecontract.ManagedMCPHealthProof{TestRef: "tst_fixture01", Generation: 1, ConnectionRef: "icn_context01", ConnectionVersion: 4,
		ConfigurationSHA256: strings.Repeat("b", 64), CredentialRevisionRef: "icr_fixture01", CredentialRevision: 2,
		CredentialSHA256: strings.Repeat("c", 64), DefinitionKey: "context7", DefinitionVersion: "1.0.0",
		DefinitionDigest: strings.Repeat("a", 64), CheckedAt: time.Now().UTC().Add(-time.Hour), Probe: runtimecontract.ManagedMCPHealthProbe}
	profile, err := runtimecontract.DeriveContext7ManagedMCPProfile("SYSTEM", "agt_fixture01", runtimeRevisionGrants(grants), health)
	if err != nil {
		t.Fatal("derive synthetic owner MCP profile")
	}
	return map[string]any{"agentRef": "agt_fixture01", "assistantScope": "SYSTEM", "projectRef": "", "assistantProfileRef": "",
		"integrationGrants": grants, "managedMCPProfiles": []runtimecontract.ManagedMCPProfile{profile}}, profile
}

func TestManagedMCPOwnerSnapshotKeepsImmutableOldProofAndClosedStructure(t *testing.T) {
	snapshot, profile := managedMCPOwnerSnapshotFixture(t)
	raw, _ := json.Marshal(snapshot)
	input, err := managedMCPInputFromOwnerSnapshot(raw)
	if err != nil || input.ManagedMCPProfiles[0] != profile || runtimecontract.ValidateManagedMCPReadiness(input, time.Now()) == nil {
		t.Fatal("structural read rewrote immutable proof or bypassed startup TTL")
	}
	for name, mutate := range map[string]func(map[string]any){
		"missing profile": func(s map[string]any) { delete(s, "managedMCPProfiles") },
		"unknown provider": func(s map[string]any) {
			p := profile
			p.Provider = "ARBITRARY"
			s["managedMCPProfiles"] = []runtimecontract.ManagedMCPProfile{p}
		},
		"foreign scope":    func(s map[string]any) { s["agentRef"] = "agt_foreign01" },
		"malformed grants": func(s map[string]any) { s["integrationGrants"] = []any{7} },
		"unknown profile field": func(s map[string]any) {
			encoded, _ := json.Marshal(profile)
			var p map[string]any
			_ = json.Unmarshal(encoded, &p)
			p["private_value"] = "synthetic-private-marker"
			s["managedMCPProfiles"] = []map[string]any{p}
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy, _ := managedMCPOwnerSnapshotFixture(t)
			mutate(copy)
			raw, _ := json.Marshal(copy)
			if _, err := managedMCPInputFromOwnerSnapshot(raw); err == nil || strings.Contains(err.Error(), "synthetic-private-marker") {
				t.Fatal("invalid owner projection accepted or leaked private value")
			}
		})
	}
}

func TestManagedMCPFreshObservationDoesNotAdoptDependencyDrift(t *testing.T) {
	_, profile := managedMCPOwnerSnapshotFixture(t)
	a := profile.Health
	b := a
	b.TestRef, b.Generation, b.CheckedAt = "tst_fixture02", 2, time.Now().UTC()
	if !sameManagedMCPHealthInputs(a, b) {
		t.Fatal("new observation changed identical health dependencies")
	}
	for _, mutate := range []func(*runtimecontract.ManagedMCPHealthProof){
		func(h *runtimecontract.ManagedMCPHealthProof) { h.ConnectionVersion++ },
		func(h *runtimecontract.ManagedMCPHealthProof) { h.ConfigurationSHA256 = strings.Repeat("d", 64) },
		func(h *runtimecontract.ManagedMCPHealthProof) { h.CredentialRevision++ },
		func(h *runtimecontract.ManagedMCPHealthProof) { h.CredentialRevisionRef = "icr_foreign01" },
		func(h *runtimecontract.ManagedMCPHealthProof) { h.CredentialSHA256 = strings.Repeat("d", 64) },
		func(h *runtimecontract.ManagedMCPHealthProof) { h.DefinitionVersion = "2.0.0" },
		func(h *runtimecontract.ManagedMCPHealthProof) { h.DefinitionDigest = strings.Repeat("d", 64) },
	} {
		changed := b
		mutate(&changed)
		if sameManagedMCPHealthInputs(a, changed) {
			t.Fatal("new health receipt adopted changed dependency")
		}
	}
}

func TestManagedMCPPendingPairDoesNotCreateHealthAuthority(t *testing.T) {
	snapshot, _ := managedMCPOwnerSnapshotFixture(t)
	grants := runtimeRevisionGrants(snapshot["integrationGrants"].([]map[string]string))
	resolve, query, valid := managedMCPPendingGrantPair(grants)
	if !valid || resolve.CapabilityKey != runtimecontract.Context7ResolveCapability || query.CapabilityKey != runtimecontract.Context7QueryCapability {
		t.Fatal("exact owner pair was not recognized")
	}
	for name, mutate := range map[string]func([]runtimecontract.RunnerIntegrationGrant) []runtimecontract.RunnerIntegrationGrant{
		"missing": func(g []runtimecontract.RunnerIntegrationGrant) []runtimecontract.RunnerIntegrationGrant {
			return g[:1]
		},
		"duplicate": func(g []runtimecontract.RunnerIntegrationGrant) []runtimecontract.RunnerIntegrationGrant {
			return append(g, g[0])
		},
		"foreign connection": func(g []runtimecontract.RunnerIntegrationGrant) []runtimecontract.RunnerIntegrationGrant {
			g[1].ConnectionRef = "icn_foreign01"
			return g
		},
		"wrong version": func(g []runtimecontract.RunnerIntegrationGrant) []runtimecontract.RunnerIntegrationGrant {
			g[1].ConnectionVersion++
			return g
		},
		"unknown operation": func(g []runtimecontract.RunnerIntegrationGrant) []runtimecontract.RunnerIntegrationGrant {
			g[1].Operation = "private-marker"
			return g
		},
		"write": func(g []runtimecontract.RunnerIntegrationGrant) []runtimecontract.RunnerIntegrationGrant {
			g[1].Risk = "WRITE"
			return g
		},
		"approval": func(g []runtimecontract.RunnerIntegrationGrant) []runtimecontract.RunnerIntegrationGrant {
			g[1].ApprovalPolicy = "ALWAYS"
			return g
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := append([]runtimecontract.RunnerIntegrationGrant(nil), grants...)
			if _, _, valid := managedMCPPendingGrantPair(mutate(copy)); valid {
				t.Fatal("unbound pair accepted for readiness wait")
			}
		})
	}
	if runtimeCandidateEligibilityFailure(errManagedMCPHealthPending) {
		t.Fatal("private pending result is a terminal eligibility rejection")
	}
}
