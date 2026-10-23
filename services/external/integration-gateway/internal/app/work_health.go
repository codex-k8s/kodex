package app

import (
	"sync/atomic"
	"time"
)

// workCycleHealth atomically records the last successful complete owner-work cycle, not merely a successful sidecar probe.
// RU: workCycleHealth атомарно хранит последний успешный полный цикл owner-работы, а не просто успешную проверку sidecar.
type workCycleHealth struct{ completed atomic.Int64 }

// record replaces the timestamp after a successful cycle and clears it on any error, immediately invalidating previous success.
// RU: record заменяет время после успешного цикла и очищает его при любой ошибке, сразу отменяя прежний успешный результат.
func (health *workCycleHealth) record(now time.Time, err error) {
	if err != nil {
		health.completed.Store(0)
		return
	}
	health.completed.Store(now.UnixNano())
}

// ready accepts only an initialized success whose nonnegative age is within the caller's inclusive freshness budget.
// RU: ready принимает только заданный успешный результат с неотрицательным возрастом в пределах включительного бюджета свежести вызывающего кода.
func (health *workCycleHealth) ready(now time.Time, budget time.Duration) bool {
	completed := health.completed.Load()
	if completed <= 0 {
		return false
	}
	age := now.Sub(time.Unix(0, completed))
	return age >= 0 && age <= budget
}

// integrationCycleBudget sums two ordinary operations, capped source/writeback operations and twelve owner-request budgets for a sequential cycle.
// RU: integrationCycleBudget складывает две обычные операции, ограниченные операции источника и записи и двенадцать owner-запросов для последовательного цикла.
func integrationCycleBudget(config Config) time.Duration {
	return 2*config.OperationTimeout + min(config.OperationTimeout, maximumConfigurationSourceOperation) + min(config.OperationTimeout, maximumConfigurationWriteBackOperation) + 12*config.RequestTimeout
}
