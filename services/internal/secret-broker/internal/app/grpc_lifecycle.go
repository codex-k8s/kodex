package app

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// brokerRPCUnavailable bounds shutdown/admission diagnostics without exposing peer or request details.
	// brokerRPCUnavailable ограничивает shutdown/admission diagnostics без раскрытия peer либо request details.
	brokerRPCUnavailable = "secret broker RPC lifecycle is unavailable"
	// brokerRPCUnjoined marks missing positive handler/transport join and requires process failure with dependencies retained.
	// brokerRPCUnjoined отмечает отсутствие positive handler/transport join и требует process failure с сохранёнными dependencies.
	brokerRPCUnjoined = "secret broker RPC handlers or transport did not join"
	// brokerRPCConcurrency bounds admitted synchronous handlers independently of business grants or remote effects.
	// brokerRPCConcurrency ограничивает admitted synchronous handlers независимо от business grants либо remote effects.
	brokerRPCConcurrency = 128
)

// brokerGRPCComponent owns admission, request cancellation and bounded server shutdown; canceled RPCs or returned Stop alone cannot authorize dependency closure.
// brokerGRPCComponent владеет admission, request cancellation и bounded server shutdown; canceled RPCs либо возврат Stop сами не разрешают dependency closure.
type brokerGRPCComponent struct {
	server           *grpc.Server
	listener         net.Listener
	requests         *serviceruntime.RequestLifecycle
	timeout          time.Duration
	started          atomic.Bool
	serveReturned    atomic.Bool
	stopFinished     atomic.Bool
	forceRequired    atomic.Bool
	gracefulReturned atomic.Bool
	forcedReturned   atomic.Bool
}

// newBrokerGRPCComponent requires an explicit app context and positive finite shutdown budget; it starts no listener or RPC and owns no authority selector.
// newBrokerGRPCComponent требует explicit app context и positive finite shutdown budget, не запускает listener либо RPC и не владеет authority selector.
func newBrokerGRPCComponent(application context.Context, timeout time.Duration) (*brokerGRPCComponent, error) {
	if timeout <= 0 || timeout > time.Minute {
		return nil, errors.New(brokerRPCUnavailable)
	}
	requests, err := serviceruntime.NewRequestLifecycle(application, brokerRPCConcurrency)
	if err != nil {
		return nil, errors.New(brokerRPCUnavailable)
	}
	return &brokerGRPCComponent{requests: requests, timeout: timeout}, nil
}

// unary links the entire synchronous interceptor/handler chain to app-owned cancellation and releases only after its cleanup returns; incoming verified values remain unchanged.
// unary связывает всю synchronous interceptor/handler chain с app-owned cancellation и освобождает activity лишь после её cleanup; incoming verified values сохраняются.
func (component *brokerGRPCComponent) unary(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	linked, release, err := component.requests.Begin(ctx)
	if err != nil {
		code := codes.Unavailable
		if errors.Is(err, serviceruntime.ErrActivityGateLimit) {
			code = codes.ResourceExhausted
		}
		return nil, status.Error(code, brokerRPCUnavailable)
	}
	defer release()
	return handler(linked, request)
}

// GracefulStop records actual completion of the underlying graceful operation for the existing shared shutdown helper; no separate shutdown engine is introduced.
// GracefulStop фиксирует actual completion underlying graceful operation для existing shared shutdown helper без отдельного shutdown engine.
func (component *brokerGRPCComponent) GracefulStop() {
	component.server.GracefulStop()
	component.gracefulReturned.Store(true)
}

// Stop records actual forced-stop return independently of handler release; handlers ignoring cancellation remain unjoined and keep dependencies alive.
// Stop фиксирует actual forced-stop return независимо от handler release; handlers, игнорирующие cancellation, остаются unjoined и удерживают dependencies.
func (component *brokerGRPCComponent) Stop() {
	component.server.Stop()
	component.forcedReturned.Store(true)
}

// worker closes admission before bounded transport shutdown inside the worker itself, before WorkerGroup.Wait; unexpected Serve exit is fatal even when local cleanup succeeds.
// worker закрывает admission перед bounded transport shutdown внутри самого worker до WorkerGroup.Wait; неожиданный Serve exit является fatal также при успешном local cleanup.
func (component *brokerGRPCComponent) worker(base context.Context) serviceruntime.Worker {
	return func(ctx context.Context) error {
		if component == nil || component.server == nil || component.listener == nil || !component.started.CompareAndSwap(false, true) {
			return errors.New(brokerRPCUnavailable)
		}
		done := make(chan struct{})
		go func() {
			_ = component.server.Serve(component.listener)
			component.serveReturned.Store(true)
			close(done)
		}()
		var result error
		select {
		case <-done:
			if ctx.Err() == nil {
				result = errors.New(brokerRPCUnavailable)
			}
		case <-ctx.Done():
		}
		_ = component.requests.StopAdmission()
		graceful, cancelGraceful := context.WithTimeout(context.WithoutCancel(base), component.timeout)
		defer cancelGraceful()
		deadline, _ := graceful.Deadline()
		forced, cancelForced := context.WithDeadline(context.WithoutCancel(base), deadline.Add(component.timeout))
		defer cancelForced()
		stopErr := grpcserver.GracefulStop(graceful, forced, component)
		component.forceRequired.Store(stopErr != nil)
		component.stopFinished.Store(true)
		joinErr := component.requests.Wait(forced)
		select {
		case <-done:
		case <-forced.Done():
		}
		if joinErr != nil || !component.joined() {
			return errors.Join(result, stopErr, errors.New(brokerRPCUnjoined))
		}
		return errors.Join(result, stopErr)
	}
}

// joined requires actual Serve, shutdown-call and handler completion after serving begins; it makes no remote-process or operation-outcome claim.
// joined требует actual Serve, shutdown-call и handler completion после начала serving, не утверждая remote-process join либо operation outcome.
func (component *brokerGRPCComponent) joined() bool {
	return component == nil || !component.started.Load() || (component.stopFinished.Load() && component.serveReturned.Load() && component.gracefulReturned.Load() && (!component.forceRequired.Load() || component.forcedReturned.Load()) && component.requests.Joined())
}

// wait observes local completion already owned by the worker; it never invokes another stop or equates an expired observer budget with join.
// wait наблюдает local completion, которым уже владеет worker, не вызывает повторный stop и не приравнивает expired observer budget к join.
func (component *brokerGRPCComponent) wait(ctx context.Context) error {
	if component.joined() {
		return nil
	}
	if err := component.requests.Wait(ctx); err != nil {
		return errors.Join(err, errors.New(brokerRPCUnjoined))
	}
	if !component.joined() {
		return errors.New(brokerRPCUnjoined)
	}
	return nil
}
