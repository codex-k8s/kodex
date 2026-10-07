package workload

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestManagedMCPAddCatalogPreservesOwnerPinsAndTypedHealth(t *testing.T) {
	input := runtimecontract.RunnerInput{AgentRef: "agt_agent001", ProjectRef: "prj_project01", AssistantScope: runtimecontract.AssistantScopeNone}
	grants := []runtimecontract.RunnerIntegrationGrant{}
	revision := &cp.RuntimeRevisionSnapshot{}
	for index, capability := range []string{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
		field := "library_name"
		if index == 1 {
			field = "library_id"
		}
		schema := `{"type":"object","additionalProperties":false,"properties":{"` + field + `":{"type":"string"},"query":{"type":"string"}},"required":["` + field + `","query"]}`
		digest := sha256.Sum256([]byte(schema))
		grant := runtimecontract.RunnerIntegrationGrant{Ref: []string{"igr_resolve01", "igr_query001"}[index], GrantVersion: 3, ConnectionRef: "icn_context01", ConnectionVersion: 7, ApprovalPolicy: "NONE",
			DefinitionKey: "context7", DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64), ConnectionName: "Context7", CapabilityKey: capability, CapabilityName: capability, Risk: "READ", Operation: capability, InputSchema: schema, InputSchemaSHA256: hex.EncodeToString(digest[:])}
		grants = append(grants, grant)
		revision.IntegrationGrants = append(revision.IntegrationGrants, &cp.IntegrationGrant{Ref: grant.Ref, Version: grant.GrantVersion, ConnectionRef: grant.ConnectionRef, ConnectionVersion: grant.ConnectionVersion, ApprovalPolicy: cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_NONE,
			DefinitionKey: grant.DefinitionKey, DefinitionVersion: grant.DefinitionVersion, DefinitionDigest: grant.DefinitionDigest, ConnectionName: grant.ConnectionName, CapabilityKey: grant.CapabilityKey, CapabilityName: grant.CapabilityName, Risk: grant.Risk, Operation: grant.Operation, InputSchema: grant.InputSchema, InputSchemaSha256: grant.InputSchemaSHA256, Enabled: true})
	}
	health := runtimecontract.ManagedMCPHealthProof{TestRef: "ict_health001", Generation: 4, ConnectionRef: "icn_context01", ConnectionVersion: 7,
		ConfigurationSHA256: strings.Repeat("b", 64), CredentialRevisionRef: "icr_credential01", CredentialRevision: 2, CredentialSHA256: strings.Repeat("c", 64), DefinitionKey: "context7", DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64), CheckedAt: time.Now().UTC(), Probe: runtimecontract.ManagedMCPHealthProbe}
	profile, err := runtimecontract.DeriveContext7ManagedMCPProfile("AGENT", input.AgentRef, grants, health)
	if err != nil {
		t.Fatal(err)
	}
	revision.ManagedMcpProfiles = []*cp.ManagedMCPProfile{{Provider: profile.Provider, Version: uint32(profile.Version), Namespace: profile.Namespace, Required: profile.Required,
		ScopeKind: cp.ManagedMCPScopeKind_MANAGED_MCP_SCOPE_KIND_AGENT, ScopeRef: profile.ScopeRef, ResolveGrantRef: profile.ResolveGrantRef, QueryGrantRef: profile.QueryGrantRef, Digest: profile.Digest,
		Health: &cp.ManagedMCPHealthProof{TestRef: health.TestRef, Generation: health.Generation, ConnectionRef: health.ConnectionRef, ConnectionVersion: health.ConnectionVersion,
			ConfigurationSha256: health.ConfigurationSHA256, CredentialRevisionRef: health.CredentialRevisionRef, CredentialRevision: health.CredentialRevision, CredentialSha256: health.CredentialSHA256,
			DefinitionKey: health.DefinitionKey, DefinitionVersion: health.DefinitionVersion, DefinitionDigest: health.DefinitionDigest, CheckedAt: timestamppb.New(health.CheckedAt), Probe: health.Probe}}}
	manager := &Manager{}
	got := input
	manager.addCatalog(&got, revision)
	if !slices.Equal(got.IntegrationGrants, grants) || runtimecontract.ValidateManagedMCPProfiles(got) != nil || got.ManagedMCPProfiles[0].Digest != profile.Digest {
		t.Fatal("typed materialization lost owner pins")
	}
	for name, mutate := range map[string]func(*cp.RuntimeRevisionSnapshot){
		"grant version":        func(r *cp.RuntimeRevisionSnapshot) { r.IntegrationGrants[0].Version++ },
		"connection version":   func(r *cp.RuntimeRevisionSnapshot) { r.IntegrationGrants[0].ConnectionVersion++ },
		"unspecified approval": func(r *cp.RuntimeRevisionSnapshot) { r.IntegrationGrants[0].ApprovalPolicy = 0 },
		"foreign scope":        func(r *cp.RuntimeRevisionSnapshot) { r.ManagedMcpProfiles[0].ScopeRef = "agt_foreign01" },
		"unknown scope":        func(r *cp.RuntimeRevisionSnapshot) { r.ManagedMcpProfiles[0].ScopeKind = cp.ManagedMCPScopeKind(999) },
		"missing proof":        func(r *cp.RuntimeRevisionSnapshot) { r.ManagedMcpProfiles[0].Health = nil },
		"missing timestamp":    func(r *cp.RuntimeRevisionSnapshot) { r.ManagedMcpProfiles[0].Health.CheckedAt = nil },
		"invalid timestamp": func(r *cp.RuntimeRevisionSnapshot) {
			r.ManagedMcpProfiles[0].Health.CheckedAt = &timestamppb.Timestamp{Nanos: -1}
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := proto.Clone(revision).(*cp.RuntimeRevisionSnapshot)
			mutate(changed)
			got := input
			manager.addCatalog(&got, changed)
			if runtimecontract.ValidateManagedMCPProfiles(got) == nil {
				t.Fatal("changed owner proof accepted")
			}
		})
	}
}
