package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/getkin/kin-openapi/openapi3"
)

// TestIntegrationAdapterCatalogContractParity sends all real shipped adapter keys through the HTTP serializer and checks exact parity with the public schema/generated enum, so newly registered typed consumers cannot disappear at the frontend boundary.
// TestIntegrationAdapterCatalogContractParity проводит все настоящие shipped adapter keys через HTTP serializer и проверяет exact parity с public schema/generated enum, чтобы новые typed consumers не терялись на frontend boundary.
func TestIntegrationAdapterCatalogContractParity(t *testing.T) {
	packages, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := openapi3.NewLoader().LoadFromFile("../../../../../../contracts/openapi/control-api-gateway/v1/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	adapterSchema := spec.Components.Schemas["IntegrationDefinition"].Value.Properties["adapter"].Value
	expected := map[string]bool{}
	response := &cp.ListIntegrationDefinitionsResponse{CoreReady: true}
	for _, definition := range packages {
		expected[definition.Spec.Adapter] = true
		response.Definitions = append(response.Definitions, &cp.IntegrationDefinition{Key: definition.Metadata.Key, Version: 1, Origin: cp.IntegrationDefinitionOrigin_INTEGRATION_DEFINITION_ORIGIN_SHIPPED, Adapter: definition.Spec.Adapter})
	}
	if len(adapterSchema.Enum) != len(expected) {
		t.Fatal("public adapter enum does not match shipped registry")
	}
	for _, value := range adapterSchema.Enum {
		key, ok := value.(string)
		if !ok || !expected[key] || !generated.IntegrationDefinitionAdapter(key).Valid() {
			t.Fatal("public adapter enum contains an unregistered value")
		}
	}
	if adapterSchema.VisitJSON("UNREGISTERED_PRIVATE_PLUGIN") == nil || generated.IntegrationDefinitionAdapter("UNREGISTERED_PRIVATE_PLUGIN").Valid() {
		t.Fatal("unknown adapter received public contract authority")
	}
	recorder := &catalogRPCRecorder{response: response}
	handler := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(recorder)}})
	wire := httptest.NewRecorder()
	handler.ServeHTTP(wire, httptest.NewRequest("GET", "/api/v1/integration-definitions?pageSize=40", nil))
	if wire.Code != 200 {
		t.Fatal("public adapter catalog failed", wire.Code)
	}
	var body struct {
		Items []struct {
			Key     string                                 `json:"key"`
			Adapter generated.IntegrationDefinitionAdapter `json:"adapter"`
		} `json:"items"`
	}
	if json.Unmarshal(wire.Body.Bytes(), &body) != nil || len(body.Items) != len(packages) {
		t.Fatal("public adapter catalog lost shipped items")
	}
	for _, item := range body.Items {
		if !item.Adapter.Valid() || string(item.Adapter) != packages[item.Key].Spec.Adapter {
			t.Fatal("public adapter serialization changed registry meaning")
		}
	}
}
