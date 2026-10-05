package callback

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type assistantFreshCatalogMCPClient struct {
	*assistantDefinitionCatalogClient
	projection *controlplanev1.RecordRunToolCallRequest
}

func (client *assistantFreshCatalogMCPClient) RecordRunToolCall(_ context.Context, request *controlplanev1.RecordRunToolCallRequest, _ ...grpc.CallOption) (*controlplanev1.RecordRunToolCallResponse, error) {
	client.projection = request
	return &controlplanev1.RecordRunToolCallResponse{Event: &controlplanev1.RunEvent{Ref: "event_synthetic123"}}, nil
}

func TestAssistantFreshFutureReasoningTraversesMCPAndSafeProjection(t *testing.T) {
	input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, "MODELS")
	entry := response.AssistantConfigurationCatalog.Entries[0]
	entry.Ref, entry.Name, entry.Model = "future-model", "future-model", "future-model"
	entry.ReasoningEfforts, entry.DefaultReasoningEffort = []string{"adaptive"}, "adaptive"
	client := &assistantFreshCatalogMCPClient{assistantDefinitionCatalogClient: &assistantDefinitionCatalogClient{response: response}}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	params, err := json.Marshal(map[string]any{"name": "get_configuration_catalog", "arguments": arguments})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.callTool(recorder, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"catalog-future"`), Params: params}, input)
	var wire struct {
		Result struct {
			IsError           bool           `json:"isError"`
			StructuredContent map[string]any `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &wire); err != nil || wire.Result.IsError {
		t.Fatal("future model catalog did not survive native MCP result boundary")
	}
	entries := wire.Result.StructuredContent["assistant_configuration_catalog"].(map[string]any)["entries"].([]any)
	if entries[0].(map[string]any)["default_reasoning_effort"] != "adaptive" || client.request == nil || client.request.GetLeaseRef() != input.LeaseRef || client.request.GetFence() != input.LeaseFence || client.request.GetGeneration() != input.LeaseGeneration {
		t.Fatal("MCP discovery lost fresh model reasoning or exact read authority")
	}
	projection := client.projection
	if projection == nil || projection.GetLeaseRef() != input.LeaseRef || projection.GetFence() != input.LeaseFence || projection.GetGeneration() != input.LeaseGeneration || len(projection.GetSafeParameters().AsMap()) != 1 || projection.GetSafeParameters().AsMap()["catalogKind"] != "MODELS" || projection.GetCapabilityRef() != "platform.configuration.read" || projection.GetState() != controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED {
		t.Fatal("MCP activity did not preserve exact lease and credential-free metadata")
	}
}

func TestAssistantFreshCatalogToolListStaysCompactForRealSystemInput(t *testing.T) {
	input := assistantConfigurationFixture(runtimecontract.AssistantScopeSystem)
	input.AssistantContext = nil
	input.RuntimeEnvironmentRef = "renv_current123"
	encoded, err := json.Marshal(tools(input))
	if err != nil || len(encoded) > 8000 {
		t.Fatalf("full system assistant catalog exceeds compact budget: bytes=%d err=%v", len(encoded), err)
	}
}

func assistantFreshCatalogFixture(scope runtimecontract.AssistantScope, kind string) (runtimecontract.RunnerInput, map[string]any, *controlplanev1.SearchAssistantResourcesResponse) {
	input := assistantConfigurationFixture(scope)
	input.OrganizationRef = "org_owned123"
	response := &controlplanev1.AssistantConfigurationCatalogResponse{
		Kind:         controlplanev1.AssistantConfigurationCatalogKind(controlplanev1.AssistantConfigurationCatalogKind_value["ASSISTANT_CONFIGURATION_CATALOG_KIND_"+kind]),
		AssistantRef: input.AgentRef, ScopeKind: "ORGANIZATION", OrganizationRef: input.OrganizationRef,
	}
	if scope == runtimecontract.AssistantScopeProject {
		input.AssistantProfileRef = "asstprof_owned123"
		response.ScopeKind, response.ProjectRef, response.AssistantProfileRef = "PROJECT", input.ProjectRef, input.AssistantProfileRef
	}
	entry := &controlplanev1.AssistantConfigurationCatalogEntry{Name: "Fixture", ScopeKind: response.ScopeKind, OrganizationRef: response.OrganizationRef,
		ProjectRef: response.ProjectRef, AssistantProfileRef: response.AssistantProfileRef}
	selector := map[string]any{"kind": kind, "assistant_ref": input.AgentRef, "query": " fixture ", "offset": float64(0)}
	switch kind {
	case "ASSISTANTS":
		entry.Ref, entry.Version = input.AgentRef, 1
		entry.RuntimeEnvironmentRef = "renv_current123"
	case "RUNTIME_PROFILES":
		entry.Ref, entry.Version, entry.Provider, entry.Model = "builtin-safe-runtime", 2, "openai", input.Model
	case "PROVIDER_ACCOUNTS":
		entry.Ref, entry.Version, entry.Provider = input.ProviderAccountRef, 3, "openai"
		selector["runtime_profile_ref"] = input.RuntimeProfileRef
	case "MODELS":
		entry.Ref, entry.Name, entry.Model, entry.Provider = input.Model, input.Model, input.Model, "openai"
		entry.CatalogRevision, entry.CatalogDigest = "revision-3", strings.Repeat("a", 64)
		entry.ReasoningEfforts, entry.DefaultReasoningEffort = []string{"low", "medium", "high"}, "medium"
		selector["account_ref"], selector["runtime_profile_ref"] = input.ProviderAccountRef, input.RuntimeProfileRef
	case "ROLE_IMAGE_RECIPES":
		entry.Ref, entry.Version, entry.RecipeGeneration = "imgrec_owned123", 4, 2
	case "IMAGE_ARTIFACTS":
		entry.Ref, entry.Version, entry.RecipeGeneration = "imgart_owned123", 5, 2
		entry.ManifestDigest = "sha256:" + strings.Repeat("b", 64)
		entry.Reference = "pull.fixture.invalid/assistant@" + entry.ManifestDigest
		entry.AdmissionVerdict, entry.PromotionState = "ACCEPTED", "PROMOTED"
		entry.VerifiedToolInventory = &controlplanev1.ImageToolInventory{Status: "UNAVAILABLE"}
	case "ROLE_ENVIRONMENTS":
		entry.Ref = "base"
	}
	response.Entries = []*controlplanev1.AssistantConfigurationCatalogEntry{entry}
	return input, map[string]any{"assistant_configuration_catalog": selector}, &controlplanev1.SearchAssistantResourcesResponse{AssistantConfigurationCatalog: response}
}

func TestAssistantFreshConfigurationCatalogTraversesExactFencedRPC(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		for _, kind := range assistantConfigurationCatalogKinds {
			if kind == "CURRENT_CONFIGURATION" {
				continue // Собственный полный read проверяется отдельным typed сценарием.
			}
			t.Run(string(scope)+"/"+kind, func(t *testing.T) {
				input, arguments, response := assistantFreshCatalogFixture(scope, kind)
				client := &assistantDefinitionCatalogClient{response: response}
				server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
				result, err := server.configurationCatalog(t.Context(), input, arguments)
				if err != nil {
					t.Fatal(err)
				}
				request := client.request
				if request == nil || request.GetLeaseRef() != input.LeaseRef || request.GetFence() != input.LeaseFence || request.GetGeneration() != input.LeaseGeneration ||
					request.GetQuery() != "" || request.GetIntegrationDefinitionCatalog() || request.GetDefinitionQuery() != "" || request.GetAssistantConfigurationCatalog().GetAssistantRef() != input.AgentRef ||
					request.GetAssistantConfigurationCatalog().GetQuery() != "fixture" || request.GetAssistantConfigurationCatalog().GetKind() != response.GetAssistantConfigurationCatalog().GetKind() {
					t.Fatal("fresh configuration discovery lost canonical selector or lease authority")
				}
				catalog := result.(map[string]any)["assistant_configuration_catalog"].(map[string]any)
				entries := catalog["entries"].([]map[string]any)
				fieldCount := 16
				if kind == "IMAGE_ARTIFACTS" {
					fieldCount += 3
				}
				if kind == "ASSISTANTS" {
					fieldCount++
					if entries[0]["runtime_environment_ref"] != "renv_current123" {
						t.Fatal("catalog lost current bound environment locator")
					}
				}
				if catalog["kind"] != kind || len(entries) != 1 || len(entries[0]) != fieldCount || entries[0]["ref"] != response.AssistantConfigurationCatalog.Entries[0].Ref {
					t.Fatal("catalog lost closed safe entry projection")
				}
				parameters, permission, _, ok := safeToolCallParameters(input, "get_configuration_catalog", arguments)
				if !ok || permission != "platform.configuration.read" || len(parameters) != 1 || parameters["catalogKind"] != kind {
					t.Fatal("catalog request metadata escaped safe tool projection")
				}
			})
		}
	}
}

func TestAssistantFreshModelCatalogAcceptsCanonicalFutureReasoningThroughFencedRPC(t *testing.T) {
	for _, scope := range []runtimecontract.AssistantScope{runtimecontract.AssistantScopeSystem, runtimecontract.AssistantScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			input, arguments, response := assistantFreshCatalogFixture(scope, "MODELS")
			entry := response.AssistantConfigurationCatalog.Entries[0]
			entry.Ref, entry.Name, entry.Model = "future-model", "future-model", "future-model"
			entry.ReasoningEfforts, entry.DefaultReasoningEffort = []string{"adaptive", "future_2"}, "adaptive"
			client := &assistantDefinitionCatalogClient{response: response}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			result, err := server.configurationCatalog(t.Context(), input, arguments)
			if err != nil {
				t.Fatal(err)
			}
			if client.request.GetLeaseRef() != input.LeaseRef || client.request.GetFence() != input.LeaseFence || client.request.GetGeneration() != input.LeaseGeneration || client.request.GetAssistantConfigurationCatalog().GetAccountRef() != input.ProviderAccountRef {
				t.Fatal("future model discovery bypassed fenced provider-account authority")
			}
			entries := result.(map[string]any)["assistant_configuration_catalog"].(map[string]any)["entries"].([]map[string]any)
			if entries[0]["model"] != "future-model" || entries[0]["default_reasoning_effort"] != "adaptive" {
				t.Fatal("catalog-defined future reasoning was lost")
			}
		})
	}
}

func TestAssistantFreshModelCatalogReasoningIsStructurallyClosedAndBounded(t *testing.T) {
	efforts := make([]string, 16)
	for index := range efforts {
		efforts[index] = "adaptive_" + strconv.Itoa(index)
	}
	for _, fixture := range []struct {
		name, defaultEffort string
		efforts             []string
		valid               bool
	}{
		{"maximum canonical efforts", efforts[0], efforts, true},
		{"canonical maximum length", strings.Repeat("a", 64), []string{strings.Repeat("a", 64)}, true},
		{"unsupported model", "", []string{}, true},
		{"too many efforts", efforts[0], append(append([]string{}, efforts...), "adaptive_16"), false},
		{"uppercase", "Adaptive", []string{"Adaptive"}, false},
		{"invalid separator", "adaptive/slower", []string{"adaptive/slower"}, false},
		{"too long", strings.Repeat("a", 65), []string{strings.Repeat("a", 65)}, false},
		{"empty entry", "", []string{""}, false},
		{"duplicate", "adaptive", []string{"adaptive", "adaptive"}, false},
		{"missing default", "other", []string{"adaptive"}, false},
		{"empty supported default", "", []string{"adaptive"}, false},
		{"default on unsupported", "adaptive", []string{}, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, "MODELS")
			entry := response.AssistantConfigurationCatalog.Entries[0]
			entry.ReasoningEfforts, entry.DefaultReasoningEffort = fixture.efforts, fixture.defaultEffort
			client := &assistantDefinitionCatalogClient{response: response}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			_, err := server.configurationCatalog(t.Context(), input, arguments)
			if (err == nil) != fixture.valid || client.request == nil {
				t.Fatal("fresh catalog reasoning differs from closed canonical model catalog contract")
			}
		})
	}
}

func TestAssistantFreshCatalogEnvironmentLocatorIsOnlyAssistantMetadata(t *testing.T) {
	for _, kind := range assistantConfigurationCatalogKinds {
		t.Run(kind, func(t *testing.T) {
			input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, kind)
			response.AssistantConfigurationCatalog.Entries[0].RuntimeEnvironmentRef = "renv_current123"
			client := &assistantDefinitionCatalogClient{response: response}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			_, err := server.configurationCatalog(t.Context(), input, arguments)
			if (err == nil) != (kind == "ASSISTANTS") {
				t.Fatal("environment locator escaped per-kind closed metadata boundary")
			}
		})
	}
	for _, ref := range []string{"", "bad", "renv_bad/path", strings.Repeat("a", 97)} {
		input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, "ASSISTANTS")
		response.AssistantConfigurationCatalog.Entries[0].RuntimeEnvironmentRef = ref
		client := &assistantDefinitionCatalogClient{response: response}
		server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
		if _, err := server.configurationCatalog(t.Context(), input, arguments); (err == nil) != (ref == "") {
			t.Fatal("unbound or malformed assistant environment locator was handled incorrectly")
		}
	}
}

func TestAssistantFreshConfigurationCatalogRejectsMalformedSelectorsBeforeRPC(t *testing.T) {
	for _, mutate := range []struct {
		name  string
		apply func(runtimecontract.RunnerInput, map[string]any) (runtimecontract.RunnerInput, map[string]any)
	}{
		{"mixed definition", func(i runtimecontract.RunnerInput, a map[string]any) (runtimecontract.RunnerInput, map[string]any) {
			a["definition_query"] = ""
			return i, a
		}},
		{"mixed agent", func(i runtimecontract.RunnerInput, a map[string]any) (runtimecontract.RunnerInput, map[string]any) {
			a["agent_offset"] = float64(0)
			return i, a
		}},
		{"nonempty schemas", func(i runtimecontract.RunnerInput, a map[string]any) (runtimecontract.RunnerInput, map[string]any) {
			a["operation_types"] = []any{prepareAssistantConfigurationOperation}
			return i, a
		}},
		{"unknown selector", func(i runtimecontract.RunnerInput, a map[string]any) (runtimecontract.RunnerInput, map[string]any) {
			a["assistant_configuration_catalog"].(map[string]any)["owner_ref"] = "untrusted"
			return i, a
		}},
		{"unknown kind", func(i runtimecontract.RunnerInput, a map[string]any) (runtimecontract.RunnerInput, map[string]any) {
			a["assistant_configuration_catalog"].(map[string]any)["kind"] = "CREDENTIALS"
			return i, a
		}},
		{"fractional offset", func(i runtimecontract.RunnerInput, a map[string]any) (runtimecontract.RunnerInput, map[string]any) {
			a["assistant_configuration_catalog"].(map[string]any)["offset"] = 1.5
			return i, a
		}},
		{"oversized offset", func(i runtimecontract.RunnerInput, a map[string]any) (runtimecontract.RunnerInput, map[string]any) {
			a["assistant_configuration_catalog"].(map[string]any)["offset"] = float64(10001)
			return i, a
		}},
		{"missing account", func(i runtimecontract.RunnerInput, a map[string]any) (runtimecontract.RunnerInput, map[string]any) {
			delete(a["assistant_configuration_catalog"].(map[string]any), "account_ref")
			return i, a
		}},
		{"missing fence", func(i runtimecontract.RunnerInput, a map[string]any) (runtimecontract.RunnerInput, map[string]any) {
			i.LeaseFence = ""
			return i, a
		}},
		{"foreign project helper", func(i runtimecontract.RunnerInput, a map[string]any) (runtimecontract.RunnerInput, map[string]any) {
			i.AssistantScope = runtimecontract.AssistantScopeProject
			a["assistant_configuration_catalog"].(map[string]any)["assistant_ref"] = "agt_foreign123"
			return i, a
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, "MODELS")
			input, arguments = mutate.apply(input, arguments)
			client := &assistantDefinitionCatalogClient{response: response}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			if _, err := server.configurationCatalog(t.Context(), input, arguments); err == nil || client.request != nil {
				t.Fatal("malformed selector reached owner RPC")
			}
		})
	}
}

func TestAssistantFreshConfigurationCatalogRejectsResponseBoundaryMismatch(t *testing.T) {
	for _, mutate := range []struct {
		name  string
		apply func(*controlplanev1.SearchAssistantResourcesResponse)
	}{
		{"foreign organization", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantConfigurationCatalog.OrganizationRef = "org_foreign123"
		}},
		{"foreign target", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantConfigurationCatalog.AssistantRef = "agt_foreign123"
		}},
		{"unknown scope", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantConfigurationCatalog.ScopeKind = "PLATFORM"
		}},
		{"wrong kind", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantConfigurationCatalog.Kind = controlplanev1.AssistantConfigurationCatalogKind_ASSISTANT_CONFIGURATION_CATALOG_KIND_ASSISTANTS
		}},
		{"foreign entry", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantConfigurationCatalog.Entries[0].OrganizationRef = "org_foreign123"
		}},
		{"mixed search", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.Results = []*controlplanev1.SearchResult{{Ref: "prj_foreign123"}}
		}},
		{"mixed definition", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.Definitions = []*controlplanev1.AssistantIntegrationDefinition{{Key: "fixture"}}
		}},
		{"unrecognized wire field", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantConfigurationCatalog.Entries[0].ProtoReflect().SetUnknown([]byte{0x80, 0x01, 0x01})
		}},
		{"scope mismatch entry", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantConfigurationCatalog.Entries[0].ProjectRef = "prj_foreign123"
		}},
		{"duplicate entry", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantConfigurationCatalog.Entries = append(r.AssistantConfigurationCatalog.Entries, proto.Clone(r.AssistantConfigurationCatalog.Entries[0]).(*controlplanev1.AssistantConfigurationCatalogEntry))
		}},
		{"missing catalog pin", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantConfigurationCatalog.Entries[0].CatalogDigest = ""
		}},
		{"wrong default effort", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantConfigurationCatalog.Entries[0].DefaultReasoningEffort = "ultra"
		}},
		{"irrelevant image reference", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			r.AssistantConfigurationCatalog.Entries[0].Reference = "pull.fixture.invalid/irrelevant"
		}},
		{"oversized page", func(r *controlplanev1.SearchAssistantResourcesResponse) {
			for len(r.AssistantConfigurationCatalog.Entries) < 11 {
				r.AssistantConfigurationCatalog.Entries = append(r.AssistantConfigurationCatalog.Entries, proto.Clone(r.AssistantConfigurationCatalog.Entries[0]).(*controlplanev1.AssistantConfigurationCatalogEntry))
			}
		}},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, "MODELS")
			mutate.apply(response)
			client := &assistantDefinitionCatalogClient{response: response}
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
			if _, err := server.configurationCatalog(t.Context(), input, arguments); err == nil {
				t.Fatal("invalid catalog response crossed consumer boundary")
			}
		})
	}
}

func TestAssistantFreshConfigurationCatalogChecksEveryEntryKindAndPagination(t *testing.T) {
	for _, kind := range assistantConfigurationCatalogKinds {
		if kind == "CURRENT_CONFIGURATION" {
			continue // Полный read не имеет entries/pagination.
		}
		t.Run(kind, func(t *testing.T) {
			input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeProject, kind)
			request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
			if err != nil {
				t.Fatal(err)
			}
			entry := response.AssistantConfigurationCatalog.Entries[0]
			switch kind {
			case "ASSISTANTS", "RUNTIME_PROFILES", "PROVIDER_ACCOUNTS", "ROLE_IMAGE_RECIPES":
				entry.Version = 0
			case "MODELS":
				entry.Model = "wrong-model"
			case "IMAGE_ARTIFACTS":
				entry.Reference = "pull.fixture.invalid/assistant@sha256:" + strings.Repeat("c", 64)
			case "ROLE_ENVIRONMENTS":
				entry.Ref = "../untrusted"
			}
			if _, err := castAssistantConfigurationCatalog(input, request, response.AssistantConfigurationCatalog); err == nil {
				t.Fatal("missing or detached kind-specific metadata accepted")
			}
		})
	}
	input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, "MODELS")
	arguments["operation_types"] = []any{}
	request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	page := response.AssistantConfigurationCatalog
	for len(page.Entries) < 10 {
		entry := proto.Clone(page.Entries[0]).(*controlplanev1.AssistantConfigurationCatalogEntry)
		entry.Ref = "gpt-fixture-" + strconv.Itoa(len(page.Entries))
		entry.Name, entry.Model = entry.Ref, entry.Ref
		page.Entries = append(page.Entries, entry)
	}
	page.NextOffset = 10
	if _, err := castAssistantConfigurationCatalog(input, request, page); err != nil {
		t.Fatal(err)
	}
	request.Offset = 10
	if _, err := castAssistantConfigurationCatalog(input, request, page); err == nil {
		t.Fatal("non-advancing pagination accepted")
	}
	page.NextOffset = 10001
	if _, err := castAssistantConfigurationCatalog(input, request, page); err == nil {
		t.Fatal("unbounded pagination accepted")
	}
}

func TestAssistantCatalogRejectsMixedResponseArmsForOtherQueries(t *testing.T) {
	input, _, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, "MODELS")
	client := &assistantDefinitionCatalogClient{response: response}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	if _, err := server.configurationCatalog(t.Context(), input, map[string]any{"definition_query": ""}); err == nil {
		t.Fatal("integration query accepted assistant configuration response")
	}
	if _, err := server.findPlatformResources(t.Context(), input, map[string]any{"query": "fixture"}); err == nil {
		t.Fatal("ordinary query accepted assistant configuration response")
	}
}

func TestSystemAssistantFreshCatalogCanDiscoverProjectAssistantWithoutScreenRebind(t *testing.T) {
	input, arguments, response := assistantFreshCatalogFixture(runtimecontract.AssistantScopeSystem, "ASSISTANTS")
	projectEntry := proto.Clone(response.AssistantConfigurationCatalog.Entries[0]).(*controlplanev1.AssistantConfigurationCatalogEntry)
	projectEntry.Ref, projectEntry.ScopeKind, projectEntry.ProjectRef, projectEntry.AssistantProfileRef = "agt_project123", "PROJECT", "prj_target123", "asstprof_target123"
	response.AssistantConfigurationCatalog.Entries = append(response.AssistantConfigurationCatalog.Entries, projectEntry)
	request, err := parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := castAssistantConfigurationCatalog(input, request, response.AssistantConfigurationCatalog); err != nil {
		t.Fatal(err)
	}
	input, arguments, response = assistantFreshCatalogFixture(runtimecontract.AssistantScopeProject, "MODELS")
	input.AssistantScope, input.AgentRef, input.AssistantProfileRef = runtimecontract.AssistantScopeSystem, "agt_system123", ""
	request, err = parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := castAssistantConfigurationCatalog(input, request, response.AssistantConfigurationCatalog); err != nil {
		t.Fatal(err)
	}
	input, arguments, response = assistantFreshCatalogFixture(runtimecontract.AssistantScopeProject, "ASSISTANTS")
	response.AssistantConfigurationCatalog.Entries[0].Ref = "agt_foreign123"
	request, err = parseAssistantConfigurationCatalog(input, arguments, arguments["assistant_configuration_catalog"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := castAssistantConfigurationCatalog(input, request, response.AssistantConfigurationCatalog); err == nil {
		t.Fatal("project assistant catalog accepted another assistant")
	}
}
