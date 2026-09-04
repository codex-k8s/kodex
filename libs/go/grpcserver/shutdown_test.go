package grpcserver

import (
	"context"
	"errors"
	"sync"
	"testing"
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
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- GracefulStop(ctx, server) }()
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
	if err := GracefulStop(t.Context(), server); err != nil || server.stopped {
		t.Fatalf("graceful result: stopped=%v err=%v", server.stopped, err)
	}
}
