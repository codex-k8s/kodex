package runtimecontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

func managedMCPFixture(t *testing.T) RunnerInput {
	t.Helper()
	input := validRunnerInputFixture()
	input.AssistantScope = AssistantScopeNone
	input.Capabilities = []string{Context7ResolveCapability, Context7QueryCapability}
	for index, capability := range input.Capabilities {
		field := "library_name"
		if index == 1 {
			field = "library_id"
		}
		raw := `{"type":"object","additionalProperties":false,"properties":{"` + field + `":{"type":"string"},"query":{"type":"string"}},"required":["` + field + `","query"]}`
		digest := sha256.Sum256([]byte(raw))
		input.IntegrationGrants = append(input.IntegrationGrants, RunnerIntegrationGrant{Ref: []string{"igr_resolve01", "igr_query001"}[index], GrantVersion: 2,
			ConnectionRef: "icn_context01", ConnectionVersion: 4, ApprovalPolicy: "NONE", DefinitionKey: "context7", ConnectionName: "Context7",
			CapabilityKey: capability, CapabilityName: capability, Risk: "READ", DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64),
			Operation: capability, InputSchema: raw, InputSchemaSHA256: hex.EncodeToString(digest[:])})
	}
	health := ManagedMCPHealthProof{TestRef: "ict_health001", Generation: 1, ConnectionRef: "icn_context01", ConnectionVersion: 4,
		ConfigurationSHA256: strings.Repeat("b", 64), CredentialRevisionRef: "icr_credential01", CredentialRevision: 2, CredentialSHA256: strings.Repeat("c", 64),
		DefinitionKey: "context7", DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64), CheckedAt: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC), Probe: ManagedMCPHealthProbe}
	profile, err := DeriveContext7ManagedMCPProfile("AGENT", input.AgentRef, input.IntegrationGrants, health)
	if err != nil {
		t.Fatal(err)
	}
	input.ManagedMCPProfiles = []ManagedMCPProfile{profile}
	refreshRunnerInputBindings(&input)
	return input
}

func TestManagedMCPExactPinsScopeAndReadiness(t *testing.T) {
	base := managedMCPFixture(t)
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"AGENT", "SYSTEM", "PROJECT"} {
		input := base
		input.ManagedMCPProfiles = slices.Clone(base.ManagedMCPProfiles)
		profile := &input.ManagedMCPProfiles[0]
		profile.ScopeKind = scope
		switch scope {
		case "SYSTEM":
			input.AssistantScope = AssistantScopeSystem
			profile.ScopeRef = input.AgentRef
		case "PROJECT":
			input.AssistantScope = AssistantScopeProject
			input.AssistantProfileRef = "asp_profile001"
			profile.ScopeRef = input.AssistantProfileRef
		}
		profile.Digest, _ = managedMCPDigest(*profile, input.IntegrationGrants[0], input.IntegrationGrants[1])
		if err := ValidateManagedMCPProfiles(input); err != nil {
			t.Fatal(scope, err)
		}
		for _, age := range []time.Duration{0, ManagedMCPHealthMaximumAge} {
			if err := ValidateManagedMCPReadiness(input, profile.Health.CheckedAt.Add(age)); err != nil {
				t.Fatal(err)
			}
		}
		for _, age := range []time.Duration{-time.Nanosecond, ManagedMCPHealthMaximumAge + time.Nanosecond} {
			if ValidateManagedMCPReadiness(input, profile.Health.CheckedAt.Add(age)) == nil {
				t.Fatal("stale or future health accepted")
			}
		}
	}
	for name, mutate := range map[string]func(*RunnerInput){
		"grant version":      func(i *RunnerInput) { i.IntegrationGrants[0].GrantVersion++ },
		"connection version": func(i *RunnerInput) { i.IntegrationGrants[0].ConnectionVersion++ },
		"grant zero":         func(i *RunnerInput) { i.IntegrationGrants[0].GrantVersion = 0 },
		"connection zero":    func(i *RunnerInput) { i.IntegrationGrants[0].ConnectionVersion = 0 },
		"approval":           func(i *RunnerInput) { i.IntegrationGrants[0].ApprovalPolicy = "HUMAN_EACH_EFFECT" },
		"unknown approval":   func(i *RunnerInput) { i.IntegrationGrants[0].ApprovalPolicy = "AUTO" },
		"health config":      func(i *RunnerInput) { i.ManagedMCPProfiles[0].Health.ConfigurationSHA256 = strings.Repeat("d", 64) },
		"health credential":  func(i *RunnerInput) { i.ManagedMCPProfiles[0].Health.CredentialRevision++ },
		"health package":     func(i *RunnerInput) { i.ManagedMCPProfiles[0].Health.DefinitionKey = "https-json" },
		"health probe":       func(i *RunnerInput) { i.ManagedMCPProfiles[0].Health.Probe = "LAST_TEST_OUTCOME" },
		"scope":              func(i *RunnerInput) { i.ManagedMCPProfiles[0].ScopeRef = "agt_foreign01" },
		"required":           func(i *RunnerInput) { i.ManagedMCPProfiles[0].Required = false },
		"namespace":          func(i *RunnerInput) { i.ManagedMCPProfiles[0].Namespace = "external" },
		"profile missing":    func(i *RunnerInput) { i.ManagedMCPProfiles = nil },
		"profile duplicate":  func(i *RunnerInput) { i.ManagedMCPProfiles = append(i.ManagedMCPProfiles, i.ManagedMCPProfiles[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			input.IntegrationGrants = slices.Clone(base.IntegrationGrants)
			input.ManagedMCPProfiles = slices.Clone(base.ManagedMCPProfiles)
			mutate(&input)
			if ValidateManagedMCPProfiles(input) == nil {
				t.Fatal("unbound profile accepted")
			}
		})
	}
}

func TestManagedMCPDerivationRejectsMissingAmbiguousAndMixedPairs(t *testing.T) {
	input := managedMCPFixture(t)
	for _, mode := range []string{"missing", "duplicate", "multiple", "mixed", "package"} {
		t.Run(mode, func(t *testing.T) {
			grants := slices.Clone(input.IntegrationGrants)
			switch mode {
			case "missing":
				grants = grants[:1]
			case "duplicate":
				grants = append(grants, grants[0])
			case "multiple":
				extra := grants[0]
				extra.Ref = "igr_foreign01"
				extra.ConnectionRef = "icn_foreign01"
				grants = append(grants, extra)
			case "mixed":
				grants[1].ConnectionRef = "icn_foreign01"
			case "package":
				grants[1].DefinitionVersion = "2.0.0"
			}
			if _, err := DeriveContext7ManagedMCPProfile("AGENT", input.AgentRef, grants, input.ManagedMCPProfiles[0].Health); err == nil {
				t.Fatal("ambiguous owner pair selected")
			}
		})
	}
	grants := []RunnerIntegrationGrant{input.IntegrationGrants[1], input.IntegrationGrants[0]}
	profile, err := DeriveContext7ManagedMCPProfile("AGENT", input.AgentRef, grants, input.ManagedMCPProfiles[0].Health)
	if err != nil || profile.Digest != input.ManagedMCPProfiles[0].Digest {
		t.Fatal("profile depends on grant ordering")
	}
}

func TestManagedMCPCatalogAndCanonicalSchema(t *testing.T) {
	input := managedMCPFixture(t)
	names := RuntimeMCPToolNames(input)
	for _, name := range []string{Context7ResolveTool, Context7QueryTool} {
		if !slices.Contains(names, name) {
			t.Fatal("closed Context7 tool missing")
		}
	}
	schemas, err := ManagedMCPToolSchemas(input)
	if err != nil || len(schemas) != 2 {
		t.Fatal("closed Context7 schema catalog invalid")
	}
	one := []byte(`{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string"}}}`)
	two := []byte(`{ "properties": {"query":{"type":"string"}}, "additionalProperties":false, "type":"object" }`)
	a, err := ManagedMCPSchemaDigest(one)
	b, other := ManagedMCPSchemaDigest(two)
	if err != nil || other != nil || a != b {
		t.Fatal("schema key ordering changed digest")
	}
	for _, invalid := range []string{`{"type":"object","type":"object","additionalProperties":false}`, `{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string","type":"string"}}}`, `{"type":"object"}`, `[]`, string(one) + ` {}`} {
		if _, err := ManagedMCPSchemaDigest([]byte(invalid)); err == nil {
			t.Fatal("ambiguous or unbounded schema accepted")
		}
	}
	raw, err := EncodeRunnerInput(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRunnerInput(raw)
	if err != nil || decoded.ManagedMCPProfiles[0].Digest != input.ManagedMCPProfiles[0].Digest {
		t.Fatal("profile lost during wire roundtrip")
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		t.Fatal("wire invalid")
	}
	if strings.Contains(string(raw), "CONTEXT7_API_KEY") || strings.Contains(string(raw), "https://mcp.context7.com") {
		t.Fatal("raw provider configuration leaked")
	}
}
