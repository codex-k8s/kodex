package app

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"google.golang.org/grpc"
)

// controlPlaneRPCUnjoined identifies absent handler or transport completion without exposing
// request content. Cancellation or a returned Serve worker never establishes positive join.
// controlPlaneRPCUnjoined обозначает отсутствие handler либо transport completion без request
// content. Cancellation либо возврат Serve worker не устанавливает positive join.
const controlPlaneRPCUnjoined = "control-plane RPC handlers or transport did not join"

// controlPlaneGRPCLifecycle records completion of the existing server's Serve/graceful/forced
// operations. The server must use WaitForHandlers(true); no task, retry or authority is owned here.
// controlPlaneGRPCLifecycle фиксирует completion existing server Serve/graceful/forced operations.
// Server использует WaitForHandlers(true); task, retry либо authority здесь не принадлежат.
type controlPlaneGRPCLifecycle struct {
	server           *grpc.Server
	listener         net.Listener
	serveDone        chan struct{}
	gracefulDone     chan struct{}
	forcedDone       chan struct{}
	gracefulOnce     sync.Once
	forcedOnce       sync.Once
	started          atomic.Bool
	forceRequired    atomic.Bool
	shutdownReturned atomic.Bool
}

// newControlPlaneGRPCLifecycle binds the already constructed server/listener without launching
// goroutines. Dependency closure remains forbidden until all required calls actually return.
// newControlPlaneGRPCLifecycle связывает готовые server/listener без запуска goroutines.
// Dependency closure запрещено до actual return всех required calls.
func newControlPlaneGRPCLifecycle(server *grpc.Server, listener net.Listener) *controlPlaneGRPCLifecycle {
	return &controlPlaneGRPCLifecycle{server: server, listener: listener, serveDone: make(chan struct{}), gracefulDone: make(chan struct{}), forcedDone: make(chan struct{})}
}

// serve waits for cancellation or Serve return, retaining the independent completion channel
// for the composition root. Unexpected serving failure still cancels the worker group.
// serve ожидает cancellation либо Serve return, сохраняя independent completion channel
// для composition root. Неожиданный serving failure по-прежнему отменяет worker group.
func (lifecycle *controlPlaneGRPCLifecycle) serve(ctx context.Context) error {
	if !lifecycle.started.CompareAndSwap(false, true) {
		return errors.New(controlPlaneRPCUnjoined)
	}
	done := make(chan error, 1)
	go func() {
		done <- lifecycle.server.Serve(lifecycle.listener)
		close(lifecycle.serveDone)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GracefulStop records actual completion after gRPC has joined its current handlers.
// GracefulStop фиксирует actual completion после join текущих handlers внутри gRPC.
func (lifecycle *controlPlaneGRPCLifecycle) GracefulStop() {
	lifecycle.gracefulOnce.Do(func() {
		lifecycle.server.GracefulStop()
		close(lifecycle.gracefulDone)
	})
}

// Stop records actual forced completion; WaitForHandlers(true) prevents an unfinished method
// from being mistaken for a completed transport shutdown.
// Stop фиксирует actual forced completion; WaitForHandlers(true) не позволяет принять unfinished
// method за завершённый transport shutdown.
func (lifecycle *controlPlaneGRPCLifecycle) Stop() {
	lifecycle.forcedOnce.Do(func() {
		lifecycle.server.Stop()
		close(lifecycle.forcedDone)
	})
}

// shutdown uses the existing shared bounded graceful/forced helper. Half the independent caller
// budget is reserved for forced cancellation and join; timeout retains dependencies for process exit.
// shutdown использует existing shared bounded graceful/forced helper. Половина independent caller
// budget оставлена forced cancellation и join; timeout сохраняет dependencies до process exit.
func (lifecycle *controlPlaneGRPCLifecycle) shutdown(ctx context.Context) error {
	deadline, bounded := ctx.Deadline()
	if !bounded || !time.Now().Before(deadline) {
		return errors.New(controlPlaneRPCUnjoined)
	}
	graceful, cancel := context.WithTimeout(ctx, time.Until(deadline)/2)
	defer cancel()
	err := grpcserver.GracefulStop(graceful, ctx, lifecycle)
	lifecycle.forceRequired.Store(err != nil)
	select {
	case <-lifecycle.serveDone:
	case <-ctx.Done():
	}
	lifecycle.shutdownReturned.Store(true)
	if !lifecycle.joined() {
		return errors.Join(err, errors.New(controlPlaneRPCUnjoined))
	}
	return err
}

// joined checks actual completion signals, never elapsed time, canceled contexts or file counts.
// It remains false when shutdown returned while an admitted handler still owns no spool file yet.
// joined проверяет actual completion signals, не elapsed time, canceled contexts либо file counts.
// При shutdown return и admitted handler без открытого spool file результат остаётся false.
func (lifecycle *controlPlaneGRPCLifecycle) joined() bool {
	if lifecycle == nil || !lifecycle.shutdownReturned.Load() {
		return false
	}
	for _, done := range []<-chan struct{}{lifecycle.serveDone, lifecycle.gracefulDone} {
		select {
		case <-done:
		default:
			return false
		}
	}
	if lifecycle.forceRequired.Load() {
		select {
		case <-lifecycle.forcedDone:
		default:
			return false
		}
	}
	return true
}
