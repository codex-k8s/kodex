// Package metrics содержит service-owned метрики STT.
package metrics

import (
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/prometheus/client_golang/prometheus"
)

type Observer struct {
	completed *prometheus.CounterVec
}

func New() *Observer {
	return &Observer{completed: prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kodex", Subsystem: "stt_tts_service", Name: "transcription_stage_total",
		Help: "Total number of transcription outcomes by bounded stage and error class.",
	}, []string{"stage", "error_class"})}
}

func (observer *Observer) Collector() prometheus.Collector { return observer.completed }

func (observer *Observer) Observe(stage value.Stage, class value.ErrorClass) {
	observer.completed.WithLabelValues(normalizeStage(stage), normalizeClass(class)).Inc()
}

func normalizeStage(stage value.Stage) string {
	switch stage {
	case value.StageAuthority, value.StagePolicy, value.StageAudio, value.StageCredential,
		value.StageEgress, value.StageProvider, value.StageSuccess:
		return string(stage)
	default:
		return string(value.StageUnknown)
	}
}

func normalizeClass(class value.ErrorClass) string {
	switch class {
	case value.ErrorNone, value.ErrorDenied, value.ErrorInvalid, value.ErrorUnavailable, value.ErrorTimeout, value.ErrorRejected:
		return string(class)
	default:
		return string(value.ErrorUnknown)
	}
}
