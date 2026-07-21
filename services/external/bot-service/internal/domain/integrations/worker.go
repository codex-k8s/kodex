package integrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

const defaultExecutionLease = 30 * time.Second

// WorkerHooks используются только для детерминированного внесения отказов в тестах.
type WorkerHooks struct {
	AfterClaim   func(ExecutionClaim) error
	AfterReceipt func(ExecutionReceipt) error
}

// WorkerConfig задаёт T3–T5 worker.
type WorkerConfig struct {
	Repository Repository
	Executor   RecordingExecutor
	WorkerID   string
	Now        func() time.Time
	Random     io.Reader
	Lease      time.Duration
	Hooks      WorkerHooks
}

// Worker выполняет только PostgreSQL recording executor и не имеет Kubernetes порта.
type Worker struct {
	repository Repository
	executor   RecordingExecutor
	workerID   string
	now        func() time.Time
	random     io.Reader
	lease      time.Duration
	hooks      WorkerHooks
}

func NewWorker(cfg WorkerConfig) *Worker {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	randomSource := cfg.Random
	if randomSource == nil {
		randomSource = rand.Reader
	}
	lease := cfg.Lease
	if lease <= 0 {
		lease = defaultExecutionLease
	}
	return &Worker{repository: cfg.Repository, executor: cfg.Executor, workerID: cfg.WorkerID, now: now, random: randomSource, lease: lease, hooks: cfg.Hooks}
}

// RunOnce обрабатывает максимум один вызов; отсутствие работы не является ошибкой.
func (worker *Worker) RunOnce(ctx context.Context) (bool, error) {
	if worker == nil || worker.repository == nil || worker.executor == nil || worker.workerID == "" {
		return false, ErrNoExecution
	}
	fence, err := worker.randomID("fence")
	if err != nil {
		return false, err
	}
	claim, found, err := worker.repository.ClaimExecution(ctx, worker.workerID, fence, worker.now().UTC(), worker.lease)
	if err != nil || !found {
		return found, err
	}
	if worker.hooks.AfterClaim != nil {
		if err := worker.hooks.AfterClaim(claim); err != nil {
			return true, err
		}
	}
	receipt, err := worker.executor.Execute(ctx, claim)
	if err != nil {
		if errors.Is(err, ErrAuthorizationChanged) {
			cancelErr := worker.repository.CancelExecution(ctx, claim, "execution.authorization_changed", worker.now().UTC())
			return true, errors.Join(err, cancelErr)
		}
		return true, err
	}
	if worker.hooks.AfterReceipt != nil {
		if err := worker.hooks.AfterReceipt(receipt); err != nil {
			return true, err
		}
	}
	if _, err := worker.repository.FinalizeExecution(ctx, claim, worker.now().UTC()); err != nil {
		return true, fmt.Errorf("finalize recording execution: %w", err)
	}
	return true, nil
}

func (worker *Worker) randomID(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := io.ReadFull(worker.random, buffer); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(buffer), nil
}
