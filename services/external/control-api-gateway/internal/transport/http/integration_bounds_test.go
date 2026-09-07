package httptransport

import (
	"encoding/json"
	"math"
	"net/http/httptest"
	"strconv"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func TestIntegrationCatalogShippedInputBoundsRemainLossless(t *testing.T) {
	packages, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	response := &cp.ListIntegrationDefinitionsResponse{CoreReady: true}
	large := 0
	for _, p := range packages {
		definition := &cp.IntegrationDefinition{Key: p.Metadata.Key, Version: 1, Origin: cp.IntegrationDefinitionOrigin_INTEGRATION_DEFINITION_ORIGIN_SHIPPED}
		for _, capability := range p.Spec.Capabilities {
			item := &cp.IntegrationCapability{Key: capability.Key}
			for _, field := range capability.InputFields {
				item.InputFields = append(item.InputFields, &cp.IntegrationConfigurationField{Key: field.Key, ValueType: field.Type, Minimum: field.Minimum, Maximum: field.Maximum, HasMinimum: field.Type == "INTEGER", HasMaximum: field.Type == "INTEGER"})
				if field.Maximum > maximumSafeJSONInteger {
					large++
				}
			}
			definition.Capabilities = append(definition.Capabilities, item)
		}
		response.Definitions = append(response.Definitions, definition)
	}
	if large < 2 {
		t.Fatal("shipped large input fixtures disappeared")
	}
	recorder := &catalogRPCRecorder{response: response}
	handler := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(recorder)}})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/integration-definitions?pageSize=40", nil))
	if w.Code != 200 {
		t.Fatalf("shipped catalog status=%d", w.Code)
	}
	var body map[string]any
	if json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatal("invalid catalog JSON")
	}
	count := 0
	for _, definition := range body["items"].([]any) {
		for _, capability := range definition.(map[string]any)["capabilities"].([]any) {
			for _, field := range capability.(map[string]any)["inputFields"].([]any) {
				if maximum, ok := field.(map[string]any)["maximum"].(string); ok {
					if maximum != strconv.FormatInt(math.MaxInt64, 10) {
						t.Fatal("bound rounded")
					}
					count++
				}
			}
		}
	}
	if count != large {
		t.Fatal("large bounds omitted")
	}
	if _, err := ProtoMap(response); err != nil {
		t.Fatal("realtime projection rejects catalog")
	}
}

func TestIntegrationBoundExactEdgesAndOtherIntegerGuards(t *testing.T) {
	for _, bound := range []int64{math.MinInt64, -maximumSafeJSONInteger - 1, -maximumSafeJSONInteger, 0, maximumSafeJSONInteger, maximumSafeJSONInteger + 1, math.MaxInt64} {
		value, err := messageMap(&cp.IntegrationConfigurationField{Minimum: bound, Maximum: bound, HasMinimum: true, HasMaximum: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"minimum", "maximum"} {
			if bound < -maximumSafeJSONInteger || bound > maximumSafeJSONInteger {
				if value[key] != strconv.FormatInt(bound, 10) {
					t.Fatal("lossy string bound")
				}
			} else if value[key] != float64(bound) {
				t.Fatal("safe bound changed type")
			}
		}
	}
	for _, bound := range []string{"9223372036854775808", "-9223372036854775809", "private-sentinel"} {
		if normalizeProtoJSONShape(map[string]any{"maximum": bound}, (&cp.IntegrationConfigurationField{}).ProtoReflect().Descriptor()) == nil {
			t.Fatal("invalid int64 accepted")
		}
	}
	if _, err := messageMap(&cp.MutationContext{ExpectedVersion: func() *int64 { v := int64(math.MaxInt64); return &v }()}); err == nil {
		t.Fatal("non-metadata guard weakened")
	}
}
