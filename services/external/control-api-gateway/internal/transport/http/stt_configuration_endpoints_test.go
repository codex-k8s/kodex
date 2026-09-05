package httptransport

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

const typedSTTDraftBody = `{"configurationRef":"mcfg_fixture01","name":"Dictation","specification":{"enabled":true,"providerAccountRef":"pacc_fixture01","model":"gpt-transcribe","language":"","permissionKey":"platform.stt.use","parameters":{"languages":["ru","en"],"keywords":["Kodex"],"prompt":"Names","temperature":0.2,"chunkingStrategy":"auto","stream":false},"maximumAudioBytes":10485760,"maximumAudioDurationMilliseconds":120000,"providerTimeoutMilliseconds":15000}}`

func TestTypedSTTDraftMapsImmutableJSONAndOCC(t *testing.T) {
	client := &managedRPCRecorder{}
	w := httptest.NewRecorder()
	managedTestHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1/system-stt-configurations/typed-drafts", typedSTTDraftBody))
	if w.Code != 201 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	input, ok := client.request.(*controlplanev1.CreateSystemSTTConfigurationDraftRequest)
	if !ok || input.GetProjectRef() != "" || input.GetConfigurationRef() != "mcfg_fixture01" || input.GetContentFormat() != "JSON" || input.GetMutation().GetExpectedVersion() != 3 || input.GetMutation().GetIdempotencyKey() != "managed-fixture-01" {
		t.Fatalf("wrong mapping: %v", client.request)
	}
	var document struct {
		Name string                           `json:"name"`
		STT  generated.SystemSTTSpecification `json:"stt"`
	}
	if err := json.Unmarshal([]byte(input.Content), &document); err != nil {
		t.Fatal(err)
	}
	p := document.STT.Parameters
	if document.Name != "Dictation" || len(p.Languages) != 2 || len(p.Keywords) != 1 || p.Prompt != "Names" || p.Temperature != 0.2 || p.ChunkingStrategy != "auto" || p.Stream || document.STT.MaximumAudioBytes != 10<<20 || document.STT.MaximumAudioDurationMilliseconds != 120000 || document.STT.ProviderTimeoutMilliseconds != 15000 {
		t.Fatal("typed specification lost fields")
	}
}

func TestTypedSTTDraftRejectsUnsupportedBeforeRPC(t *testing.T) {
	for _, tc := range []struct{ name, from, to string }{
		{"unknown-model", `"gpt-transcribe"`, `"invented-model"`},
		{"incompatible-model", `"gpt-transcribe"`, `"whisper-1"`},
		{"mixed-language", `"language":""`, `"language":"ru"`},
		{"stream", `"stream":false`, `"stream":true`},
		{"temperature", `"temperature":0.2`, `"temperature":2`},
		{"size", `"maximumAudioBytes":10485760`, `"maximumAudioBytes":26214401`},
		{"duration", `"maximumAudioDurationMilliseconds":120000`, `"maximumAudioDurationMilliseconds":1800001`},
		{"small-size", `"maximumAudioBytes":10485760`, `"maximumAudioBytes":1023`},
		{"short-duration", `"maximumAudioDurationMilliseconds":120000`, `"maximumAudioDurationMilliseconds":999`},
		{"timeout", `"providerTimeoutMilliseconds":15000`, `"providerTimeoutMilliseconds":0`},
		{"short-timeout", `"providerTimeoutMilliseconds":15000`, `"providerTimeoutMilliseconds":999`},
		{"long-timeout", `"providerTimeoutMilliseconds":15000`, `"providerTimeoutMilliseconds":60000`},
		{"authority", `"name":"Dictation"`, `"name":"Dictation","projectRef":"prj_forged01"`},
		{"credential", `"enabled":true`, `"enabled":true,"apiKey":"forbidden-fixture"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &managedRPCRecorder{}
			w := httptest.NewRecorder()
			managedTestHandler(client).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1/system-stt-configurations/typed-drafts", strings.Replace(typedSTTDraftBody, tc.from, tc.to, 1)))
			if w.Code != 400 || client.request != nil {
				t.Fatalf("status=%d called=%t", w.Code, client.request != nil)
			}
		})
	}
}

func TestSystemSTTReadMapsParametersWithoutClaimingAvailability(t *testing.T) {
	client := &managedRPCRecorder{}
	w := httptest.NewRecorder()
	managedTestHandler(client).ServeHTTP(w, managedTestRequest(http.MethodGet, "/api/v1/system-stt-configuration", ""))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var result generated.SystemSTTConfiguration
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Ready || result.Enabled || result.MaximumAudioBytes != 10<<20 || result.MaximumAudioDurationMilliseconds != 120000 || result.ProviderTimeoutMilliseconds != 15000 || result.Parameters.Languages == nil || result.Parameters.Keywords == nil {
		t.Fatalf("read lost config or invented readiness: %s", w.Body.String())
	}
}

func TestSystemSTTReadRejectsMalformedParameters(t *testing.T) {
	for _, mutate := range []func(*controlplanev1.SystemSTTConfiguration){
		func(v *controlplanev1.SystemSTTConfiguration) { v.Parameters = nil },
		func(v *controlplanev1.SystemSTTConfiguration) { v.Parameters.Temperature = math.NaN() },
		func(v *controlplanev1.SystemSTTConfiguration) { v.Parameters.Stream = true },
		func(v *controlplanev1.SystemSTTConfiguration) { v.MaximumAudioBytes = math.MaxUint64 },
		func(v *controlplanev1.SystemSTTConfiguration) { v.ProviderTimeoutMilliseconds = 0 },
		func(v *controlplanev1.SystemSTTConfiguration) { v.ProviderTimeoutMilliseconds = 60000 },
		func(v *controlplanev1.SystemSTTConfiguration) { v.ProviderTimeoutMilliseconds = 999 },
		func(v *controlplanev1.SystemSTTConfiguration) { v.MaximumAudioBytes = 1023 },
		func(v *controlplanev1.SystemSTTConfiguration) { v.MaximumAudioDurationMilliseconds = 999 },
	} {
		v := &controlplanev1.SystemSTTConfiguration{ProviderAccountRef: "pacc_fixture01", Model: "gpt-transcribe", PermissionKey: "platform.stt.use", Parameters: &controlplanev1.SystemSTTParameters{}, MaximumAudioBytes: 10 << 20, MaximumAudioDurationMilliseconds: 120000, ProviderTimeoutMilliseconds: 15000}
		mutate(v)
		if _, ok := systemSTTSpecificationView(v); ok {
			t.Fatal("invalid upstream STT accepted")
		}
	}
}

func TestSystemSTTReadAcceptsExecutablePolicyBoundaries(t *testing.T) {
	for _, bounds := range [][3]uint64{{1024, 1000, 1000}, {25 << 20, 1800000, 15000}} {
		value := &controlplanev1.SystemSTTConfiguration{ProviderAccountRef: "pacc_fixture01", Model: "gpt-transcribe", PermissionKey: "platform.stt.use",
			Parameters: &controlplanev1.SystemSTTParameters{}, MaximumAudioBytes: bounds[0], MaximumAudioDurationMilliseconds: bounds[1], ProviderTimeoutMilliseconds: bounds[2]}
		view, ok := systemSTTSpecificationView(value)
		if !ok || view.MaximumAudioBytes != int64(bounds[0]) || view.MaximumAudioDurationMilliseconds != int64(bounds[1]) || view.ProviderTimeoutMilliseconds != int64(bounds[2]) {
			t.Fatal("executable policy boundary was rejected or changed")
		}
	}
}
