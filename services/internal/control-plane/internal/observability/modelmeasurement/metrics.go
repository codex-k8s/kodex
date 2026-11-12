// Package modelmeasurement records bounded post-commit operational counters, not billing or a durable ledger.
// Package modelmeasurement хранит ограниченные post-commit operational counters, а не billing или durable ledger.
package modelmeasurement

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics owns collectors registered once by composition; all labels come from validated closed vocabularies.
// Metrics владеет collectors, однажды зарегистрированными composition; labels происходят из проверенных закрытых словарей.
type Metrics struct {
	observations *prometheus.CounterVec
	tokens       *prometheus.CounterVec
	ttft         *prometheus.HistogramVec
	duration     *prometheus.HistogramVec
}

// New registers process-local counters with the existing private registry; registration errors prevent startup.
// New регистрирует process-local counters в прежнем приватном registry; ошибка регистрации закрывает startup.
func New(register func(...prometheus.Collector) error) (*Metrics, error) {
	metrics := &Metrics{
		observations: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kodex_control_plane_model_measurements_total", Help: "Validated model observations after acknowledged owner commit; incomplete across process crashes."}, []string{"source", "phase", "outcome", "completeness"}),
		tokens:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kodex_control_plane_model_tokens_total", Help: "Native partial token accounting; cache and reasoning categories are subsets and must not be added to input or output."}, []string{"phase", "kind"}),
		ttft:         prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "kodex_control_plane_model_ttft_seconds", Help: "Time from actual HTTP send to first content-bearing delta, when observed.", Buckets: []float64{.01, .05, .1, .25, .5, 1, 2, 5, 10, 30, 60, 120}}, []string{"phase"}),
		duration:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "kodex_control_plane_model_request_duration_seconds", Help: "Actual HTTP attempt duration before observation delivery, when observed.", Buckets: []float64{.1, .5, 1, 5, 10, 30, 60, 120, 300, 900}}, []string{"phase", "outcome"}),
	}
	if err := register(metrics.observations, metrics.tokens, metrics.ttft, metrics.duration); err != nil {
		return nil, err
	}
	return metrics, nil
}

// Observe accepts one newly committed receipt; invalid input is ignored without creating arbitrary label values.
// Acknowledgement loss or process death may omit counters; authoritative run events remain the recovery source.
// Observe принимает одну новую committed квитанцию; неверный input игнорируется без произвольных labels.
// Потеря подтверждения или process death может пропустить counters; authoritative run events остаются источником восстановления.
func (metrics *Metrics) Observe(value runtimecontract.ModelMeasurement) {
	if value.Validate() != nil {
		return
	}
	metrics.observations.WithLabelValues(value.Source, value.Phase, value.Outcome, value.Completeness).Inc()
	if value.Source == "HTTP_REQUEST" {
		if value.DurationMS != nil {
			metrics.duration.WithLabelValues(value.Phase, value.Outcome).Observe(float64(*value.DurationMS) / 1000)
		}
		if value.TTFTMS != nil {
			metrics.ttft.WithLabelValues(value.Phase).Observe(float64(*value.TTFTMS) / 1000)
		}
	}
	if value.Usage != nil && value.Usage.Completeness == runtimecontract.UsagePartial {
		for kind, count := range map[string]int64{"input": value.Usage.InputTokens, "output": value.Usage.OutputTokens, "cache_read": value.Usage.CachedInputTokens, "cache_write": value.Usage.CacheWriteInputTokens, "reasoning": value.Usage.ReasoningOutputTokens} {
			metrics.tokens.WithLabelValues(value.Phase, kind).Add(float64(count))
		}
	}
}
