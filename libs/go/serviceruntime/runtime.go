package serviceruntime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Readiness struct {
	ready  atomic.Bool
	reason atomic.Value
}

func NewReadiness() *Readiness {
	readiness := &Readiness{}
	readiness.reason.Store("starting")
	return readiness
}

func (readiness *Readiness) Set(ready bool, reason string) {
	if reason == "" {
		reason = "unspecified"
	}
	readiness.reason.Store(reason)
	readiness.ready.Store(ready)
}

func (readiness *Readiness) Ready() (bool, string) {
	return readiness.ready.Load(), readiness.reason.Load().(string)
}

type Worker func(context.Context) error

type WorkerGroup struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    chan error
	once   sync.Once
}

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

func (group *WorkerGroup) Stop() {
	group.once.Do(group.cancel)
}

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

type ShutdownOperation struct {
	Name    string
	Timeout time.Duration
	Run     func(context.Context) error
}

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
