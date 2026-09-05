package app

import (
	"sync/atomic"
	"time"
)

// Успех sidecar не подменяет свежий цикл полного защищённого owner work.
type workCycleHealth struct{ completed atomic.Int64 }

func (health *workCycleHealth) record(now time.Time, err error) {
	if err != nil {
		health.completed.Store(0)
		return
	}
	health.completed.Store(now.UnixNano())
}

func (health *workCycleHealth) ready(now time.Time, budget time.Duration) bool {
	completed := health.completed.Load()
	if completed <= 0 {
		return false
	}
	age := now.Sub(time.Unix(0, completed))
	return age >= 0 && age <= budget
}

func integrationCycleBudget(config Config) time.Duration {
	return 2*config.OperationTimeout + min(config.OperationTimeout, maximumConfigurationSourceOperation) + 6*config.RequestTimeout
}
