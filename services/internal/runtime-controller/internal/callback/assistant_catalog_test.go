package callback

import (
	"context"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

type assistantDefinitionCatalogClient struct {
	controlplanev1.RuntimeWorkServiceClient
	request  *controlplanev1.SearchAssistantResourcesRequest
	response *controlplanev1.SearchAssistantResourcesResponse
}

func (client *assistantDefinitionCatalogClient) SearchAssistantResources(_ context.Context, request *controlplanev1.SearchAssistantResourcesRequest, _ ...grpc.CallOption) (*controlplanev1.SearchAssistantResourcesResponse, error) {
	client.request = request
	return client.response, nil
}

func TestAssistantCatalogDiscoversExactIntegrationDefinitionWithoutCredential(t *testing.T) {
	client := &assistantDefinitionCatalogClient{response: &controlplanev1.SearchAssistantResourcesResponse{Definitions: []*controlplanev1.AssistantIntegrationDefinition{{
		Key: "https-json", Name: "HTTPS JSON", Adapter: "HTTPS_JSON_READ", CredentialSecretKey: "token",
		ConfigurationFields: []*controlplanev1.IntegrationConfigurationField{{Key: "base_url", Label: "Base URL", ValueType: "STRING", Format: "HTTPS_ORIGIN", Required: true}},
		CapabilityKeys:      []string{"https_json.resource.read"},
	}}}}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	input := runtimecontract.RunnerInput{SystemAssistant: true, LeaseRef: "lse_current123", LeaseFence: "private-fence", LeaseGeneration: 4}
	result, err := server.configurationCatalog(t.Context(), input, map[string]any{"operation_types": []any{}, "definition_query": " https-json "})
	if err != nil {
		t.Fatal(err)
	}
	if client.request == nil || !client.request.GetIntegrationDefinitionCatalog() || client.request.GetQuery() != "" ||
		client.request.GetDefinitionQuery() != "https-json" || client.request.GetLeaseRef() != input.LeaseRef ||
		client.request.GetFence() != input.LeaseFence || client.request.GetGeneration() != input.LeaseGeneration {
		t.Fatalf("integration catalog lost fenced server-owned lookup: %#v", client.request)
	}
	definitions := result.(map[string]any)["integration_definitions"].([]map[string]any)
	if len(definitions) != 1 || definitions[0]["key"] != "https-json" || definitions[0]["credential_secret_key"] != "token" ||
		len(definitions[0]["configuration_fields"].([]map[string]any)) != 1 {
		t.Fatalf("integration catalog lost exact public fields: %#v", definitions)
	}
	if parameters, permission, _, ok := safeToolCallParameters(input, "get_configuration_catalog", map[string]any{"definition_query": "https-json"}); !ok || permission != "platform.configuration.read" || len(parameters) != 0 {
		t.Fatalf("integration query leaked through tool projection: %#v %q %v", parameters, permission, ok)
	}
	input.AssistantContext = &runtimecontract.RunnerAssistantContext{AllowedOperations: []string{"TEST_INTEGRATION_CONNECTION"}}
	if _, err := server.configurationCatalog(t.Context(), input, map[string]any{"definition_query": "https-json"}); err != nil {
		t.Fatalf("read-only catalog was incorrectly coupled to create permission: %v", err)
	}
}

func TestAssistantCatalogRejectsUnboundOrMalformedDefinitionLookup(t *testing.T) {
	input := runtimecontract.RunnerInput{SystemAssistant: true, LeaseRef: "lse_current123", LeaseFence: "private-fence", LeaseGeneration: 4}
	for _, test := range []struct {
		name      string
		input     runtimecontract.RunnerInput
		arguments map[string]any
	}{
		{"ordinary agent", runtimecontract.RunnerInput{LeaseRef: input.LeaseRef, LeaseFence: input.LeaseFence, LeaseGeneration: 4}, map[string]any{"definition_query": "https"}},
		{"missing fence", runtimecontract.RunnerInput{SystemAssistant: true, LeaseRef: input.LeaseRef, LeaseGeneration: 4}, map[string]any{"definition_query": "https"}},
		{"offset without query", input, map[string]any{"definition_offset": float64(10)}},
		{"negative offset", input, map[string]any{"definition_query": "https", "definition_offset": float64(-1)}},
		{"fractional offset", input, map[string]any{"definition_query": "https", "definition_offset": 1.5}},
		{"unknown argument", input, map[string]any{"definition_query": "https", "actor": "owner"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: &assistantDefinitionCatalogClient{response: &controlplanev1.SearchAssistantResourcesResponse{}}}}
			if result, err := server.configurationCatalog(t.Context(), test.input, test.arguments); err == nil {
				t.Fatalf("accepted invalid definition lookup: %#v", result)
			}
		})
	}
}
