// Package metrics содержит service-owned метрики STT.
package metrics

import (
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/prometheus/client_golang/prometheus"
)

// Observer owns the STT outcome counter with only approved stage and error-class label values.
// RU: Observer владеет счётчиком исходов STT только с утверждёнными значениями этапа и класса ошибки.
type Observer struct {
	completed *prometheus.CounterVec
}

// New creates an unregistered counter; the composition root registers its Collector explicitly.
// RU: New создаёт незарегистрированный счётчик; корень композиции явно регистрирует его Collector.
func New() *Observer {
	return &Observer{completed: prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kodex", Subsystem: "stt_tts_service", Name: "transcription_stage_total",
		Help: "Total number of transcription outcomes by bounded stage and error class.",
	}, []string{"stage", "error_class"})}
}

// Collector exposes the service-owned counter to the caller's Prometheus registry.
// RU: Collector передаёт счётчик сервиса в реестр Prometheus вызывающей стороны.
func (observer *Observer) Collector() prometheus.Collector { return observer.completed }

// Observe increments one outcome after replacing unrecognized labels with fixed fallback values.
// RU: Observe увеличивает один исход, заменяя неизвестные метки фиксированными запасными значениями.
func (observer *Observer) Observe(stage value.Stage, class value.ErrorClass) {
	observer.completed.WithLabelValues(normalizeStage(stage), normalizeClass(class)).Inc()
}

// normalizeStage confines externally influenced stage values to the approved bounded set.
// RU: normalizeStage ограничивает потенциально внешние значения этапа утверждённым конечным набором.
func normalizeStage(stage value.Stage) string {
	switch stage {
	case value.StageAuthority, value.StagePolicy, value.StageAudio, value.StageCredential,
		value.StageEgress, value.StageProvider, value.StageSuccess:
		return string(stage)
	default:
		return string(value.StageUnknown)
	}
}

// normalizeClass confines error classes to the approved bounded set before metric emission.
// RU: normalizeClass ограничивает классы ошибок утверждённым конечным набором до записи метрики.
func normalizeClass(class value.ErrorClass) string {
	switch class {
	case value.ErrorNone, value.ErrorDenied, value.ErrorInvalid, value.ErrorUnavailable, value.ErrorTimeout, value.ErrorRejected:
		return string(class)
	default:
		return string(value.ErrorUnknown)
	}
}
