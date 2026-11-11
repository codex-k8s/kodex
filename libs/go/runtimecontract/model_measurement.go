package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaximumModelMeasurementBytes bounds secret-free native/transport observations before decoding.
// MaximumModelMeasurementBytes ограничивает несекретные native/transport observations до разбора.
const MaximumModelMeasurementBytes = 4096

// ErrModelMeasurement reports invalid metadata without echoing content, identifiers or headers.
// ErrModelMeasurement обозначает неверные metadata без вывода содержимого, identifiers или headers.
var ErrModelMeasurement = errors.New("model measurement is invalid")

// modelMeasurementRefPattern accepts a bounded observation receipt key, never an execution authority or path.
// modelMeasurementRefPattern допускает ограниченный ключ квитанции наблюдения, который не является execution authority или путём.
var modelMeasurementRefPattern = regexp.MustCompile(`^(request|step)_[A-Za-z0-9_-]{8,80}$`)

// ModelMeasurement separates actual HTTP attempts from native steps; SDK retries need not map one-to-one to steps.
// Owner execution/session/turn/attempt identity is derived from the verified callback, never from this payload.
// Duration and TTFT are optional measured milliseconds; absence stays unknown. Usage is normalized native accounting,
// not a physical cache guarantee. Cost is intentionally absent until a qualified billing source exists.
// ModelMeasurement разделяет фактические HTTP attempts и native steps; SDK retries не обязаны соответствовать шагам один к одному.
// Owner execution/session/turn/attempt выводятся из проверенного callback, а не из этого payload.
// Duration и TTFT содержат необязательные измеренные миллисекунды; отсутствие остаётся неизвестным. Usage означает
// нормализованный native accounting, а не физическую гарантию кеша. Cost отсутствует до квалификации billing source.
type ModelMeasurement struct {
	Ref          string      `json:"ref"`
	Source       string      `json:"source"`
	Phase        string      `json:"phase"`
	Outcome      string      `json:"outcome"`
	Completeness string      `json:"completeness"`
	DurationMS   *int64      `json:"duration_ms,omitempty"`
	TTFTMS       *int64      `json:"ttft_ms,omitempty"`
	Usage        *TokenUsage `json:"usage,omitempty"`
}

// Validate enforces source-specific measurement fields and preserves unknown values instead of inventing zero usage or latency.
// COMPLETED denotes a measurement boundary, not a successful business run or accepted external tool effect.
// Validate проверяет поля каждого источника и сохраняет неизвестные значения вместо выдуманного нулевого usage или latency.
// COMPLETED обозначает границу измерения, а не успешный бизнес-запуск или принятый внешний tool effect.
func (measurement ModelMeasurement) Validate() error {
	if !modelMeasurementRefPattern.MatchString(measurement.Ref) || !containsString([]string{"NORMAL", "COMPACTION", "AUXILIARY"}, measurement.Phase) || !containsString([]string{"COMPLETED", "FAILED", "CANCELLED"}, measurement.Outcome) || !containsString([]string{"COMPLETE", "PARTIAL", "UNKNOWN"}, measurement.Completeness) {
		return ErrModelMeasurement
	}
	for _, duration := range []*int64{measurement.DurationMS, measurement.TTFTMS} {
		if duration != nil && (*duration < 0 || *duration > 86_400_000) {
			return ErrModelMeasurement
		}
	}
	if measurement.TTFTMS != nil && (measurement.DurationMS == nil || *measurement.TTFTMS > *measurement.DurationMS) {
		return ErrModelMeasurement
	}
	switch measurement.Source {
	case "HTTP_REQUEST":
		if !strings.HasPrefix(measurement.Ref, "request_") || measurement.Usage != nil {
			return ErrModelMeasurement
		}
		if measurement.Completeness == "COMPLETE" && (measurement.DurationMS == nil || measurement.TTFTMS == nil || measurement.Outcome != "COMPLETED") {
			return ErrModelMeasurement
		}
		if measurement.Completeness == "UNKNOWN" && (measurement.DurationMS != nil || measurement.TTFTMS != nil) {
			return ErrModelMeasurement
		}
		if measurement.Completeness == "PARTIAL" && measurement.DurationMS == nil {
			return ErrModelMeasurement
		}
	case "NATIVE_STEP":
		if !strings.HasPrefix(measurement.Ref, "step_") || measurement.TTFTMS != nil || measurement.Usage == nil || measurement.Usage.Validate() != nil || measurement.Completeness == "COMPLETE" {
			return ErrModelMeasurement
		}
		for _, count := range []int64{measurement.Usage.InputTokens, measurement.Usage.OutputTokens, measurement.Usage.TotalTokens, measurement.Usage.CachedInputTokens, measurement.Usage.CacheWriteInputTokens, measurement.Usage.ReasoningOutputTokens, measurement.Usage.ModelContextWindow} {
			if count > 1_000_000_000_000 {
				return ErrModelMeasurement
			}
		}
		if measurement.Usage.CachedInputTokens+measurement.Usage.CacheWriteInputTokens > measurement.Usage.InputTokens {
			return ErrModelMeasurement
		}
		// Native accounting cannot prove all provider dimensions; the pinned adapter reports only PARTIAL or UNKNOWN.
		// Native accounting не доказывает все provider dimensions; закреплённый adapter сообщает лишь PARTIAL или UNKNOWN.
		if measurement.Usage.Completeness != UsagePartial && measurement.Usage.Completeness != UsageUnknown {
			return ErrModelMeasurement
		}
		if measurement.Completeness == "PARTIAL" && measurement.Usage.Completeness != UsagePartial {
			return ErrModelMeasurement
		}
		if measurement.Completeness == "UNKNOWN" && (measurement.Usage.Completeness != UsageUnknown || measurement.Usage.TotalTokens != 0) {
			return ErrModelMeasurement
		}
	default:
		return ErrModelMeasurement
	}
	return nil
}

// DecodeModelMeasurement rejects duplicate/unknown keys, trailing documents and oversized observations before semantic use.
// DecodeModelMeasurement отклоняет повторные/неизвестные ключи, лишние документы и oversized observations до смыслового использования.
func DecodeModelMeasurement(raw []byte) (ModelMeasurement, error) {
	// plainMeasurement avoids recursive decoding while preserving the exact public fields.
	// plainMeasurement исключает рекурсивный decode, сохраняя точные публичные поля.
	type plainMeasurement ModelMeasurement
	var measurement plainMeasurement
	if len(raw) == 0 || len(raw) > MaximumModelMeasurementBytes || !utf8.Valid(raw) || boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 5) != nil {
		return ModelMeasurement(measurement), ErrModelMeasurement
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&measurement) != nil || decoder.Decode(new(any)) != io.EOF || ModelMeasurement(measurement).Validate() != nil {
		return ModelMeasurement{}, ErrModelMeasurement
	}
	return ModelMeasurement(measurement), nil
}

// UnmarshalJSON applies the same closed boundary when the observation is nested in a callback or persisted activity.
// UnmarshalJSON применяет ту же закрытую границу для вложенного callback или сохранённой activity.
func (measurement *ModelMeasurement) UnmarshalJSON(raw []byte) error {
	value, err := DecodeModelMeasurement(raw)
	if err != nil {
		return err
	}
	*measurement = value
	return nil
}
