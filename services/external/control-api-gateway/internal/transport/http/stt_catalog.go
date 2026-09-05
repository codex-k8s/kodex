package httptransport

import (
	"context"
	"math"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) GetSystemSTTModelCatalog(w http.ResponseWriter, r *http.Request) {
	if server.speech == nil {
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", true)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), speechAvailabilityTimeout)
	defer cancel()
	response, err := server.speech.GetModelCatalog(ctx, &sttv1.GetModelCatalogRequest{})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	view, ok := sttCatalogView(response.GetCatalog())
	if !ok {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// Совместимость моделей принадлежит adapter; HTTP проверяет только форму снимка.
func sttCatalogView(value *sttv1.TranscriptionModelCatalog) (generated.STTModelCatalog, bool) {
	if value == nil || !sttCatalogText(value.GetVersion(), 160, false) || value.GetObservedAt() == nil || value.GetObservedAt().CheckValid() != nil ||
		!sttCatalogText(value.GetRecommendedModel(), 160, false) || !sttCatalogText(value.GetResponseFormat(), 64, false) ||
		value.GetRecommendedMaximumAudioBytes() == 0 || value.GetRecommendedMaximumAudioBytes() > uint64(maximumSafeJSONInteger) ||
		value.GetRecommendedMaximumAudioDurationMilliseconds() == 0 || value.GetRecommendedMaximumAudioDurationMilliseconds() > uint64(maximumSafeJSONInteger) ||
		len(value.GetModels()) == 0 || len(value.GetModels()) > 128 {
		return generated.STTModelCatalog{}, false
	}
	result := generated.STTModelCatalog{Version: value.GetVersion(), ObservedAt: value.GetObservedAt().AsTime(),
		RecommendedModel: value.GetRecommendedModel(), ResponseFormat: value.GetResponseFormat(),
		RecommendedMaximumAudioBytes:                int64(value.GetRecommendedMaximumAudioBytes()),
		RecommendedMaximumAudioDurationMilliseconds: int64(value.GetRecommendedMaximumAudioDurationMilliseconds()),
		Models: make([]generated.STTModelProfile, 0, len(value.GetModels()))}
	seen := make(map[string]bool, len(value.GetModels()))
	for _, model := range value.GetModels() {
		if model == nil || !sttCatalogText(model.GetModel(), 160, false) || seen[model.GetModel()] ||
			!sttCatalogStrings(model.GetParameterNames(), false) || !sttCatalogStrings(model.GetChunkingStrategies(), true) ||
			math.IsNaN(model.GetMinimumTemperature()) || math.IsNaN(model.GetMaximumTemperature()) ||
			math.IsInf(model.GetMinimumTemperature(), 0) || math.IsInf(model.GetMaximumTemperature(), 0) ||
			model.GetMinimumTemperature() > model.GetMaximumTemperature() || model.GetStreamEnabled() && !model.GetFileStreamSupported() {
			return generated.STTModelCatalog{}, false
		}
		seen[model.GetModel()] = true
		result.Models = append(result.Models, generated.STTModelProfile{Model: model.GetModel(), Legacy: model.GetLegacy(),
			ParameterNames: append([]string{}, model.GetParameterNames()...), ChunkingStrategies: append([]string{}, model.GetChunkingStrategies()...),
			FileStreamSupported: model.GetFileStreamSupported(), StreamEnabled: model.GetStreamEnabled(),
			MaximumPromptBytes: int64(model.GetMaximumPromptBytes()), MaximumKeywords: int64(model.GetMaximumKeywords()), MaximumKeywordBytes: int64(model.GetMaximumKeywordBytes()),
			MinimumTemperature: model.GetMinimumTemperature(), MaximumTemperature: model.GetMaximumTemperature()})
	}
	return result, seen[value.GetRecommendedModel()]
}

func sttCatalogStrings(values []string, allowEmpty bool) bool {
	if len(values) > 32 {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] || !sttCatalogText(value, 64, allowEmpty) {
			return false
		}
		seen[value] = true
	}
	return true
}

func sttCatalogText(value string, maximum int, allowEmpty bool) bool {
	return (allowEmpty || value != "") && len(value) <= maximum && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}
