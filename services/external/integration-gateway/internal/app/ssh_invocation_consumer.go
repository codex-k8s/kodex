package app

import (
	"context"
	"errors"
	"sync"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
)

// sshInvocationActivities binds domain reservations to the shared ActivityGate; it owns no Claim loop or remote process-quiescence proof.
// sshInvocationActivities связывает domain reservations с shared ActivityGate; Claim loop и remote process-quiescence proof отсутствуют.
type sshInvocationActivities struct {
	mutex    sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	gate     *serviceruntime.ActivityGate
	consumer *sshIdentityConsumer
	slots    map[*sshInvocationSlot]bool
	failed   bool
	failure  chan struct{}
}

// sshInvocationSlot retains one pre-claim reservation until unused release or the sole adopted finite activity and its cleanup actually join.
// sshInvocationSlot сохраняет pre-claim reservation до unused release либо actual join единственной adopted finite activity и cleanup.
type sshInvocationSlot struct {
	activities        *sshInvocationActivities
	release           func()
	adopted, released bool
}

// newSSHExecutionConsumer composes only the actual fixed-purpose adapter and broker on an application-owned lifecycle; trusted mixed-factory readiness must precede exposing its capacity.
// newSSHExecutionConsumer связывает лишь actual fixed-purpose adapter и broker с application-owned lifecycle; trusted mixed-factory readiness обязана предшествовать выдаче capacity.
func newSSHExecutionConsumer(lifecycle context.Context, instance string, capacity int, adapter *integration.Adapter, broker sb.ManagedSSHServiceClient) (*sshIdentityConsumer, error) {
	if lifecycle == nil || lifecycle.Err() != nil || !managedssh.OwnerReference(instance) || capacity < 1 || capacity > 4 || adapter == nil || broker == nil {
		return nil, brokerIdentityWorkUnavailable
	}
	gate, err := serviceruntime.NewActivityGate(capacity)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(lifecycle)
	consumer := &sshIdentityConsumer{instance: instance, adapter: adapter, broker: broker}
	consumer.activities = &sshInvocationActivities{ctx: ctx, cancel: cancel, gate: gate, consumer: consumer, slots: make(map[*sshInvocationSlot]bool), failure: make(chan struct{})}
	return consumer, nil
}

// reserve allocates a real local slot before the existing Runtime Claim; failure, cancellation and closure reject both new reservations and later adoption.
// reserve выделяет real local slot до existing Runtime Claim; failure, cancellation и closure отклоняют новые reservations и subsequent adoption.
func (activities *sshInvocationActivities) reserve() (brokerIdentityClaimSlot, bool) {
	activities.mutex.Lock()
	defer activities.mutex.Unlock()
	if activities.failed || activities.ctx.Err() != nil || activities.consumer.closed.Load() {
		return nil, false
	}
	release, err := activities.gate.Enter()
	if err != nil {
		return nil, false
	}
	slot := &sshInvocationSlot{activities: activities, release: release}
	activities.slots[slot] = true
	return slot, true
}

// ExecuteClaim validates an owned exact claim before atomic adoption, then returns RUNNING without borrowing the caller's cycle context for ongoing work.
// ExecuteClaim проверяет owned exact claim до atomic adoption, затем возвращает RUNNING без использования caller cycle context для ongoing work.
func (slot *sshInvocationSlot) ExecuteClaim(ctx context.Context, claim *cp.IntegrationInvocationClaim) (brokerIdentityInvocationProgress, error) {
	if ctx == nil || ctx.Err() != nil {
		slot.ReleaseUnused()
		return brokerIdentityInvocationProgress{}, brokerIdentityWorkUnavailable
	}
	consumer := slot.activities.consumer
	invocation, err := consumer.adapter.PrepareSSHInvocation(claim, consumer.instance)
	if err != nil {
		slot.ReleaseUnused()
		return brokerIdentityInvocationProgress{}, err
	}
	activities := slot.activities
	activities.mutex.Lock()
	defer activities.mutex.Unlock()
	if slot.released || slot.adopted || activities.failed || activities.ctx.Err() != nil || consumer.closed.Load() || ctx.Err() != nil || !invocation.ExpiresAt().After(time.Now()) {
		if !slot.released && !slot.adopted {
			slot.releaseLocked()
		}
		return brokerIdentityInvocationProgress{}, brokerIdentityWorkUnavailable
	}
	slot.adopted = true
	binding := invocation.Binding()
	go slot.execute(invocation)
	return brokerIdentityInvocationProgress{InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, InputDigest: binding.InputDigest, Generation: binding.Generation, State: "RUNNING", RemoteOutcome: "NOT_STARTED"}, nil
}

// ReleaseUnused never releases or cancels adopted work, including when the handoff response is lost.
// ReleaseUnused не освобождает и не отменяет adopted work, включая lost handoff response.
func (slot *sshInvocationSlot) ReleaseUnused() {
	slot.activities.mutex.Lock()
	defer slot.activities.mutex.Unlock()
	if !slot.released && !slot.adopted {
		slot.releaseLocked()
	}
}

// releaseLocked retires one local reservation while holding its domain lock; it conveys no remote writer-stop proof.
// releaseLocked удаляет одну local reservation под domain lock; remote writer-stop proof не выдаётся.
func (slot *sshInvocationSlot) releaseLocked() {
	slot.released = true
	delete(slot.activities.slots, slot)
	slot.release()
}

// execute keeps the slot through actual unary and reconciliation join, latching unresolved lifecycle failure before releasing local capacity.
// execute сохраняет slot через actual unary и reconciliation join, фиксируя unresolved lifecycle failure до освобождения local capacity.
func (slot *sshInvocationSlot) execute(invocation *integration.SSHInvocation) {
	activities := slot.activities
	err := activities.run(invocation)
	activities.mutex.Lock()
	defer activities.mutex.Unlock()
	if err != nil && !activities.failed {
		activities.failed = true
		close(activities.failure)
		activities.cancel()
	}
	slot.releaseLocked()
}

// sshExecutionTerminal accepts only authoritative closed owner metadata after every local call joins; it never promotes an unrecorded success or declares remote process quiescence.
// sshExecutionTerminal допускает лишь authoritative closed owner metadata после join всех local calls; unrecorded success и remote process quiescence не утверждаются.
func sshExecutionTerminal(receipt managedssh.ExecutionReceipt) bool {
	if receipt.Validate(time.Now(), true) != nil {
		return false
	}
	if receipt.OwnerState == "SUCCEEDED" {
		return receipt.Recorded && receipt.Applied && receipt.State == "SUCCEEDED"
	}
	return receipt.OwnerState == "FAILED" || receipt.OwnerState == "CANCELLED" || receipt.OwnerState == "UNKNOWN_OUTCOME"
}

// run invokes Execute once, then only bounded metadata reconciliation; malformed successful replies stay fatal and never gain an automatic readback bypass.
// run вызывает Execute один раз, затем лишь bounded metadata reconciliation; malformed successful replies остаются fatal без automatic readback bypass.
func (activities *sshInvocationActivities) run(invocation *integration.SSHInvocation) error {
	result, err := invocation.Execute(activities.ctx, activities.consumer.broker)
	defer func() { clear(result.Receipt.Observation.Result) }()
	if err == nil && sshExecutionTerminal(result.Receipt) {
		return nil
	}
	var safe *integration.SafeError
	if errors.As(err, &safe) && safe.Code == "INTEGRATION_RESPONSE_INVALID" {
		return brokerIdentityWorkUnavailable
	}
	cleanup, done := context.WithTimeout(context.WithoutCancel(activities.ctx), 15*time.Second)
	defer done()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		read, cancel := context.WithTimeout(cleanup, 2*time.Second)
		observed, readErr := invocation.Reconcile(read, activities.consumer.broker)
		cancel()
		if readErr == nil && sshExecutionTerminal(observed.Receipt) {
			return nil
		}
		if errors.As(readErr, &safe) && safe.Code == "INTEGRATION_RESPONSE_INVALID" {
			return brokerIdentityWorkUnavailable
		}
		select {
		case <-cleanup.Done():
			return brokerIdentityWorkUnavailable
		case <-ticker.C:
		}
	}
}

// close denies new admission and cancels activities while leaving adopted slots held until every local RPC and cleanup returns.
// close запрещает новую admission и отменяет activities, сохраняя adopted slots до возврата каждого local RPC и cleanup.
func (activities *sshInvocationActivities) close() error {
	activities.mutex.Lock()
	defer activities.mutex.Unlock()
	err := activities.gate.Close()
	activities.cancel()
	for slot := range activities.slots {
		if !slot.adopted {
			slot.releaseLocked()
		}
	}
	return err
}

// wait preserves joined failure separately from timeout; neither a cancelled context nor a fatal result proves join.
// wait сохраняет joined failure отдельно от timeout; cancelled context и fatal result сами не доказывают join.
func (activities *sshInvocationActivities) wait(ctx context.Context) error {
	if err := activities.gate.Wait(ctx); err != nil {
		return err
	}
	activities.mutex.Lock()
	defer activities.mutex.Unlock()
	if activities.failed {
		return brokerIdentityWorkUnavailable
	}
	return nil
}
