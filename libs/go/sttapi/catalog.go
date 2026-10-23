package sttapi

import (
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ModelCatalog converts a dated adapter compatibility snapshot into a safe
// protobuf catalog. It copies model lists, disables streaming in this surface
// and omits provider credentials; an empty source version yields no catalog.
// RU: ModelCatalog преобразует датированный снимок совместимости адаптера в
// безопасный protobuf-каталог. Списки моделей копируются, streaming для этой
// поверхности выключен, реквизиты провайдера не передаются; при пустой версии
// источника каталог не возвращается.
func ModelCatalog(catalog modelprofile.Catalog) *sttv1.TranscriptionModelCatalog {
	if catalog.Version == "" {
		return nil
	}
	result := &sttv1.TranscriptionModelCatalog{Version: catalog.Version, ObservedAt: timestamppb.New(catalog.ObservedAt),
		RecommendedModel: modelprofile.RecommendedModel, RecommendedMaximumAudioBytes: modelprofile.RecommendedMaximumBytes,
		RecommendedMaximumAudioDurationMilliseconds: uint64(modelprofile.RecommendedMaximumDuration.Milliseconds()), ResponseFormat: "json"}
	for _, model := range catalog.Models {
		result.Models = append(result.Models, &sttv1.TranscriptionModelProfile{Model: model.Model, Legacy: model.Legacy,
			ParameterNames: append([]string(nil), model.ParameterNames...), ChunkingStrategies: append([]string(nil), model.ChunkingStrategies...),
			FileStreamSupported: model.FileStreamSupported, StreamEnabled: false, MaximumPromptBytes: model.MaximumPromptBytes,
			MaximumKeywords: model.MaximumKeywords, MaximumKeywordBytes: model.MaximumKeywordBytes, MaximumTemperature: 1})
	}
	return result
}
