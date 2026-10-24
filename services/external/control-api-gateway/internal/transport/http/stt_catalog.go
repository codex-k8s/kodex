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

// GetSystemSTTModelCatalog requires an attached STT client, reads its model catalog within the
// bounded catalog timeout and returns only a valid public snapshot. RPC failures are mapped safely
// without a provider fallback.
// GetSystemSTTModelCatalog требует подключённый STT клиент, читает его каталог моделей в
// ограниченный catalog timeout и выдаёт только корректный публичный снимок. Ошибки RPC безопасно
// отображаются без fallback провайдера.
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

// sttCatalogView validates snapshot identity/time, safe recommendation limits, unique bounded
// model metadata and finite ordered temperature ranges, then copies public arrays. The recommended
// model must exist; actual compatibility decisions remain with the STT adapter.
// sttCatalogView проверяет identity/time снимка, безопасные рекомендованные лимиты, уникальные
// ограниченные metadata моделей и конечные упорядоченные температуры, затем копирует публичные
// массивы. Рекомендованная модель обязана существовать; решения совместимости остаются у STT
// adapter.
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

// sttCatalogStrings checks at most 32 unique catalog strings using bounded text rules. The
// allowEmpty flag applies to individual values, while an empty list remains valid.
// sttCatalogStrings проверяет не более 32 уникальных строк каталога по ограниченным текстовым
// правилам. Флаг allowEmpty относится к отдельным значениям, а пустой список допустим.
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

// sttCatalogText checks optional emptiness, a byte bound, UTF-8, no surrounding whitespace and no
// Unicode controls without altering catalog text.
// sttCatalogText проверяет допустимость пустоты, байтовый лимит, UTF-8, отсутствие внешних
// пробелов и Unicode управляющих символов без изменения текста каталога.
func sttCatalogText(value string, maximum int, allowEmpty bool) bool {
	return (allowEmpty || value != "") && len(value) <= maximum && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}
