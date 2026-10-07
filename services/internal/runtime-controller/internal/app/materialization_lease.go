package app

import (
	"context"
	"errors"
	"sync"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errMaterializationLeaseStopped = errors.New("runtime materialization lease keeper stopped")

// Keeper принадлежит только промежутку claim→tracker. Публикация Pod и renew
// сериализованы: уже полученный отказ не может разрешить новый Pod.
type materializationLeaseKeeper struct {
	runtime *runtime
	input   runtimecontract.RunnerInput
	parent  context.Context
	ctx     context.Context
	cancel  context.CancelCauseFunc
	ready   chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	failure error
}

func (runtime *runtime) keepMaterializationClaims(parent context.Context, executions []*controlplanev1.ClaimedExecution) []*materializationLeaseKeeper {
	keepers := make([]*materializationLeaseKeeper, len(executions))
	for index, execution := range executions {
		lease := execution.GetLease()
		keepers[index] = runtime.keepMaterializationLease(parent, runtimecontract.RunnerInput{
			LeaseRef: lease.GetRef(), LeaseFence: lease.GetFence(), LeaseGeneration: lease.GetGeneration()})
	}
	return keepers
}

func (runtime *runtime) keepMaterializationLease(parent context.Context, input runtimecontract.RunnerInput) *materializationLeaseKeeper {
	ctx, cancel := context.WithCancelCause(parent)
	keeper := &materializationLeaseKeeper{runtime: runtime, input: input, parent: parent,
		ctx: ctx, cancel: cancel, ready: make(chan struct{}), done: make(chan struct{})}
	go keeper.run()
	return keeper
}

func (keeper *materializationLeaseKeeper) run() {
	defer close(keeper.done)
	err := keeper.renew()
	close(keeper.ready)
	if err != nil {
		return
	}
	ticker := time.NewTicker(keeper.runtime.config.LeaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-keeper.ctx.Done():
			return
		case <-ticker.C:
			if keeper.renew() != nil {
				return
			}
		}
	}
}

func (keeper *materializationLeaseKeeper) renew() error {
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	return keeper.renewLocked()
}

func (keeper *materializationLeaseKeeper) renewLocked() error {
	if err := context.Cause(keeper.ctx); err != nil {
		return err
	}
	err := keeper.runtime.renewMaterializationLease(keeper.ctx, keeper.input)
	if err != nil {
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

func (runtime *runtime) renewMaterializationLease(parent context.Context, input runtimecontract.RunnerInput) error {
	if input.LeaseRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 {
		return errors.New("runtime materialization lease binding is invalid")
	}
	request, cancel := context.WithTimeout(parent, runtime.config.RequestTimeout)
	defer cancel()
	_, err := runtime.control.RenewExecution(request, &controlplanev1.RenewExecutionRequest{
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration})
	return err
}

func (keeper *materializationLeaseKeeper) await() error {
	select {
	case <-keeper.ready:
		return context.Cause(keeper.ctx)
	case <-keeper.ctx.Done():
		return context.Cause(keeper.ctx)
	}
}

func (keeper *materializationLeaseKeeper) publish(action func(context.Context) error) error {
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	if err := keeper.renewLocked(); err != nil {
		return err
	}
	publication, cancel := context.WithTimeout(keeper.ctx, keeper.runtime.config.RequestTimeout)
	defer cancel()
	return action(publication)
}

func (keeper *materializationLeaseKeeper) stop() {
	keeper.cancel(errMaterializationLeaseStopped)
	<-keeper.done
}

// После join только caller выполняет последний renew и передаёт владение
// tracker. Старый keeper больше не может продлить lease параллельно tracker.
func (keeper *materializationLeaseKeeper) handoff() error {
	keeper.stop()
	keeper.mu.Lock()
	failure := keeper.failure
	keeper.mu.Unlock()
	if failure != nil {
		return failure
	}
	if cause := context.Cause(keeper.ctx); !errors.Is(cause, errMaterializationLeaseStopped) {
		return cause
	}
	return keeper.runtime.renewMaterializationLease(keeper.parent, keeper.input)
}
