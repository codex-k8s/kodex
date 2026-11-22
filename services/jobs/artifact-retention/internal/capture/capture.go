// Package capture consumes independently fenced retained-output tasks in the existing retention job.
// Пакет capture исполняет independently fenced retained-output tasks в existing retention job.
package capture

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

var (
	// ErrUncertain preserves an RPC outcome whose owner commit may already have happened.
	// ErrUncertain сохраняет RPC outcome, owner commit которого мог уже произойти.
	ErrUncertain = errors.New("Kubernetes capture owner outcome is uncertain")
	// ErrMalformed refuses a positive but unbound or unknown owner response before semantic use.
	// ErrMalformed отклоняет positive unbound либо unknown owner response до semantic use.
	ErrMalformed = errors.New("Kubernetes capture owner response is invalid")
	// ErrLostClaim refuses an expired or superseded independently fenced capture attempt.
	// ErrLostClaim отклоняет expired либо superseded independently fenced capture attempt.
	ErrLostClaim = errors.New("Kubernetes capture task claim is lost")
	// ErrNotReady refuses work while the actual producer/storage contract is unavailable.
	// ErrNotReady закрывает работу, пока actual producer/storage contract недоступен.
	ErrNotReady = errors.New("Kubernetes capture producer is not ready")
)

// Claim carries one server-issued immutable task binding and its current short lease.
// Claim передаёт один server-issued immutable task binding и current short lease.
type Claim struct {
	Binding   managedkubernetes.CaptureTaskBinding
	ExpiresAt time.Time
}

// Repository exposes the existing database owner's claim and compatibility heartbeat only.
// Repository предоставляет только claim existing database owner и compatibility heartbeat.
type Repository interface {
	// ClaimCapture returns at most one task after the caller reserved its actual processing slot.
	// ClaimCapture возвращает не более одной task после резервирования actual processing slot.
	ClaimCapture(context.Context, string, int64) (*Claim, error)
	// HeartbeatCapture records only the running compatible consumer, never invocation authority.
	// HeartbeatCapture фиксирует running compatible consumer, не invocation authority.
	HeartbeatCapture(context.Context, string, uint32) error
}

// Owner materializes only the immutable task through the fixed protected control-plane port.
// Owner материализует immutable task через fixed protected control-plane port.
type Owner interface {
	// Check verifies producer compatibility without requiring this worker's heartbeat.
	// Check проверяет producer compatibility без требования heartbeat этого worker.
	Check(context.Context) (uint32, error)
	// Finalize submits metadata once; output bytes and artifact identity are owner-derived.
	// Finalize передаёт metadata один раз; output bytes и artifact identity назначает owner.
	Finalize(context.Context, managedkubernetes.CaptureTaskBinding) (managedkubernetes.CaptureTaskReceipt, error)
	// Read reconciles only the original task after a lost finalization acknowledgement.
	// Read сверяет только original task после lost finalization acknowledgement.
	Read(context.Context, managedkubernetes.CaptureTaskBinding) (managedkubernetes.CaptureTaskReceipt, error)
	// Renew preserves the exact task incarnation and returns only its current lease expiry.
	// Renew сохраняет exact task incarnation и возвращает только current lease expiry.
	Renew(context.Context, managedkubernetes.CaptureTaskBinding) (time.Time, error)
}

// Result counts committed technical capture facts, independently from a tool's business outcome.
// Result считает committed technical capture facts отдельно от business outcome инструмента.
type Result struct{ Complete, Partial, Closed, Uncertain int }

// Processor serializes one reserved task slot before claim and owns every renewal goroutine.
// Processor сериализует один reserved task slot до claim и владеет renewal goroutine.
type Processor struct {
	repository Repository
	owner      Owner
	slots      chan struct{}
	renewEvery time.Duration
}

// NewProcessor binds the existing job/database lifecycle without starting background work.
// NewProcessor связывает existing job/database lifecycle без запуска background work.
func NewProcessor(repository Repository, owner Owner) *Processor {
	return &Processor{repository: repository, owner: owner, slots: make(chan struct{}, 1), renewEvery: 20 * time.Second}
}

// Check proves the current producer and then publishes this live consumer's exact revision.
// Check проверяет current producer и затем публикует exact revision живого consumer.
func (processor *Processor) Check(ctx context.Context, instance string) error {
	if processor == nil || processor.repository == nil || processor.owner == nil {
		return ErrLostClaim
	}
	revision, err := processor.owner.Check(ctx)
	if err != nil {
		return err
	}
	if revision == 0 {
		return ErrNotReady
	}
	if revision != managedkubernetes.CaptureContractRevision {
		return ErrMalformed
	}
	return processor.repository.HeartbeatCapture(ctx, instance, revision)
}

// Process claims only after reserving its local slot, submits one Finalize and uses one metadata
// Read after uncertainty. A positive malformed/refused reply never gets hidden behind recovery.
// Every renewal worker is cancelled and joined before returning or closing shared dependencies.
// Process захватывает task после local slot, отправляет один Finalize и один metadata Read после
// uncertainty. Positive malformed/refused reply не скрывается recovery.
// Каждый renewal worker отменяется и завершается до возврата либо закрытия общих зависимостей.
func (processor *Processor) Process(ctx context.Context, instance string, lease int64) (result Result, resultErr error) {
	if processor == nil || processor.repository == nil || processor.owner == nil || lease < 30 || lease > 300 {
		return result, ErrLostClaim
	}
	select {
	case processor.slots <- struct{}{}:
		defer func() { <-processor.slots }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	claim, err := processor.repository.ClaimCapture(ctx, instance, lease)
	if err != nil {
		return result, err
	}
	if claim == nil {
		return result, nil
	}
	if claim.Binding.Validate() != nil || claim.Binding.WorkloadInstance != instance || !claim.ExpiresAt.After(time.Now()) || claim.ExpiresAt.After(time.Now().Add(301*time.Second)) {
		return result, ErrMalformed
	}
	execution, cancel := context.WithCancel(ctx)
	renewDone := make(chan error, 1)
	go func() {
		err := processor.renew(execution, claim.Binding)
		if err != nil {
			cancel()
		}
		renewDone <- err
	}()
	receipt, callErr := processor.owner.Finalize(execution, claim.Binding)
	cancel()
	renewErr := <-renewDone
	if callErr == nil && receipt.ValidateFor(claim.Binding) != nil {
		callErr = ErrMalformed
	}
	if errors.Is(callErr, ErrUncertain) {
		readback, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		receipt, callErr = processor.owner.Read(readback, claim.Binding)
		stop()
		if callErr == nil && receipt.ValidateFor(claim.Binding) != nil {
			callErr = ErrMalformed
		}
	}
	if callErr != nil {
		result.Uncertain = 1
		return result, errors.Join(callErr, renewErr)
	}
	switch receipt.State {
	case managedkubernetes.CaptureComplete:
		result.Complete = 1
		if receipt.Kind == managedkubernetes.CapturePartial {
			result.Partial = 1
		}
	case managedkubernetes.CaptureRejected, managedkubernetes.CaptureSuperseded, managedkubernetes.CaptureWaitingOwner:
		result.Closed = 1
	default:
		result.Uncertain = 1
		return result, errors.Join(ErrUncertain, renewErr)
	}
	return result, nil
}

// renew keeps only this task alive and ends on cancellation, timeout or a lost owner response.
// renew поддерживает только эту task и заканчивается при cancellation, timeout либо lost response.
func (processor *Processor) renew(ctx context.Context, binding managedkubernetes.CaptureTaskBinding) error {
	ticker := time.NewTicker(processor.renewEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			check, cancel := context.WithTimeout(ctx, 5*time.Second)
			expires, err := processor.owner.Renew(check, binding)
			cancel()
			if ctx.Err() != nil {
				return nil
			}
			if err != nil || !expires.After(time.Now()) {
				return ErrLostClaim
			}
		}
	}
}
