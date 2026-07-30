package serviceruntime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Readiness потокобезопасно хранит статус и ограниченную причину.
type Readiness struct {
	ready  atomic.Bool
	reason atomic.Value
}

// NewReadiness создаёт неготовое начальное состояние.
func NewReadiness() *Readiness {
	readiness := &Readiness{}
	readiness.reason.Store("starting")
	return readiness
}

// Set атомарно обновляет готовность и причину.
func (readiness *Readiness) Set(ready bool, reason string) {
	if reason == "" {
		reason = "unspecified"
	}
	readiness.reason.Store(reason)
	readiness.ready.Store(ready)
}

// Ready возвращает согласованный снимок готовности.
func (readiness *Readiness) Ready() (bool, string) {
	return readiness.ready.Load(), readiness.reason.Load().(string)
}

// Worker выполняет фоновую работу до отмены контекста.
type Worker func(context.Context) error

// WorkerGroup владеет отменой и объединением результатов workers.
type WorkerGroup struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    chan error
	once   sync.Once
}

// StartWorkers запускает workers под общим контекстом и cancel/join boundary.
func StartWorkers(parent context.Context, workers ...Worker) *WorkerGroup {
	ctx, cancel := context.WithCancel(parent)
	group := &WorkerGroup{
		cancel: cancel,
		done:   make(chan struct{}),
		err:    make(chan error, len(workers)),
	}
	var wait sync.WaitGroup
	wait.Add(len(workers))
	for _, worker := range workers {
		go func(workerValue Worker) {
			defer wait.Done()
			if err := workerValue(ctx); err != nil && !errors.Is(err, context.Canceled) {
				group.err <- err
				cancel()
			}
		}(worker)
	}
	go func() {
		wait.Wait()
		close(group.err)
		close(group.done)
	}()
	return group
}

// Stop отменяет workers ровно один раз.
func (group *WorkerGroup) Stop() {
	group.once.Do(group.cancel)
}

// Wait ожидает завершение и объединяет ошибки workers.
func (group *WorkerGroup) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait workers: %w", ctx.Err())
	case <-group.done:
		var joined error
		for err := range group.err {
			joined = errors.Join(joined, err)
		}
		return joined
	}
}

// ShutdownOperation задаёт независимую cleanup-операцию и её бюджет.
type ShutdownOperation struct {
	Name    string
	Timeout time.Duration
	Run     func(context.Context) error
}

// RunShutdown последовательно выполняет cleanup с независимыми контекстами.
func RunShutdown(
	background context.Context,
	operations ...ShutdownOperation,
) error {
	var joined error
	for _, operation := range operations {
		if operation.Timeout <= 0 {
			joined = errors.Join(joined, fmt.Errorf("%s: invalid shutdown timeout", operation.Name))
			continue
		}
		ctx, cancel := context.WithTimeout(background, operation.Timeout)
		err := operation.Run(ctx)
		cancel()
		if err != nil {
			joined = errors.Join(joined, fmt.Errorf("%s: %w", operation.Name, err))
		}
	}
	return joined
}
