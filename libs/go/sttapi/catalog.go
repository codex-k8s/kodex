package sttapi

import (
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ModelCatalog переносит безопасные возможности адаптера без credential data.
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
