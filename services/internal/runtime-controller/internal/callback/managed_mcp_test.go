package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func managedMCPCallbackFixture(t *testing.T) runtimecontract.RunnerInput {
	t.Helper()
	input := runtimecontract.RunnerInput{AssistantScope: runtimecontract.AssistantScopeNone,
		AgentRef: "agt_context01", ProjectRef: "prj_context01", RunRef: "run_context01", NodeRef: "nod_context01",
		LeaseRef: "lse_context01", LeaseFence: "fence_context01", LeaseGeneration: 2}
	for index, operation := range []string{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
		field, limit := "library_name", "200"
		if index == 1 {
			field, limit = "library_id", "512"
		}
		schema := `{"type":"object","additionalProperties":false,"properties":{"` + field + `":{"type":"string","minLength":1,"maxLength":` + limit + `},"query":{"type":"string","minLength":1,"maxLength":4096}},"required":["` + field + `","query"]}`
		digest := sha256.Sum256([]byte(schema))
		input.IntegrationGrants = append(input.IntegrationGrants, runtimecontract.RunnerIntegrationGrant{
			Ref: []string{"igr_resolve01", "igr_query001"}[index], GrantVersion: 2,
			ConnectionRef: "icn_context01", ConnectionVersion: 4, ApprovalPolicy: "NONE", DefinitionKey: "context7", ConnectionName: "Context7",
			CapabilityKey: operation, CapabilityName: operation, Operation: operation, Risk: "READ", DefinitionVersion: "1.0.0",
			DefinitionDigest: strings.Repeat("a", 64), InputSchema: schema, InputSchemaSHA256: hex.EncodeToString(digest[:]),
		})
	}
	health := runtimecontract.ManagedMCPHealthProof{TestRef: "ict_health001", Generation: 1, ConnectionRef: "icn_context01", ConnectionVersion: 4,
		ConfigurationSHA256: strings.Repeat("b", 64), CredentialRevisionRef: "icr_credential01", CredentialRevision: 2, CredentialSHA256: strings.Repeat("c", 64),
		DefinitionKey: "context7", DefinitionVersion: "1.0.0", DefinitionDigest: strings.Repeat("a", 64), CheckedAt: time.Now().UTC().Add(-time.Second), Probe: runtimecontract.ManagedMCPHealthProbe}
	profile, err := runtimecontract.DeriveContext7ManagedMCPProfile("AGENT", input.AgentRef, input.IntegrationGrants, health)
	if err != nil {
		t.Fatal(err)
	}
	input.ManagedMCPProfiles = []runtimecontract.ManagedMCPProfile{profile}
	return input
}

func TestManagedMCPCatalogUsesOnlyExactGrantSchemas(t *testing.T) {
	input := managedMCPCallbackFixture(t)
	available := managedMCPTools(input)
	if len(available) != 2 {
		t.Fatal("closed Context7 aliases are missing")
	}
	for index, tool := range available {
		names := []string{runtimecontract.Context7ResolveTool, runtimecontract.Context7QueryTool}
		if tool["name"] != names[index] {
			t.Fatal("managed alias order changed")
		}
		raw, err := json.Marshal(tool["inputSchema"])
		digest, shapeErr := runtimecontract.ManagedMCPSchemaDigest(raw)
		want, wantErr := runtimecontract.ManagedMCPSchemaDigest([]byte(input.IntegrationGrants[index].InputSchema))
		if err != nil || shapeErr != nil || wantErr != nil || digest != want {
			t.Fatal("alias schema differs from its owner-pinned grant")
		}
		for _, name := range []string{"grant_ref", "connection_ref", "base_url", "api_key", "command", "env", "transport"} {
			if strings.Contains(string(raw), `"`+name+`"`) {
				t.Fatal("managed alias exposed transport or authority fields")
			}
		}
	}
	raw, _ := json.Marshal(input.ManagedMCPProfiles)
	for _, name := range []string{"https://", "CONTEXT7_API_KEY", `"base_url"`, `"command"`, `"api_key"`, `"transport"`} {
		if strings.Contains(string(raw), name) {
			t.Fatal("managed profile exposed a raw external MCP configuration")
		}
	}
	full, _ := json.Marshal(tools(input))
	for _, name := range []string{runtimecontract.Context7ResolveTool, runtimecontract.Context7QueryTool} {
		if !strings.Contains(string(full), name) {
			t.Fatal("working tools/list omitted its required managed alias")
		}
	}
}

func TestManagedMCPAliasesResolveExactOwnerConnectionAndProjectSafeActivity(t *testing.T) {
	for index, tool := range []string{runtimecontract.Context7ResolveTool, runtimecontract.Context7QueryTool} {
		t.Run(tool, func(t *testing.T) {
			input := managedMCPCallbackFixture(t)
			field, library := "library_name", "Go MCP SDK"
			if index == 1 {
				field, library = "library_id", "/modelcontextprotocol/go-sdk"
			}
			arguments := map[string]any{field: library, "query": "private-documentation-query"}
			mapped, err := managedMCPArguments(input, tool, arguments)
			if err != nil || mapped["grant_ref"] != input.IntegrationGrants[index].Ref || len(mapped) != 2 {
				t.Fatal("managed alias did not select its exact owner grant")
			}
			client := &integrationResultClient{state: "SUCCEEDED", invocation: "inv_context01"}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			params, _ := json.Marshal(map[string]any{"name": tool, "arguments": arguments})
			writer := httptest.NewRecorder()
			server.callTool(writer, httptest.NewRequest("POST", "/", nil), mcpRequest{ID: json.RawMessage(`"context7-call"`), Params: params}, input)
			var wire struct {
				Result struct {
					IsError           bool           `json:"isError"`
					StructuredContent map[string]any `json:"structuredContent"`
				} `json:"result"`
			}
			if json.Unmarshal(writer.Body.Bytes(), &wire) != nil || wire.Result.IsError || wire.Result.StructuredContent["invocationRef"] != "inv_context01" {
				t.Fatalf("managed alias did not complete through owner invocation: %s", writer.Body.String())
			}
			grant := input.IntegrationGrants[index]
			if len(client.resolves) != 1 || client.resolves[0].ConnectionRef != grant.ConnectionRef || client.resolves[0].CapabilityKey != grant.CapabilityKey || client.resolves[0].BoundedInput.AsMap()[field] != library {
				t.Fatal("alias crossed its exact owner connection or capability")
			}
			projection := client.projection
			if projection == nil || projection.GrantRef != grant.Ref || projection.CapabilityRef != grant.CapabilityKey || projection.Tool != tool || projection.Revision != 2 || projection.State != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED {
				t.Fatal("alias lost its terminal owner activity linkage")
			}
			if len(projection.SafeParameters.AsMap()) != 2 || strings.Contains(projection.SafeResult, "private") || strings.Contains(projection.SafeParameters.String(), "private") {
				t.Fatal("documentation input or provider result leaked into safe activity")
			}
		})
	}
}

func TestManagedMCPRejectsUnboundOrStaleHealthBeforeAnyOwnerCall(t *testing.T) {
	base := managedMCPCallbackFixture(t)
	for name, mutate := range map[string]func(*runtimecontract.RunnerInput){
		"missing profile":     func(i *runtimecontract.RunnerInput) { i.ManagedMCPProfiles = nil },
		"foreign query grant": func(i *runtimecontract.RunnerInput) { i.ManagedMCPProfiles[0].QueryGrantRef = "igr_foreign01" },
		"swapped grant": func(i *runtimecontract.RunnerInput) {
			i.ManagedMCPProfiles[0].ResolveGrantRef, i.ManagedMCPProfiles[0].QueryGrantRef = i.ManagedMCPProfiles[0].QueryGrantRef, i.ManagedMCPProfiles[0].ResolveGrantRef
		},
		"wrong connection":        func(i *runtimecontract.RunnerInput) { i.IntegrationGrants[1].ConnectionRef = "icn_foreign01" },
		"wrong grant version":     func(i *runtimecontract.RunnerInput) { i.IntegrationGrants[0].GrantVersion++ },
		"wrong health credential": func(i *runtimecontract.RunnerInput) { i.ManagedMCPProfiles[0].Health.CredentialRevision++ },
		"wrong health configuration": func(i *runtimecontract.RunnerInput) {
			i.ManagedMCPProfiles[0].Health.ConfigurationSHA256 = strings.Repeat("d", 64)
		},
		"legacy outcome": func(i *runtimecontract.RunnerInput) { i.ManagedMCPProfiles[0].Health.Probe = "LAST_TEST_OUTCOME" },
		"wrong scope":    func(i *runtimecontract.RunnerInput) { i.ManagedMCPProfiles[0].ScopeRef = "agt_foreign01" },
		"stale": func(i *runtimecontract.RunnerInput) {
			i.ManagedMCPProfiles[0].Health.CheckedAt = time.Now().UTC().Add(-6 * time.Minute)
			rederiveManagedMCPCallback(t, i)
		},
		"future": func(i *runtimecontract.RunnerInput) {
			i.ManagedMCPProfiles[0].Health.CheckedAt = time.Now().UTC().Add(time.Minute)
			rederiveManagedMCPCallback(t, i)
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			input.IntegrationGrants = slices.Clone(base.IntegrationGrants)
			input.ManagedMCPProfiles = slices.Clone(base.ManagedMCPProfiles)
			mutate(&input)
			if _, err := managedMCPArguments(input, runtimecontract.Context7ResolveTool, map[string]any{"library_name": "Go", "query": "docs"}); err == nil {
				t.Fatal("unbound or stale managed MCP invocation accepted")
			}
			client := &integrationResultClient{}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			params, _ := json.Marshal(map[string]any{"name": runtimecontract.Context7ResolveTool, "arguments": map[string]any{"library_name": "Go", "query": "private-query"}})
			writer := httptest.NewRecorder()
			server.callTool(writer, httptest.NewRequest("POST", "/", nil), mcpRequest{ID: json.RawMessage(`1`), Params: params}, input)
			if !strings.Contains(writer.Body.String(), `"code":-32603`) || len(client.resolves) != 0 || client.projection != nil || len(client.reads) != 0 || strings.Contains(writer.Body.String(), "private-query") {
				t.Fatalf("invalid managed proof reached owner or disclosed query: %s", writer.Body.String())
			}
		})
	}
	if _, err := managedMCPArguments(base, "context7_arbitrary_tool", map[string]any{}); err == nil {
		t.Fatal("unregistered Context7 tool was accepted")
	}
}

func rederiveManagedMCPCallback(t *testing.T, input *runtimecontract.RunnerInput) {
	t.Helper()
	profile := input.ManagedMCPProfiles[0]
	derived, err := runtimecontract.DeriveContext7ManagedMCPProfile(profile.ScopeKind, profile.ScopeRef, input.IntegrationGrants, profile.Health)
	if err != nil {
		t.Fatal(err)
	}
	input.ManagedMCPProfiles[0] = derived
}

func TestManagedMCPClosedInputGuardPrecedesOwnerMutation(t *testing.T) {
	input := managedMCPCallbackFixture(t)
	for index, tool := range []string{runtimecontract.Context7ResolveTool, runtimecontract.Context7QueryTool} {
		field, otherField, maximum := "library_name", "library_id", 200
		if index == 1 {
			field, otherField, maximum = "library_id", "library_name", 512
		}
		t.Run(tool, func(t *testing.T) {
			boundary := map[string]any{field: strings.Repeat("я", maximum), "query": strings.Repeat("я", 4096)}
			if _, err := managedMCPArguments(input, tool, boundary); err != nil {
				t.Fatal("valid Unicode input at exact limits rejected")
			}
			cases := map[string]map[string]any{
				"missing query":       {field: "Go"},
				"missing library":     {"query": "docs"},
				"wrong library field": {otherField: "Go", "query": "docs"},
				"non-string library":  {field: 7, "query": "docs"},
				"non-string query":    {field: "Go", "query": map[string]any{"text": "docs"}},
				"empty library":       {field: "", "query": "docs"},
				"blank query":         {field: "Go", "query": " \n\t"},
				"large library":       {field: strings.Repeat("я", maximum+1), "query": "docs"},
				"large query":         {field: "Go", "query": strings.Repeat("я", 4097)},
			}
			for _, forbidden := range []string{"grant_ref", "connection_ref", "base_url", "url", "api_key", "command", "env", "transport"} {
				cases[forbidden] = map[string]any{field: "Go", "query": "private-query", forbidden: "private-value"}
			}
			for name, arguments := range cases {
				t.Run(name, func(t *testing.T) {
					if _, err := managedMCPArguments(input, tool, arguments); err == nil {
						t.Fatal("unbounded or caller-owned MCP configuration accepted")
					}
					client := &integrationResultClient{}
					server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
					params, err := json.Marshal(map[string]any{"name": tool, "arguments": arguments})
					if err != nil {
						t.Fatal(err)
					}
					writer := httptest.NewRecorder()
					server.callTool(writer, httptest.NewRequest("POST", "/", nil), mcpRequest{ID: json.RawMessage(`1`), Params: params}, input)
					if !strings.Contains(writer.Body.String(), `"code":-32603`) || len(client.resolves) != 0 || client.projection != nil || len(client.reads) != 0 || strings.Contains(writer.Body.String(), "private-") {
						t.Fatalf("invalid alias input reached owner or disclosed payload: %s", writer.Body.String())
					}
				})
			}
			for _, invalid := range []map[string]any{
				{field: string([]byte{0xff}), "query": "docs"},
				{field: "Go", "query": string([]byte{0xff})},
			} {
				if _, err := managedMCPArguments(input, tool, invalid); err == nil {
					t.Fatal("invalid UTF-8 input accepted")
				}
			}
		})
	}
}
