package grpcserver

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// shutdownServer coordinates graceful completion and forced-stop fallback
// with channels so tests can observe both shutdown paths.
// RU: shutdownServer согласует плавное завершение и принудительную остановку
// через каналы, чтобы тесты различали оба пути.
type shutdownServer struct {
	started  chan struct{}
	release  chan struct{}
	stopOnce sync.Once
	stopped  bool
}

// GracefulStop signals startup and blocks until the fixture releases it.
// RU: GracefulStop сообщает о начале и ждёт освобождения тестовым объектом.
func (server *shutdownServer) GracefulStop() { close(server.started); <-server.release }

// Stop records the fallback and releases the graceful waiter once.
// RU: Stop фиксирует принудительный путь и однократно освобождает ожидающий вызов.
func (server *shutdownServer) Stop() {
	server.stopped = true
	server.stopOnce.Do(func() { close(server.release) })
}

// TestGracefulStopUsesStopFallbackAndJoins checks that cancellation starts
// Stop and joins the previously blocked graceful operation.
// RU: TestGracefulStopUsesStopFallbackAndJoins проверяет запуск Stop при
// отмене и ожидание ранее заблокированного плавного завершения.
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

// TestGracefulStopReturnsAfterGracefulCompletion ensures a completed graceful
// path does not invoke the forced Stop operation.
// RU: TestGracefulStopReturnsAfterGracefulCompletion проверяет, что успешное
// плавное завершение не вызывает принудительный Stop.
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

// blockingStopServer models a Stop implementation that never joins until an
// explicit test release, exercising the force-context budget.
// RU: blockingStopServer имитирует Stop, который не завершается до явного
// освобождения тестом, для проверки ограниченного force-контекста.
type blockingStopServer struct {
	gracefulStarted chan struct{}
	stopStarted     chan struct{}
	release         chan struct{}
}

// GracefulStop signals entry and waits for the test release.
// RU: GracefulStop сообщает о входе и ждёт освобождения тестом.
func (server *blockingStopServer) GracefulStop() { close(server.gracefulStarted); <-server.release }

// Stop signals the fallback and remains blocked until the test release.
// RU: Stop сообщает о принудительном пути и ждёт освобождения тестом.
func (server *blockingStopServer) Stop() { close(server.stopStarted); <-server.release }

// TestGracefulStopDoesNotWaitForBlockingStop checks that an exhausted force
// context lets later cleanup proceed even if both server calls remain blocked.
// RU: TestGracefulStopDoesNotWaitForBlockingStop проверяет, что истечение
// force-контекста не задерживает дальнейшую очистку при двух блокировках.
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

// TestGracefulStopRejectsUnboundedContexts requires separate deadlines for
// graceful and forced shutdown before any goroutine is started.
// RU: TestGracefulStopRejectsUnboundedContexts требует отдельных сроков
// плавной и принудительной остановки до запуска фоновых вызовов.
func TestGracefulStopRejectsUnboundedContexts(t *testing.T) {
	server := &shutdownServer{started: make(chan struct{}), release: make(chan struct{})}
	if err := GracefulStop(context.Background(), context.Background(), server); err == nil {
		t.Fatal("неограниченные shutdown contexts приняты")
	}
}
