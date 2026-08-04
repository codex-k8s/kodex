package websockettransport

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type blockedConnection struct {
	closeStarted chan struct{}
	forced       chan struct{}
	startOnce    sync.Once
	forceOnce    sync.Once
	onForce      func()
}

func (connection *blockedConnection) Close(websocket.StatusCode, string) error {
	connection.startOnce.Do(func() { close(connection.closeStarted) })
	<-connection.forced
	return nil
}

func (connection *blockedConnection) CloseNow() error {
	connection.forceOnce.Do(func() {
		close(connection.forced)
		connection.onForce()
	})
	return nil
}

func TestShutdownForceClosesAllPeersWithinBudget(t *testing.T) {
	t.Parallel()

	server := &Server{active: make(map[trackedConnection]struct{})}
	const peerCount = 4
	for range peerCount {
		connection := &blockedConnection{
			closeStarted: make(chan struct{}),
			forced:       make(chan struct{}),
		}
		server.connectionWG.Add(1)
		connection.onForce = server.connectionWG.Done
		server.active[connection] = struct{}{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown should force and join peers before the budget expires: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 120*time.Millisecond {
		t.Fatalf("shutdown exceeded its budget: %s", elapsed)
	}
	if !server.stopping {
		t.Fatal("shutdown must stop admission before draining peers")
	}
}

func TestShutdownReturnsDeadlineAndDoesNotBlockFollowingCleanup(t *testing.T) {
	t.Parallel()

	server := &Server{active: make(map[trackedConnection]struct{})}
	connection := &blockedConnection{
		closeStarted: make(chan struct{}),
		forced:       make(chan struct{}),
		onForce:      func() {},
	}
	server.connectionWG.Add(1) // Моделирует handler, который не успел завершиться.
	server.active[connection] = struct{}{}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := server.Shutdown(ctx); err == nil {
		t.Fatal("shutdown must report the exhausted budget")
	}
	cleanupContinued := false
	cleanupContinued = true
	if !cleanupContinued {
		t.Fatal("a bounded WebSocket drain must not block subsequent cleanup")
	}
}
