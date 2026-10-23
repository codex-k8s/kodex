package app

import (
	"context"
	"errors"
	"sync"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// errMaterializationLeaseStopped identifies intentional keeper shutdown or handoff, distinct from an owner renewal denial.
// The sentinel lets final handoff ignore its own local cancellation without discarding a concurrent owner failure.
// errMaterializationLeaseStopped обозначает намеренную остановку или передачу держателя, отдельно от отказа владельца в продлении.
// Маркер позволяет последней передаче отличить собственную локальную отмену, не теряя одновременную ошибку владельца.
var errMaterializationLeaseStopped = errors.New("runtime materialization lease keeper stopped")

// materializationLeaseKeeper owns renewal only between an owner claim and tracker handoff.
// Its mutex serializes renewal and publication; confirmed expiry, cancellation cause and separately retained failure keep observed denial from authorizing a later workload.
// materializationLeaseKeeper владеет продлением только между заявкой владельца и передачей отслеживанию.
// Мьютекс упорядочивает продление и публикацию; подтверждённый срок, причина отмены и отдельно сохранённая ошибка не позволяют известному отказу разрешить последующую нагрузку.
type materializationLeaseKeeper struct {
	runtime        *runtime
	input          runtimecontract.RunnerInput
	parent         context.Context
	ctx            context.Context
	cancel         context.CancelCauseFunc
	cancelDeadline context.CancelFunc
	ready          chan struct{}
	done           chan struct{}
	mu             sync.Mutex
	failure        error
	confirmedUntil time.Time
}

// keepMaterializationClaims creates one independently running keeper per claimed execution in the original order.
// It copies only owner claim/revision pins for renewal diagnostics, not a complete runnable input; an invalid execution deadline cancels that keeper's parent before renewal.
// keepMaterializationClaims создаёт отдельный работающий держатель для каждого заявленного выполнения в исходном порядке.
// Копируются только привязки заявки/ревизии владельца для диагностики продления, а не полный вход запуска; неверный срок выполнения отменяет родителя этого держателя до продления.
func (runtime *runtime) keepMaterializationClaims(parent context.Context, executions []*controlplanev1.ClaimedExecution) []*materializationLeaseKeeper {
	keepers := make([]*materializationLeaseKeeper, len(executions))
	for index, execution := range executions {
		lease := execution.GetLease()
		revision := execution.GetRevision()
		// Only owner claim pins are copied for diagnostics; no runnable input is constructed here.
		// Только owner claim pins для диагностики; исполняемый input здесь не строится.
		input := runtimecontract.RunnerInput{LeaseRef: lease.GetRef(), LeaseFence: lease.GetFence(), LeaseGeneration: lease.GetGeneration(),
			RunRef: execution.GetRun().GetRef(), NodeRef: execution.GetNode().GetRef(),
			SessionRef: revision.GetSessionRef(), TurnRef: revision.GetTurnRef(), Attempt: revision.GetAttempt(),
			InputDigest: revision.GetInputDigest(), RuntimeRevisionDigest: revision.GetRevisionDigest()}
		deadline, err := workload.RuntimeExecutionDeadlineFromProto(execution.GetRevision().GetExecutionDeadline())
		input.ExecutionDeadline = deadline
		keeperParent := parent
		if err != nil {
			rejected, cancel := context.WithCancelCause(parent)
			cancel(err)
			keeperParent = rejected
		}
		keepers[index] = runtime.keepMaterializationLease(keeperParent, input)
	}
	return keepers
}

// keepMaterializationLease applies the input execution deadline, creates cause-aware cancellation and starts the keeper goroutine.
// It does not wait for initial renewal; callers must await readiness and eventually stop/join or hand off ownership.
// keepMaterializationLease применяет срок выполнения входа, создаёт отмену с причиной и запускает горутину держателя.
// Первое продление не ожидается; потребители должны дождаться готовности и затем остановить/дождаться держателя либо передать владение.
func (runtime *runtime) keepMaterializationLease(parent context.Context, input runtimecontract.RunnerInput) *materializationLeaseKeeper {
	bounded, cancelDeadline := input.BoundExecutionDeadline(parent, 0)
	ctx, cancel := context.WithCancelCause(bounded)
	keeper := &materializationLeaseKeeper{runtime: runtime, input: input, parent: parent,
		ctx: ctx, cancel: cancel, cancelDeadline: cancelDeadline, ready: make(chan struct{}), done: make(chan struct{})}
	go keeper.run()
	return keeper
}

// run performs the initial renewal, signals readiness and maintains periodic renewal plus a confirmed-expiry watchdog.
// Expiry is rechecked under the publication mutex before canceling. Exit stops timers, releases the execution-deadline context and closes done for joiners.
// run выполняет первое продление, сигнализирует готовность и поддерживает периодическое продление и контроль подтверждённого срока.
// Срок повторно проверяется под мьютексом публикации перед отменой. При выходе останавливаются таймеры, освобождается контекст срока выполнения и закрывается done для ожидающих завершения.
func (keeper *materializationLeaseKeeper) run() {
	defer close(keeper.done)
	defer keeper.cancelDeadline()
	err := keeper.renew()
	close(keeper.ready)
	if err != nil {
		return
	}
	ticker := time.NewTicker(keeper.runtime.config.LeaseRenewInterval)
	defer ticker.Stop()
	expiry := time.NewTimer(time.Hour)
	defer expiry.Stop()
	keeper.mu.Lock()
	expired := resetLeaseExpiry(expiry, keeper.confirmedUntil)
	keeper.mu.Unlock()
	for {
		select {
		case <-keeper.ctx.Done():
			return
		case <-expired:
			keeper.mu.Lock()
			// Publication may have renewed the lease while run was waiting for the mutex.
			// publish мог продлить lease, пока run ожидал mutex.
			if !time.Now().Before(keeper.confirmedUntil) {
				keeper.failure = context.DeadlineExceeded
				keeper.cancel(keeper.failure)
				keeper.mu.Unlock()
				return
			}
			expired = resetLeaseExpiry(expiry, keeper.confirmedUntil)
			keeper.mu.Unlock()
		case <-ticker.C:
			if keeper.renew() != nil {
				return
			}
			keeper.mu.Lock()
			expired = resetLeaseExpiry(expiry, keeper.confirmedUntil)
			keeper.mu.Unlock()
		}
	}
}

// renew acquires the shared publication mutex before invoking renewal, preventing a competing publication from passing an observed denial.
// It delegates expiry and failure bookkeeping to renewLocked.
// renew захватывает общий мьютекс публикации перед продлением, не позволяя конкурирующей публикации обойти известный отказ.
// Учёт срока и ошибок передаётся renewLocked.
func (keeper *materializationLeaseKeeper) renew() error {
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	return keeper.renewLocked()
}

// renewLocked requires the caller to hold the mutex, preserves an existing cancellation cause and updates expiry only after verified renewal.
// On failure it cancels the keeper and retains an owner denial even when intentional handoff cancellation happened first; local canceled results do not overwrite that distinction.
// renewLocked требует удержания мьютекса, сохраняет имеющуюся причину отмены и меняет срок только после проверенного продления.
// При ошибке держатель отменяется, а отказ владельца сохраняется, даже если намеренная отмена передачи произошла раньше; локальная ошибка отмены не стирает это различие.
func (keeper *materializationLeaseKeeper) renewLocked() error {
	if err := context.Cause(keeper.ctx); err != nil {
		return err
	}
	until, err := keeper.runtime.renewLease(keeper.ctx, keeper.input, renewalPhaseMaterialization, keeper.confirmedUntil)
	if err == nil {
		keeper.confirmedUntil = until
	}
	if err != nil {
		// Handoff cancellation can interrupt the RPC; a real owner denial must not
		// be lost behind the first local cancellation cause.
		// Отмена handoff может прервать RPC; настоящий ответ владельца об
		// отказе нельзя потерять за первым локальным cancellation cause.
		stopped := errors.Is(context.Cause(keeper.ctx), errMaterializationLeaseStopped)
		if !stopped || !errors.Is(err, context.Canceled) && status.Code(err) != codes.Canceled {
			keeper.failure = err
		}
		keeper.cancel(err)
	}
	return err
}

// await waits for initial renewal readiness or keeper cancellation and returns the current cancellation cause.
// It does not transfer ownership or independently extend the confirmed lease.
// await ожидает готовности первого продления либо отмены держателя и возвращает текущую причину отмены.
// Владение не передаётся, подтверждённая аренда самостоятельно не продлевается.
func (keeper *materializationLeaseKeeper) await() error {
	select {
	case <-keeper.ready:
		return context.Cause(keeper.ctx)
	case <-keeper.ctx.Done():
		return context.Cause(keeper.ctx)
	}
}

// publish holds the renewal mutex, requires a fresh successful owner renewal and invokes the action with a lease/request-bounded context.
// It returns the action error without retry or rollback; the action must honor context cancellation.
// publish удерживает мьютекс продления, требует свежего успешного продления у владельца и вызывает действие с контекстом, ограниченным арендой и бюджетом запроса.
// Ошибка действия возвращается без повтора или отката; действие должно соблюдать отмену контекста.
func (keeper *materializationLeaseKeeper) publish(action func(context.Context) error) error {
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	if err := keeper.renewLocked(); err != nil {
		return err
	}
	publication, cancel := leaseBoundedRequest(keeper.ctx, keeper.runtime.config.RequestTimeout, keeper.confirmedUntil)
	defer cancel()
	return action(publication)
}

// stop marks intentional keeper cancellation and waits for its renewal goroutine to close done.
// It does not itself revoke the owner lease, close service dependencies or join a separately executing publication action.
// stop помечает намеренную отмену держателя и ожидает закрытия done его горутиной продления.
// Сам метод не отзывает аренду у владельца, не закрывает зависимости сервиса и не ожидает отдельно выполняемое действие публикации.
func (keeper *materializationLeaseKeeper) stop() {
	keeper.cancel(errMaterializationLeaseStopped)
	<-keeper.done
}

// handoff stops and joins the old renewal goroutine, rejects retained failures or unrelated cancellation and performs one final caller-owned renewal.
// Only a verified success updates confirmed expiry; the caller then transfers tracking responsibility without a competing old keeper.
// handoff останавливает старую горутину продления и дожидается её, отклоняет сохранённые ошибки или постороннюю отмену и выполняет последнее продление под ответственностью потребителя.
// Только проверенный успех меняет подтверждённый срок; затем потребитель передаёт ответственность отслеживанию без конкуренции старого держателя.
func (keeper *materializationLeaseKeeper) handoff() error {
	keeper.stop()
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	failure := keeper.failure
	if failure != nil {
		return failure
	}
	if cause := context.Cause(keeper.ctx); !errors.Is(cause, errMaterializationLeaseStopped) {
		return cause
	}
	until, err := keeper.runtime.renewLease(keeper.parent, keeper.input, renewalPhaseMaterialization, keeper.confirmedUntil)
	if err == nil {
		keeper.confirmedUntil = until
	}
	return err
}
