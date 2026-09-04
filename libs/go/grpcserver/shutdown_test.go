package grpcserver

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type shutdownServer struct {
	started  chan struct{}
	release  chan struct{}
	stopOnce sync.Once
	stopped  bool
}

func (server *shutdownServer) GracefulStop() { close(server.started); <-server.release }
func (server *shutdownServer) Stop() {
	server.stopped = true
	server.stopOnce.Do(func() { close(server.release) })
}

func TestGracefulStopUsesStopFallbackAndJoins(t *testing.T) {
	server := &shutdownServer{started: make(chan struct{}), release: make(chan struct{})}
	gracefulCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	forceCtx, cancelForce := context.WithTimeout(t.Context(), time.Second)
	defer cancelForce()
	result := make(chan error, 1)
	go func() { result <- GracefulStop(gracefulCtx, forceCtx, server) }()
	<-server.started
	cancel()
	err := <-result
	if !errors.Is(err, context.Canceled) || !server.stopped {
		t.Fatalf("fallback result: stopped=%v err=%v", server.stopped, err)
	}
}

func TestGracefulStopReturnsAfterGracefulCompletion(t *testing.T) {
	server := &shutdownServer{started: make(chan struct{}), release: make(chan struct{})}
	close(server.release)
	gracefulCtx, cancelGraceful := context.WithTimeout(t.Context(), time.Second)
	defer cancelGraceful()
	forceCtx, cancelForce := context.WithTimeout(t.Context(), time.Second)
	defer cancelForce()
	if err := GracefulStop(gracefulCtx, forceCtx, server); err != nil || server.stopped {
		t.Fatalf("graceful result: stopped=%v err=%v", server.stopped, err)
	}
}

type blockingStopServer struct {
	gracefulStarted chan struct{}
	stopStarted     chan struct{}
	release         chan struct{}
}

func (server *blockingStopServer) GracefulStop() { close(server.gracefulStarted); <-server.release }
func (server *blockingStopServer) Stop()         { close(server.stopStarted); <-server.release }

func TestGracefulStopDoesNotWaitForBlockingStop(t *testing.T) {
	server := &blockingStopServer{
		gracefulStarted: make(chan struct{}),
		stopStarted:     make(chan struct{}),
		release:         make(chan struct{}),
	}
	t.Cleanup(func() { close(server.release) })
	gracefulCtx, cancelGraceful := context.WithTimeout(t.Context(), time.Second)
	defer cancelGraceful()
	forceCtx, cancelForce := context.WithTimeout(t.Context(), time.Second)
	defer cancelForce()
	result := make(chan error, 1)
	go func() { result <- GracefulStop(gracefulCtx, forceCtx, server) }()
	<-server.gracefulStarted
	cancelGraceful()
	<-server.stopStarted
	cancelForce()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "forced gRPC shutdown did not join") {
			t.Fatalf("неверный bounded результат: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("блокирующий Stop задержал следующий cleanup")
	}
}

func TestGracefulStopRejectsUnboundedContexts(t *testing.T) {
	server := &shutdownServer{started: make(chan struct{}), release: make(chan struct{})}
	if err := GracefulStop(context.Background(), context.Background(), server); err == nil {
		t.Fatal("неограниченные shutdown contexts приняты")
	}
}
