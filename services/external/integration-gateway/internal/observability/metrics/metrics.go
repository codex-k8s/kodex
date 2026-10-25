// Package metrics records integration-worker outcomes with closed label sets and a local protected-work freshness gauge.
// RU: Пакет metrics учитывает результаты integration-worker закрытыми наборами меток и локальным показателем свежести защищённой работы.
package metrics

import (
	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics owns process-registry collectors for complete cycles, provider-operation outcomes and freshness without triggering dependency checks itself.
// RU: Metrics владеет process-registry collectors полных циклов, provider-результатов и свежести, самостоятельно не запуская проверки зависимостей.
type Metrics struct {
	cycles, operations *prometheus.CounterVec
	workPathReady      prometheus.Gauge
}

// New constructs and registers the gateway's fixed collectors, returning registration failure instead of silently omitting business metrics.
// RU: New создаёт и регистрирует фиксированные collectors шлюза, возвращая ошибку регистрации вместо скрытого пропуска бизнесовых метрик.
func New(registry *observability.Metrics) (*Metrics, error) {
	m := &Metrics{
		cycles:        prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: "integration_gateway", Name: "cycles_total", Help: "Total completed integration worker cycles."}, []string{"outcome"}),
		operations:    prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "kodex", Subsystem: "integration_gateway", Name: "operations_total", Help: "Total adapter outcomes before receipt persistence."}, []string{"operation", "outcome"}),
		workPathReady: prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "kodex", Subsystem: "integration_gateway", Name: "work_path_ready", Help: "Whether a fresh complete protected owner work cycle has succeeded."}),
	}
	if err := registry.Register(m.cycles, m.operations, m.workPathReady); err != nil {
		return nil, err
	}
	return m, nil
}

// WorkPathReady publishes the caller's precomputed fresh-owner-cycle boolean as zero or one; it is not an HTTP readiness probe or external health check.
// RU: WorkPathReady публикует заранее вычисленную свежесть owner-цикла как ноль либо один; это не HTTP readiness probe и не внешняя проверка здоровья.
func (m *Metrics) WorkPathReady(ready bool) {
	value := float64(0)
	if ready {
		value = 1
	}
	m.workPathReady.Set(value)
}

// Cycle increments exactly one success/error counter for a completed polling cycle without placing error text in metric labels.
// RU: Cycle увеличивает ровно один success/error счётчик завершённого цикла опроса, не помещая текст ошибки в метки.
func (m *Metrics) Cycle(err error) {
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	m.cycles.WithLabelValues(outcome).Inc()
}

// Operation records a test or execute adapter outcome before owner receipt persistence; unknown overrides success/error when explicitly supplied.
// RU: Operation учитывает test либо execute результат адаптера до сохранения owner-квитанции; явно переданный unknown имеет приоритет над success/error.
func (m *Metrics) Operation(test, success, unknown bool) {
	operation := "execute"
	if test {
		operation = "test"
	}
	outcome := "success"
	if !success {
		outcome = "error"
	}
	if unknown {
		outcome = "unknown"
	}
	m.operations.WithLabelValues(operation, outcome).Inc()
}

// ConfigurationSource records one source-read success/error independently of later owner completion or configuration admission.
// RU: ConfigurationSource учитывает один success/error чтения источника независимо от последующего owner completion и допуска конфигурации.
func (m *Metrics) ConfigurationSource(success bool) {
	outcome := "success"
	if !success {
		outcome = "error"
	}
	m.operations.WithLabelValues("configuration_source", outcome).Inc()
}

// ConfigurationWriteBack records an attempted branch or PR effect as success or unknown, not a final owner proposal state or proven exactly-once delivery.
// RU: ConfigurationWriteBack учитывает попытку branch либо PR эффекта как success или unknown, а не итоговое owner-состояние предложения либо доказанную exactly-once доставку.
func (m *Metrics) ConfigurationWriteBack(branch, success bool) {
	operation := "configuration_writeback_pr"
	if branch {
		operation = "configuration_writeback_branch"
	}
	outcome := "success"
	if !success {
		outcome = "unknown"
	}
	m.operations.WithLabelValues(operation, outcome).Inc()
}

// RepositoryBundle records the single proposal-branch push attempt before owner finalization; an uncertain provider response remains unknown even if a later readback succeeds.
// RepositoryBundle учитывает единственную proposal-branch push attempt до owner finalization; uncertain provider response остаётся unknown, даже если поздний readback успешен.
func (m *Metrics) RepositoryBundle(success bool) {
	outcome := "unknown"
	if success {
		outcome = "success"
	}
	m.operations.WithLabelValues("repository_bundle_branch", outcome).Inc()
}
