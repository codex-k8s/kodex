// Package modelmeasurementwire preserves optional model accounting across the internal protobuf boundary.
// Package modelmeasurementwire сохраняет необязательные измерения модели на внутренней protobuf-границе.
package modelmeasurementwire

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// ToProto allocates an independent projection without inventing absent timings, usage or authority.
// ToProto выделяет независимую проекцию, не выдумывая отсутствующие timings, usage или authority.
func ToProto(value *runtimecontract.ModelMeasurement) *controlplanev1.ModelMeasurement {
	if value == nil {
		return nil
	}
	result := &controlplanev1.ModelMeasurement{Ref: value.Ref, Source: value.Source, Phase: value.Phase, Outcome: value.Outcome, Completeness: value.Completeness}
	if value.DurationMS != nil {
		result.DurationMs = new(*value.DurationMS)
	}
	if value.TTFTMS != nil {
		result.TtftMs = new(*value.TTFTMS)
	}
	if usage := value.Usage; usage != nil {
		result.Usage = &controlplanev1.TokenUsage{Completeness: controlplanev1.UsageCompleteness(usage.Completeness), TotalTokens: usage.TotalTokens, InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CachedInputTokens: usage.CachedInputTokens, CacheWriteInputTokens: usage.CacheWriteInputTokens, ReasoningOutputTokens: usage.ReasoningOutputTokens, ModelContextWindow: usage.ModelContextWindow}
	}
	return result
}

// FromProto copies all optional values; unknown discriminators remain invalid for the canonical validator.
// FromProto копирует все необязательные значения; неизвестные discriminators остаются неверными для канонического validator.
func FromProto(value *controlplanev1.ModelMeasurement) *runtimecontract.ModelMeasurement {
	if value == nil {
		return nil
	}
	result := &runtimecontract.ModelMeasurement{Ref: value.Ref, Source: value.Source, Phase: value.Phase, Outcome: value.Outcome, Completeness: value.Completeness}
	if value.DurationMs != nil {
		result.DurationMS = new(*value.DurationMs)
	}
	if value.TtftMs != nil {
		result.TTFTMS = new(*value.TtftMs)
	}
	if usage := value.Usage; usage != nil {
		result.Usage = &runtimecontract.TokenUsage{Completeness: runtimecontract.UsageCompleteness(usage.Completeness), TotalTokens: usage.TotalTokens, InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CachedInputTokens: usage.CachedInputTokens, CacheWriteInputTokens: usage.CacheWriteInputTokens, ReasoningOutputTokens: usage.ReasoningOutputTokens, ModelContextWindow: usage.ModelContextWindow}
	}
	return result
}
