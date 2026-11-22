package serviceruntime

import "context"

// workerCancellationOnly recognizes a complete bounded unwrap tree of ordinary context cancellation; Is hooks and a shared joined cause cannot hide another failure.
// workerCancellationOnly распознаёт полный bounded unwrap tree ordinary context cancellation; Is hooks и общая joined cause не скрывают другую ошибку.
func workerCancellationOnly(err error, budget int) bool { return visitWorkerCancellation(err, &budget) }

// visitWorkerCancellation spends one shared node budget across every branch, so cycles and wide joins cannot expand work beyond the fixed bound.
// visitWorkerCancellation расходует общий node budget для всех branches: cycles и wide joins не расширяют работу за fixed bound.
func visitWorkerCancellation(err error, remaining *int) bool {
	if *remaining <= 0 {
		return false
	}
	*remaining--
	if err == context.Canceled {
		return true
	}
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 || len(causes) > *remaining {
			return false
		}
		for _, cause := range causes {
			if !visitWorkerCancellation(cause, remaining) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return visitWorkerCancellation(wrapped.Unwrap(), remaining)
	}
	return false
}
