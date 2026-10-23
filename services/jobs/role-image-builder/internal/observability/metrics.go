// Package observability owns builder business counters with closed operation/outcome label sets.
// Пакет observability управляет бизнес-счётчиками сборщика с закрытыми наборами меток операции и результата.
package observability

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds the registered builder operation counter; process readiness and runtime telemetry belong to shared libraries.
// Metrics хранит зарегистрированный счётчик операций сборщика; готовность процесса и runtime telemetry принадлежат общим библиотекам.
type Metrics struct{ operations *prometheus.CounterVec }

// New creates one operation counter and registers it through the caller's registry callback, returning registration failure unchanged.
// It starts no collector goroutine or network exporter and relies on the caller to provide a nonnil callback.
// New создаёт один счётчик операций и регистрирует его обработчиком реестра вызывающего, сохраняя ошибку регистрации.
// Goroutine сбора и сетевой экспорт не запускаются; ненулевой обработчик обязан предоставить вызывающий.
func New(register func(...prometheus.Collector) error) (*Metrics, error) {
	operations := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kodex", Subsystem: "role_image_builder", Name: "operations_total",
		Help: "Total number of bounded role image build lifecycle operations.",
	}, []string{"operation", "outcome"})
	if err := register(operations); err != nil {
		return nil, err
	}
	return &Metrics{operations: operations}, nil
}

// Observe increments only an accepted operation/outcome pair and silently drops all unknown labels to bound cardinality.
// It records a local observed effect, not independent proof of owner completion or external delivery.
// Observe увеличивает только допустимую пару операции и результата и молча отбрасывает неизвестные метки, ограничивая кардинальность.
// Записывается локально наблюдаемый эффект, а не независимое доказательство завершения владельца или внешней доставки.
func (metrics *Metrics) Observe(operation, outcome string) {
	if !validOperation(operation) || !validOutcome(outcome) {
		return
	}
	metrics.operations.WithLabelValues(operation, outcome).Inc()
}

// validOperation admits exactly claim, materialize, context, renew, build and complete without normalizing arbitrary input.
// validOperation допускает ровно claim, materialize, context, renew, build и complete без нормализации произвольного входа.
func validOperation(value string) bool {
	switch value {
	case "claim", "materialize", "context", "renew", "build", "complete":
		return true
	default:
		return false
	}
}

// validOutcome admits exactly success, empty, rejected and error, rejecting every other counter label value.
// validOutcome допускает ровно success, empty, rejected и error, отклоняя любое другое значение метки счётчика.
func validOutcome(value string) bool {
	switch value {
	case "success", "empty", "rejected", "error":
		return true
	default:
		return false
	}
}
