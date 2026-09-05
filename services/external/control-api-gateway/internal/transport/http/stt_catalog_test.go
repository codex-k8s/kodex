package httptransport

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type sttCatalogClient struct {
	sttv1.SpeechToTextServiceClient
	response *sttv1.TranscriptionModelCatalog
	err      error
	ctx      context.Context
	request  *sttv1.GetModelCatalogRequest
}

func (client *sttCatalogClient) GetModelCatalog(ctx context.Context, request *sttv1.GetModelCatalogRequest, _ ...grpc.CallOption) (*sttv1.GetModelCatalogResponse, error) {
	client.ctx, client.request = ctx, request
	return &sttv1.GetModelCatalogResponse{Catalog: client.response}, client.err
}

func sttCatalogFixture() *sttv1.TranscriptionModelCatalog {
	return &sttv1.TranscriptionModelCatalog{Version: "adapter-fixture-v2", ObservedAt: timestamppb.New(time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)),
		RecommendedModel: "adapter-future-file-model", RecommendedMaximumAudioBytes: 12345, RecommendedMaximumAudioDurationMilliseconds: 23456, ResponseFormat: "json",
		Models: []*sttv1.TranscriptionModelProfile{{Model: "adapter-future-file-model", Legacy: true, ParameterNames: []string{"languages", "keywords", "stream"},
			ChunkingStrategies: []string{"", "auto"}, FileStreamSupported: true, StreamEnabled: false,
			MaximumPromptBytes: 896, MaximumKeywords: 64, MaximumKeywordBytes: 128, MinimumTemperature: 0, MaximumTemperature: 1}}}
}

func TestSTTCatalogUsesAdapterWithoutConfigurationOrAudio(t *testing.T) {
	client := &sttCatalogClient{response: sttCatalogFixture()}
	response := httptest.NewRecorder()
	generated.Handler(&Server{speech: client}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/system-stt/model-catalog", nil))
	var result generated.STTModelCatalog
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatalf("catalog failed: %d %s", response.Code, response.Body.String())
	}
	if result.Version != client.response.Version || !result.ObservedAt.Equal(client.response.ObservedAt.AsTime()) || result.RecommendedModel != client.response.RecommendedModel ||
		result.RecommendedMaximumAudioBytes != 12345 || result.RecommendedMaximumAudioDurationMilliseconds != 23456 || len(result.Models) != 1 ||
		!result.Models[0].Legacy || result.Models[0].MaximumKeywords != 64 || result.Models[0].StreamEnabled || len(result.Models[0].ParameterNames) != 3 {
		t.Fatal("adapter catalog was replaced or lost fields")
	}
	if client.request == nil || client.ctx.Err() != context.Canceled {
		t.Fatal("catalog did not use bounded unary request")
	}
	deadline, ok := client.ctx.Deadline()
	if !ok || time.Until(deadline) > speechAvailabilityTimeout {
		t.Fatal("catalog deadline is absent or unbounded")
	}
	if strings.Contains(response.Body.String(), "ready") || strings.Contains(response.Body.String(), "credential") {
		t.Fatal("catalog was confused with runtime readiness")
	}
}

func TestSTTCatalogRejectsMalformedProducer(t *testing.T) {
	cases := map[string]func(*sttv1.TranscriptionModelCatalog){
		"version":                  func(v *sttv1.TranscriptionModelCatalog) { v.Version = "" },
		"timestamp":                func(v *sttv1.TranscriptionModelCatalog) { v.ObservedAt = nil },
		"invalid timestamp":        func(v *sttv1.TranscriptionModelCatalog) { v.ObservedAt.Seconds = math.MaxInt64 },
		"zero budget":              func(v *sttv1.TranscriptionModelCatalog) { v.RecommendedMaximumAudioBytes = 0 },
		"unsafe budget":            func(v *sttv1.TranscriptionModelCatalog) { v.RecommendedMaximumAudioDurationMilliseconds = 1 << 53 },
		"missing recommended":      func(v *sttv1.TranscriptionModelCatalog) { v.RecommendedModel = "missing" },
		"empty models":             func(v *sttv1.TranscriptionModelCatalog) { v.Models = nil },
		"nil model":                func(v *sttv1.TranscriptionModelCatalog) { v.Models[0] = nil },
		"duplicate model":          func(v *sttv1.TranscriptionModelCatalog) { v.Models = append(v.Models, v.Models[0]) },
		"control character":        func(v *sttv1.TranscriptionModelCatalog) { v.Models[0].Model = "bad\nmodel" },
		"duplicate parameter":      func(v *sttv1.TranscriptionModelCatalog) { v.Models[0].ParameterNames = []string{"prompt", "prompt"} },
		"oversized parameter list": func(v *sttv1.TranscriptionModelCatalog) { v.Models[0].ParameterNames = make([]string, 33) },
		"duplicate strategy":       func(v *sttv1.TranscriptionModelCatalog) { v.Models[0].ChunkingStrategies = []string{"", ""} },
		"nan":                      func(v *sttv1.TranscriptionModelCatalog) { v.Models[0].MaximumTemperature = math.NaN() },
		"infinity":                 func(v *sttv1.TranscriptionModelCatalog) { v.Models[0].MinimumTemperature = math.Inf(-1) },
		"reversed bounds":          func(v *sttv1.TranscriptionModelCatalog) { v.Models[0].MinimumTemperature = 2 },
		"unsupported stream": func(v *sttv1.TranscriptionModelCatalog) {
			v.Models[0].StreamEnabled, v.Models[0].FileStreamSupported = true, false
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			value := sttCatalogFixture()
			mutate(value)
			response := httptest.NewRecorder()
			generated.Handler(&Server{speech: &sttCatalogClient{response: value}}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/system-stt/model-catalog", nil))
			if response.Code != http.StatusBadGateway {
				t.Fatalf("invalid catalog returned %d", response.Code)
			}
		})
	}
}

func TestSTTCatalogPreservesAuthorityAndAvailabilityErrors(t *testing.T) {
	for code, expected := range map[codes.Code]int{codes.PermissionDenied: 403, codes.Unauthenticated: 401, codes.Unavailable: 503, codes.InvalidArgument: 400} {
		response := httptest.NewRecorder()
		generated.Handler(&Server{speech: &sttCatalogClient{err: status.Error(code, "private provider diagnostic")}}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/system-stt/model-catalog", nil))
		if response.Code != expected || strings.Contains(response.Body.String(), "private provider") {
			t.Fatalf("RPC %v returned %d", code, response.Code)
		}
	}
	response := httptest.NewRecorder()
	generated.Handler(&Server{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/system-stt/model-catalog", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("absent adapter returned %d", response.Code)
	}
}
