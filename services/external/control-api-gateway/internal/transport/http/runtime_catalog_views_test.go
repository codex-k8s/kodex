package httptransport

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// runtimeOverlayGolden embeds the owner-produced overlay schema used to verify exact readback
// without regenerating its digest from a gateway-specific schema.
// runtimeOverlayGolden встраивает схему overlay от владельца для проверки точного чтения
// без пересоздания её дайджеста по отдельной схеме gateway.
//
//go:embed testdata/runtime_overlay_schema.json
var runtimeOverlayGolden []byte

// runtimeOverlayProducerDigest pins the unchanged digest of the owner
// runtimecontract.OverlaySchema([low, high], high) golden fixture rather than a gateway-defined
// field set.
// runtimeOverlayProducerDigest фиксирует неизменный дайджест эталона owner
// runtimecontract.OverlaySchema([low, high], high), полученного из владельца, а не из прежнего
// набора полей gateway.
const runtimeOverlayProducerDigest = "e84f6b4c641531755549ee8c79f186e3f8778511afe2e63786269a776d90bceb"

// runtimePublishFixture provides synthetic fixed-provider runtime publication input with exact
// model catalog revision and digest pins.
// runtimePublishFixture задаёт синтетический ввод публикации runtime с фиксированным провайдером и
// точными pins ревизии и дайджеста каталога моделей.
const runtimePublishFixture = `{"runtimeProfileRef":"rtp_fixture01","model":"fixture","providerPolicyMode":"FIXED","providerAccounts":[{"accountRef":"pacc_fixture01","weight":1,"catalogRevision":"mcat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","catalogDigest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","providerDefinitionKey":"openai-codex"}]}`

// runtimeOverlayFixture decodes the embedded owner-produced overlay schema and panics if the
// checked-in fixture cannot be parsed.
// runtimeOverlayFixture декодирует встроенную схему overlay от владельца и вызывает panic при
// невозможности разобрать сохранённый в репозитории эталон.
func runtimeOverlayFixture() *cp.ConfigOverlaySchema {
	value := &cp.ConfigOverlaySchema{}
	if err := protojson.Unmarshal(runtimeOverlayGolden, value); err != nil {
		panic(err)
	}
	return value
}

// runtimeCatalogStatusFixture builds a fixed-time ready account catalog observation from
// REMOTE_CODEX with a fifteen-minute expiry and no failure.
// runtimeCatalogStatusFixture создаёт наблюдение готового каталога аккаунта от REMOTE_CODEX с
// фиксированным временем, сроком пятнадцать минут и отсутствием ошибки.
func runtimeCatalogStatusFixture() *cp.ProviderModelCatalogStatus {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	return &cp.ProviderModelCatalogStatus{State: cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_READY, Source: cp.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_CODEX, Failure: cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_NONE, ObservedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(15 * time.Minute))}
}

// TestRuntimeCatalogPublishExactPins checks unchanged model catalog authority pins and rejects
// missing or mismatched pins, caller-assigned defaults and weight overflow before RPC.
// TestRuntimeCatalogPublishExactPins проверяет неизменность pins полномочий каталога моделей и
// отказ до RPC отсутствующим либо несовпадающим pins, назначенному клиентом умолчанию и
// переполнению веса.
func TestRuntimeCatalogPublishExactPins(t *testing.T) {
	client := &contextRPCRecorder{corrupt: func(response proto.Message) {
		field := response.ProtoReflect().Descriptor().Fields().ByName("runtime_configuration")
		response.ProtoReflect().Set(field, protoreflect.ValueOfMessage(runtimeContextFixture().ProtoReflect()))
	}}
	w := httptest.NewRecorder()
	contextHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPut, "/api/v1/agents/agt_fixture01/runtime-configuration", runtimePublishFixture))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	request := client.request.(*cp.PublishAgentRuntimeConfigurationRequest)
	if len(request.ProviderAccounts) != 1 || request.ProviderAccounts[0].CatalogRevision != "mcat_"+strings.Repeat("a", 64) || request.ProviderAccounts[0].CatalogDigest != strings.Repeat("a", 64) || request.ProviderAccounts[0].ProviderDefinitionKey != "openai-codex" || request.ProviderAccounts[0].DefaultReasoningEffort != "" {
		t.Fatal("catalog authority pin changed")
	}
	for name, body := range map[string]string{
		"missing pin":           strings.Replace(runtimePublishFixture, `"catalogDigest":"`+strings.Repeat("a", 64)+`",`, "", 1),
		"mismatch":              strings.Replace(runtimePublishFixture, `"catalogDigest":"a`, `"catalogDigest":"b`, 1),
		"self assigned default": strings.Replace(runtimePublishFixture, `"weight":1`, `"weight":1,"defaultReasoningEffort":"high"`, 1),
		"weight overflow":       strings.Replace(runtimePublishFixture, `"weight":1`, `"weight":2147483648`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			client := &contextRPCRecorder{}
			w := httptest.NewRecorder()
			contextHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPut, "/api/v1/agents/agt_fixture01/runtime-configuration", body))
			if w.Code != 400 || client.request != nil {
				t.Fatalf("status=%d rpc=%v", w.Code, client.request)
			}
		})
	}
}

// TestRuntimeOverlaySchemaAndDiagnostics checks schema and diagnostic-position projection while
// rejecting missing or tampered schema, unknown fields or codes and unsafe diagnostic text.
// TestRuntimeOverlaySchemaAndDiagnostics проверяет проекцию схемы и позиции диагностики с отказом
// отсутствующей либо изменённой схеме, неизвестным полям и кодам и небезопасному тексту
// диагностики.
func TestRuntimeOverlaySchemaAndDiagnostics(t *testing.T) {
	view := runtimeContextFixture()
	view.DraftOverlay = &cp.ConfigOverlayVersion{Diagnostics: []*cp.ConfigOverlayDiagnostic{{Code: "CONFIG_OVERLAY_EFFORT_UNSUPPORTED", Key: "model_reasoning_effort", Line: 3, Column: 5, Message: "Reasoning effort is not supported by the selected model"}}, SchemaRevision: view.OverlaySchema.Revision, SchemaDigest: view.OverlaySchema.Digest}
	value, err := messageMap(view)
	if err != nil {
		t.Fatal(err)
	}
	if value["overlaySchema"] == nil || value["draftOverlay"].(map[string]any)["diagnostics"].([]any)[0].(map[string]any)["line"] != float64(3) {
		t.Fatal("typed schema or position lost")
	}
	for name, mutate := range map[string]func(*cp.AgentRuntimeConfigurationView){
		"missing schema": func(v *cp.AgentRuntimeConfigurationView) { v.OverlaySchema = nil },
		"tampered hover": func(v *cp.AgentRuntimeConfigurationView) { v.OverlaySchema.Fields[0].Hover = "changed" },
		"unknown key":    func(v *cp.AgentRuntimeConfigurationView) { v.OverlaySchema.Fields[0].Key = "api_key" },
		"unknown code":   func(v *cp.AgentRuntimeConfigurationView) { v.DraftOverlay.Diagnostics[0].Code = "UNKNOWN" },
		"raw error":      func(v *cp.AgentRuntimeConfigurationView) { v.DraftOverlay.Diagnostics[0].Message = "raw-secret-value" },
		"negative line":  func(v *cp.AgentRuntimeConfigurationView) { v.DraftOverlay.Diagnostics[0].Line = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			copy := proto.Clone(view).(*cp.AgentRuntimeConfigurationView)
			mutate(copy)
			w := httptest.NewRecorder()
			writeMessage(w, 200, copy, "", "")
			if w.Code != 502 || strings.Contains(w.Body.String(), "raw-secret-value") {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
}

// TestRuntimeOverlayNativeSearchReadPath checks the exact owner-produced five-field schema, native
// search modes and default through HTTP and generated OpenAPI enums.
// TestRuntimeOverlayNativeSearchReadPath проверяет точную схему из пяти полей от владельца, режимы
// и умолчание native search через HTTP и generated OpenAPI enum.
func TestRuntimeOverlayNativeSearchReadPath(t *testing.T) {
	view := runtimeContextFixture()
	if view.OverlaySchema.Digest != runtimeOverlayProducerDigest || view.OverlaySchema.Revision != "cos_"+runtimeOverlayProducerDigest || len(view.OverlaySchema.Fields) != 5 {
		t.Fatal("golden does not match the current five-field producer schema")
	}
	var search *cp.ConfigOverlayField
	for _, field := range view.OverlaySchema.Fields {
		if field.Key == "web_search" {
			search = field
		}
	}
	if search == nil || search.ValueType != "string" || search.DefaultValue != "cached" || !slices.Equal(search.AllowedValues, []string{"disabled", "cached", "indexed", "live"}) {
		t.Fatal("native search schema lost exact pinned modes or default")
	}
	view.DraftOverlay = &cp.ConfigOverlayVersion{Diagnostics: []*cp.ConfigOverlayDiagnostic{{Code: "CONFIG_OVERLAY_VALUE_INVALID", Key: "web_search", Line: 1, Column: 1, Message: "Overlay value is not allowed"}}, SchemaRevision: view.OverlaySchema.Revision, SchemaDigest: view.OverlaySchema.Digest}
	client := &contextRPCRecorder{corrupt: func(response proto.Message) {
		field := response.ProtoReflect().Descriptor().Fields().ByName("runtime_configuration")
		response.ProtoReflect().Set(field, protoreflect.ValueOfMessage(view.ProtoReflect()))
	}}
	w := httptest.NewRecorder()
	contextHandler(client).ServeHTTP(w, managedTestRequest(http.MethodGet, "/api/v1/agents/agt_fixture01/runtime-configuration", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("valid producer runtime configuration rejected: status=%d", w.Code)
	}
	var body struct {
		OverlaySchema *struct {
			Digest string `json:"digest"`
			Fields []struct {
				Key           string   `json:"key"`
				ValueType     string   `json:"valueType"`
				AllowedValues []string `json:"allowedValues"`
				DefaultValue  string   `json:"defaultValue"`
			} `json:"fields"`
		} `json:"overlaySchema"`
		DraftOverlay struct {
			Diagnostics []struct{ Key, Code, Message string } `json:"diagnostics"`
		} `json:"draftOverlay"`
	}
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.OverlaySchema == nil || len(body.OverlaySchema.Fields) != 5 || body.OverlaySchema.Digest != runtimeOverlayProducerDigest || len(body.DraftOverlay.Diagnostics) != 1 || body.DraftOverlay.Diagnostics[0].Key != "web_search" {
		t.Fatal("native search schema or safe diagnostic was lost on HTTP readback")
	}
	for _, field := range body.OverlaySchema.Fields {
		if !generated.ConfigOverlayFieldKey(field.Key).Valid() {
			t.Fatalf("native search readback violates generated OpenAPI enum: key=%s", field.Key)
		}
		if field.Key == "web_search" && (field.ValueType != "string" || field.DefaultValue != "cached" || !slices.Equal(field.AllowedValues, search.AllowedValues)) {
			t.Fatal("native search modes or default changed on HTTP readback")
		}
	}
	if generated.ConfigOverlayFieldKey("web_search_custom").Valid() {
		t.Fatal("generated OpenAPI enum accepts an unknown overlay key")
	}
}

// TestRuntimeOverlayNativeSearchRemainsClosed checks rejection of unknown, missing or repeated
// schema fields and altered search modes or defaults even after recomputing their digest.
// TestRuntimeOverlayNativeSearchRemainsClosed проверяет отказ неизвестным, отсутствующим либо
// повторным полям схемы и изменённым режимам либо умолчаниям поиска даже после пересчёта
// дайджеста.
func TestRuntimeOverlayNativeSearchRemainsClosed(t *testing.T) {
	for name, mutate := range map[string]func(*cp.AgentRuntimeConfigurationView){
		"unknown field with matching digest": func(v *cp.AgentRuntimeConfigurationView) {
			v.OverlaySchema.Fields[1].Key = "web_search_custom"
			resignRuntimeOverlayFixture(t, v.OverlaySchema)
		},
		"missing field with matching digest": func(v *cp.AgentRuntimeConfigurationView) {
			v.OverlaySchema.Fields = append(v.OverlaySchema.Fields[:1], v.OverlaySchema.Fields[2:]...)
			resignRuntimeOverlayFixture(t, v.OverlaySchema)
		},
		"duplicate field with matching digest": func(v *cp.AgentRuntimeConfigurationView) {
			v.OverlaySchema.Fields[2].Key = "web_search"
			resignRuntimeOverlayFixture(t, v.OverlaySchema)
		},
		"unknown mode with matching digest": func(v *cp.AgentRuntimeConfigurationView) {
			v.OverlaySchema.Fields[1].AllowedValues = append(v.OverlaySchema.Fields[1].AllowedValues, "arbitrary")
			resignRuntimeOverlayFixture(t, v.OverlaySchema)
		},
		"missing mode with matching digest": func(v *cp.AgentRuntimeConfigurationView) {
			v.OverlaySchema.Fields[1].AllowedValues = []string{"cached", "live"}
			resignRuntimeOverlayFixture(t, v.OverlaySchema)
		},
		"changed default with matching digest": func(v *cp.AgentRuntimeConfigurationView) {
			v.OverlaySchema.Fields[1].DefaultValue = "live"
			resignRuntimeOverlayFixture(t, v.OverlaySchema)
		},
		"native search boolean": func(v *cp.AgentRuntimeConfigurationView) {
			v.OverlaySchema.Fields[1].ValueType = "boolean"
			resignRuntimeOverlayFixture(t, v.OverlaySchema)
		},
		"tampered search hover": func(v *cp.AgentRuntimeConfigurationView) { v.OverlaySchema.Fields[1].Hover = "tampered" },
		"wrong digest": func(v *cp.AgentRuntimeConfigurationView) {
			v.OverlaySchema.Digest = strings.Repeat("a", 64)
			v.OverlaySchema.Revision = "cos_" + v.OverlaySchema.Digest
		},
		"unknown diagnostic key": func(v *cp.AgentRuntimeConfigurationView) { v.DraftOverlay.Diagnostics[0].Key = "web_search_custom" },
		"unknown diagnostic code": func(v *cp.AgentRuntimeConfigurationView) {
			v.DraftOverlay.Diagnostics[0].Code = "WEB_SEARCH_CUSTOM_ERROR"
		},
		"private diagnostic text": func(v *cp.AgentRuntimeConfigurationView) {
			v.DraftOverlay.Diagnostics[0].Message = "SYNTHETIC_PRIVATE_ERROR"
		},
	} {
		t.Run(name, func(t *testing.T) {
			view := runtimeContextFixture()
			view.DraftOverlay = &cp.ConfigOverlayVersion{Diagnostics: []*cp.ConfigOverlayDiagnostic{{Code: "CONFIG_OVERLAY_VALUE_INVALID", Key: "web_search", Message: "Overlay value is not allowed"}}}
			mutate(view)
			w := httptest.NewRecorder()
			writeMessage(w, http.StatusOK, view, "", "")
			if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "SYNTHETIC_PRIVATE_ERROR") {
				t.Fatalf("invalid native search response was accepted or disclosed: status=%d", w.Code)
			}
		})
	}
}

// resignRuntimeOverlayFixture recomputes the canonical schema digest and revision for negative
// fixtures so semantic schema checks are exercised independently of hash mismatch.
// resignRuntimeOverlayFixture пересчитывает канонический дайджест и ревизию отрицательных эталонов
// схемы для проверки семантики независимо от несовпадения хеша.
func resignRuntimeOverlayFixture(t *testing.T, schema *cp.ConfigOverlaySchema) {
	t.Helper()
	canonical := canonicalOverlaySchema{MaximumBytes: schema.MaximumBytes, Fields: []canonicalOverlayField{}}
	for _, field := range schema.Fields {
		canonical.Fields = append(canonical.Fields, canonicalOverlayField{field.Key, field.ValueType, append([]string{}, field.AllowedValues...), field.DefaultValue, field.Description, field.Completion, field.Hover})
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal("cannot encode synthetic schema")
	}
	digest := sha256.Sum256(raw)
	schema.Digest = hex.EncodeToString(digest[:])
	schema.Revision = "cos_" + schema.Digest
}

// TestRuntimeCatalogFreshnessMapping checks known account catalog states, observation and expiry
// validation, account/global status separation and an empty verified ready catalog.
// TestRuntimeCatalogFreshnessMapping проверяет известные состояния каталога аккаунта, времена
// наблюдения и истечения, разделение аккаунтного и глобального статуса и пустой проверенный
// готовый каталог.
func TestRuntimeCatalogFreshnessMapping(t *testing.T) {
	for _, state := range []cp.ProviderModelCatalogState{cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_PENDING, cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_READY, cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_EXPIRED, cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_FAILED} {
		status := runtimeCatalogStatusFixture()
		status.State = state
		if state == cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_FAILED {
			status.Source = 0
			status.Failure = cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_UNVERIFIED_SOURCE
		}
		result, ok := modelCatalogStatusView(status, true)
		if !ok || result.ObservedAt == nil {
			t.Fatalf("state=%s", state)
		}
	}
	for name, mutate := range map[string]func(*cp.ProviderModelCatalogStatus){"unknown state": func(v *cp.ProviderModelCatalogStatus) { v.State = 99 }, "unknown source": func(v *cp.ProviderModelCatalogStatus) { v.Source = 99 }, "unknown failure": func(v *cp.ProviderModelCatalogStatus) { v.Failure = 99 }, "missing observation": func(v *cp.ProviderModelCatalogStatus) { v.ObservedAt = nil }, "reversed expiry": func(v *cp.ProviderModelCatalogStatus) { v.ExpiresAt = v.ObservedAt }} {
		t.Run(name, func(t *testing.T) {
			status := runtimeCatalogStatusFixture()
			mutate(status)
			if _, ok := modelCatalogStatusView(status, true); ok {
				t.Fatal("invalid status accepted")
			}
		})
	}
	if _, ok := modelCatalogStatusView(nil, true); ok {
		t.Fatal("account catalog without status accepted")
	}
	if _, ok := modelCatalogStatusView(runtimeCatalogStatusFixture(), false); ok {
		t.Fatal("global catalog accepted account status")
	}
	response := modelCatalogFixture()
	response.Models = nil
	response.Total = 0
	response.CatalogStatus = runtimeCatalogStatusFixture()
	client := &catalogRPCRecorder{response: response}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/model-capabilities?providerAccountRef=pacc_fixture01", nil))
	var result map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result["catalogStatus"].(map[string]any)["state"] != "READY" {
		t.Fatalf("empty verified catalog status=%d", w.Code)
	}
}

// TestRuntimeCatalogNonReasoningPreservesEmptyDefault checks that non-reasoning capabilities
// retain an empty default and effort list, including the public provider-candidate projection.
// TestRuntimeCatalogNonReasoningPreservesEmptyDefault проверяет сохранение пустых умолчания и
// списка effort для моделей без reasoning, включая публичную проекцию кандидата провайдера.
func TestRuntimeCatalogNonReasoningPreservesEmptyDefault(t *testing.T) {
	model := modelCapabilityFixture()
	model.ReasoningEfforts, model.DefaultReasoningEffort = nil, ""
	if mapped, ok := modelCapabilityView(model); !ok || mapped.DefaultReasoningEffort != "" || len(mapped.ReasoningEfforts) != 0 {
		t.Fatal("non-reasoning capability changed")
	}
	value, err := messageMap(&cp.ProviderAccountCandidate{AccountRef: "pacc_fixture01", Weight: 1, CatalogRevision: "mcat_" + strings.Repeat("a", 64), CatalogDigest: strings.Repeat("a", 64), ProviderDefinitionKey: "openai-codex"})
	if err != nil || value["defaultReasoningEffort"] != "" {
		t.Fatalf("empty server default lost: %v", err)
	}
}
