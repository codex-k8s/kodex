package app

import (
	"context"
	"errors"
	"sync"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
)

// errKubernetesActivity exposes local lifecycle failure without owner refs, fences or remote diagnostics.
// errKubernetesActivity сообщает об ошибке локального жизненного цикла без owner refs, fences либо внешней диагностики.
var errKubernetesActivity = errors.New("managed Kubernetes activity failed")

// kubernetesActivityInput owns copied operation pins and the original lease deadline; renewal must retain the exact binding.
// kubernetesActivityInput владеет копией параметров операции и исходным сроком lease; продление сохраняет точный binding.
type kubernetesActivityInput struct {
	request    integration.Request
	binding    *sb.ManagedKubernetesInvocationBinding
	leaseUntil time.Time
}

// kubernetesActivityRunner returns only after all local work and cleanup joined. Nil additionally requires verified outer terminal state, either recorded completion or exact closed metadata; it never implies business success. Non-nil signals unresolved lifecycle, not ordinary remote rejection or owner cancellation.
// kubernetesActivityRunner возвращается лишь после join всей локальной работы и очистки. Nil дополнительно требует подтверждённого outer terminal state: recorded completion либо exact closed metadata; business success не подразумевается. Non-nil означает unresolved lifecycle, а не обычный внешний отказ либо owner cancellation.
type kubernetesActivityRunner func(context.Context, kubernetesActivityInput) error

// kubernetesConsumer composes purpose health checks and bounded application-owned activities, without running a second Claim loop or owning remote authority.
// kubernetesConsumer собирает purpose health checks и ограниченные операции приложения без второго цикла Claim либо владения внешними полномочиями.
type kubernetesConsumer struct {
	mutex    sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	gate     *serviceruntime.ActivityGate
	slots    map[*kubernetesClaimSlot]bool
	closed   bool
	failed   bool
	failure  chan struct{}
	instance string
	adapter  *integration.Adapter
	broker   sb.ManagedKubernetesServiceClient
	run      kubernetesActivityRunner
}

// kubernetesClaimSlot is one reservation whose state is protected by its consumer mutex; an adopted activity alone owns its release.
// kubernetesClaimSlot является одной резервацией под mutex потребителя; после принятия задания освобождением владеет только сама операция.
type kubernetesClaimSlot struct {
	consumer *kubernetesConsumer
	release  func()
	adopted  bool
	released bool
}

// newKubernetesConsumer requires executable dependencies and finite local capacity before any reservation; the lifecycle context belongs to the application.
// newKubernetesConsumer требует исполняемые зависимости и конечную ёмкость до резервирования; lifecycle context принадлежит приложению.
func newKubernetesConsumer(lifecycle context.Context, instance string, capacity int, adapter *integration.Adapter, broker sb.ManagedKubernetesServiceClient, run kubernetesActivityRunner) (*kubernetesConsumer, error) {
	if run == nil {
		return nil, errKubernetesActivity
	}
	return newKubernetesPurposeConsumer(lifecycle, instance, capacity, adapter, broker, run)
}

// newKubernetesPurposeConsumer builds the shared bounded lifecycle; nil runner is reserved for the health-only constructor and never advertises invocation capacity.
// newKubernetesPurposeConsumer создаёт общий bounded lifecycle; nil runner допускает лишь health-only constructor и не объявляет invocation capacity.
func newKubernetesPurposeConsumer(lifecycle context.Context, instance string, capacity int, adapter *integration.Adapter, broker sb.ManagedKubernetesServiceClient, run kubernetesActivityRunner) (*kubernetesConsumer, error) {
	if lifecycle == nil || lifecycle.Err() != nil || !managedkubernetes.ValidLocator(instance) || capacity < 1 || capacity > 4 || adapter == nil || broker == nil {
		return nil, errKubernetesActivity
	}
	gate, err := serviceruntime.NewActivityGate(capacity)
	if err != nil {
		return nil, errKubernetesActivity
	}
	ctx, cancel := context.WithCancel(lifecycle)
	return &kubernetesConsumer{ctx: ctx, cancel: cancel, gate: gate, slots: make(map[*kubernetesClaimSlot]bool), failure: make(chan struct{}), instance: instance, adapter: adapter, broker: broker, run: run}, nil
}

// ReserveClaim reserves real local capacity before the common owner RPC; cancellation or overload advertises no Kubernetes capacity.
// ReserveClaim резервирует фактическую локальную ёмкость до общего owner RPC; отмена либо перегрузка исключает Kubernetes из доступной ёмкости.
func (consumer *kubernetesConsumer) ReserveClaim() (brokerIdentityClaimSlot, bool) {
	consumer.mutex.Lock()
	defer consumer.mutex.Unlock()
	if consumer.run == nil || consumer.closed || consumer.failed || consumer.ctx.Err() != nil {
		return nil, false
	}
	release, err := consumer.gate.Enter()
	if err != nil {
		return nil, false
	}
	slot := &kubernetesClaimSlot{consumer: consumer, release: release}
	consumer.slots[slot] = true
	return slot, true
}

// ExecuteClaim adopts at most once and copies the claim before starting work; the caller context controls handoff only and cannot abandon adopted work.
// ExecuteClaim принимает задание не более одного раза и копирует его до запуска; caller context управляет только передачей и не бросает принятую работу.
func (slot *kubernetesClaimSlot) ExecuteClaim(ctx context.Context, claim *cp.IntegrationInvocationClaim) (brokerIdentityInvocationProgress, error) {
	var empty brokerIdentityInvocationProgress
	if ctx == nil || ctx.Err() != nil {
		slot.ReleaseUnused()
		return empty, errKubernetesActivity
	}
	request, binding, err := integration.PrepareKubernetesInvocation(claim, slot.consumer.instance)
	if err != nil {
		slot.ReleaseUnused()
		return empty, err
	}
	input := kubernetesActivityInput{request: request, binding: binding, leaseUntil: claim.GetLease().GetExpiresAt().AsTime()}
	consumer := slot.consumer
	consumer.mutex.Lock()
	defer consumer.mutex.Unlock()
	if slot.released || slot.adopted || consumer.closed || consumer.failed || consumer.ctx.Err() != nil || ctx.Err() != nil || !input.leaseUntil.After(time.Now()) {
		if !slot.released && !slot.adopted {
			slot.releaseLocked()
		}
		return empty, errKubernetesActivity
	}
	slot.adopted = true
	progress := brokerIdentityInvocationProgress{InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, InputDigest: binding.InputDigest, Generation: binding.Generation, State: "RUNNING", RemoteOutcome: "NOT_STARTED"}
	go slot.execute(input)
	return progress, nil
}

// execute retains the slot through all runner cleanup and records failure without retaining potentially sensitive RPC error values.
// execute сохраняет slot до завершения всей очистки runner и фиксирует ошибку без хранения потенциально чувствительных RPC error values.
func (slot *kubernetesClaimSlot) execute(input kubernetesActivityInput) {
	err := slot.consumer.run(slot.consumer.ctx, input)
	slot.consumer.mutex.Lock()
	defer slot.consumer.mutex.Unlock()
	if err != nil && !slot.consumer.failed {
		slot.consumer.failed = true
		close(slot.consumer.failure)
	}
	slot.releaseLocked()
}

// ReleaseUnused is idempotent and deliberately does nothing after adoption, including a lost handoff acknowledgement.
// ReleaseUnused идемпотентен и намеренно ничего не делает после принятия задания, включая потерю подтверждения передачи.
func (slot *kubernetesClaimSlot) ReleaseUnused() {
	slot.consumer.mutex.Lock()
	defer slot.consumer.mutex.Unlock()
	if !slot.adopted && !slot.released {
		slot.releaseLocked()
	}
}

// releaseLocked retires exactly one local reservation while the consumer lock is held; it provides no proof of remote writer quiescence.
// releaseLocked удаляет ровно одну локальную резервацию под consumer lock; остановку внешнего writer не доказывает.
func (slot *kubernetesClaimSlot) releaseLocked() {
	slot.released = true
	delete(slot.consumer.slots, slot)
	slot.release()
}

// Close closes admission, releases only unused reservations and requests cancellation; adopted slots remain held until actual runner return.
// Close закрывает приём, освобождает только неиспользованные резервации и запрашивает отмену; принятые slots остаются занятыми до фактического возврата runner.
func (consumer *kubernetesConsumer) Close() error {
	consumer.mutex.Lock()
	defer consumer.mutex.Unlock()
	consumer.closed = true
	if err := consumer.gate.Close(); err != nil {
		return errKubernetesActivity
	}
	consumer.cancel()
	for slot := range consumer.slots {
		if !slot.adopted {
			slot.releaseLocked()
		}
	}
	return nil
}

// Wait requires closed admission and actual release of all activities; a timeout is never join, while a joined failure still remains a failure.
// Wait требует закрытого приёма и фактического освобождения всех операций; timeout не означает join, а ошибка завершившейся операции остаётся ошибкой.
func (consumer *kubernetesConsumer) Wait(ctx context.Context) error {
	if err := consumer.gate.Wait(ctx); err != nil {
		return err
	}
	consumer.mutex.Lock()
	defer consumer.mutex.Unlock()
	if consumer.failed {
		return errKubernetesActivity
	}
	return nil
}

// Joined exposes local claim activity join separately from its outcome; synchronous health calls remain owned by the common worker join.
// Joined раскрывает локальный join операций отдельно от их исхода; синхронными health calls по-прежнему владеет join общего worker.
func (consumer *kubernetesConsumer) Joined() bool { return consumer.gate.Joined() }

// Failure closes once on unresolved activity failure so an application-owned monitor can stop workers immediately; the signal contains no raw error and is not a join receipt.
// Failure закрывается однократно при unresolved activity failure, позволяя app-owned monitor немедленно остановить workers; сигнал не содержит raw error и не является join receipt.
func (consumer *kubernetesConsumer) Failure() <-chan struct{} { return consumer.failure }

// ProbePersonal preserves the purpose owner's personal health boundary; the common worker owns this synchronous call's cancellation and join.
// ProbePersonal сохраняет personal health boundary владельца; отменой и join этого синхронного вызова владеет общий worker.
func (consumer *kubernetesConsumer) ProbePersonal(ctx context.Context, claim *cp.IntegrationConnectionTestClaim) (brokerIdentityProbeReceipt, error) {
	return consumer.probe(ctx, claim)
}

// ProbeShared uses the same owner validation, which resolves service identity rather than accepting a caller-selected personal credential.
// ProbeShared использует ту же проверку владельца, которая разрешает service identity вместо выбранного вызывающим personal credential.
func (consumer *kubernetesConsumer) ProbeShared(ctx context.Context, claim *cp.IntegrationConnectionTestClaim) (brokerIdentityProbeReceipt, error) {
	return consumer.probe(ctx, claim)
}

// probe projects only a validated purpose health receipt; authentication alone is never resource authorization.
// probe выдаёт только проверенную purpose health receipt; authentication сама по себе не является resource authorization.
func (consumer *kubernetesConsumer) probe(ctx context.Context, claim *cp.IntegrationConnectionTestClaim) (brokerIdentityProbeReceipt, error) {
	select {
	case <-consumer.failure:
		return brokerIdentityProbeReceipt{}, errKubernetesActivity
	default:
	}
	if ctx == nil || consumer.ctx.Err() != nil || consumer.gate.Closed() {
		return brokerIdentityProbeReceipt{}, errKubernetesActivity
	}
	receipt, err := consumer.adapter.ProbeKubernetes(ctx, claim, consumer.instance, consumer.broker)
	result := brokerIdentityProbeReceipt{TestRef: receipt.TestRef, Attempted: receipt.Attempted, Applied: receipt.Completed, Authenticated: receipt.Authenticated}
	if receipt.Completed {
		result.State = "FAILED"
		if receipt.Authenticated {
			result.State = "SUCCEEDED"
		}
	}
	return result, err
}
