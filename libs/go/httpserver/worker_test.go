package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// workerListener exposes Serve entry and listener closure without opening a socket or accepting a connection.
// RU: workerListener отмечает вход в Serve и закрытие listener без открытия сокета и приёма соединения.
type workerListener struct {
	entered  chan struct{}
	closed   chan struct{}
	returned chan struct{}
	once     sync.Once
}

// Accept announces the first blocking accept and returns a permanent closed-listener error after Close.
// RU: Accept отмечает первый блокирующий приём и после Close возвращает постоянную ошибку закрытого listener.
func (listener *workerListener) Accept() (net.Conn, error) {
	close(listener.entered)
	<-listener.closed
	close(listener.returned)
	return nil, net.ErrClosed
}

// Close releases Accept exactly once even when HTTP shutdown and Serve cleanup both close the listener.
// RU: Close освобождает Accept ровно один раз даже при закрытии listener из HTTP shutdown и очистки Serve.
func (listener *workerListener) Close() error {
	listener.once.Do(func() { close(listener.closed) })
	return nil
}

// Addr provides synthetic loopback metadata for http.Server without reserving a network endpoint.
// RU: Addr предоставляет http.Server синтетические loopback-данные без резервирования сетевого адреса.
func (*workerListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

// newWorkerFixture attaches a controlled listener to a real HTTP server and registers independent bounded cleanup.
// RU: newWorkerFixture подключает управляемый listener к настоящему HTTP-серверу и регистрирует независимую ограниченную очистку.
func newWorkerFixture(t *testing.T) (*Server, *workerListener) {
	t.Helper()
	server, err := New(testConfig(), readyStub{}, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	listener := &workerListener{entered: make(chan struct{}), closed: make(chan struct{}), returned: make(chan struct{})}
	server.listener = listener
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("worker fixture shutdown failed: %v", err)
		}
	})
	return server, listener
}

// awaitWorkerSignal bounds synchronization with the synthetic listener so a broken worker fails instead of hanging the suite.
// RU: awaitWorkerSignal ограничивает ожидание сигнала синтетического listener, чтобы сломанный worker не подвесил тесты.
func awaitWorkerSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("technical HTTP worker signal timed out")
	}
}

// TestWorkerConstructionDoesNotServe checks that obtaining the adapter neither starts Accept nor closes its listener.
// RU: TestWorkerConstructionDoesNotServe проверяет, что получение адаптера не запускает Accept и не закрывает listener.
func TestWorkerConstructionDoesNotServe(t *testing.T) {
	server, listener := newWorkerFixture(t)
	if server.Worker() == nil {
		t.Fatal("technical HTTP worker is missing")
	}
	select {
	case <-listener.entered:
		t.Fatal("worker construction started serving")
	case <-listener.closed:
		t.Fatal("worker construction closed the listener")
	default:
	}
}

// TestWorkerReportsMissingListener verifies that the adapter preserves Serve's closed error for nil and unstarted servers.
// RU: TestWorkerReportsMissingListener проверяет сохранение безопасной ошибки Serve для nil и незапущенных серверов.
func TestWorkerReportsMissingListener(t *testing.T) {
	for _, server := range []*Server{nil, {}, {server: &http.Server{}}} {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		err := server.Worker()(ctx)
		cancel()
		if err == nil || err.Error() != "technical HTTP server is not listening" {
			t.Fatalf("unexpected missing-listener result: %v", err)
		}
	}
}

// TestWorkerCancellationLeavesShutdownToCaller proves cancellation returns promptly while only the caller's Shutdown closes the listener.
// RU: TestWorkerCancellationLeavesShutdownToCaller доказывает быстрый возврат при отмене и закрытие listener только через Shutdown вызывающего кода.
func TestWorkerCancellationLeavesShutdownToCaller(t *testing.T) {
	server, listener := newWorkerFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Worker()(ctx) }()
	awaitWorkerSignal(t, listener.entered)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("worker cancellation result changed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not observe cancellation")
	}
	select {
	case <-listener.closed:
		t.Fatal("worker cancellation took over listener shutdown")
	default:
	}
	shutdown, cancelShutdown := context.WithTimeout(t.Context(), time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	awaitWorkerSignal(t, listener.returned)
}

// TestWorkerReportsOrdinaryShutdown verifies that caller-owned graceful shutdown reaches the worker as success, not ErrServerClosed.
// RU: TestWorkerReportsOrdinaryShutdown проверяет, что штатная остановка владельцем возвращается из worker как успех, а не ErrServerClosed.
func TestWorkerReportsOrdinaryShutdown(t *testing.T) {
	server, listener := newWorkerFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Worker()(ctx) }()
	awaitWorkerSignal(t, listener.entered)
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ordinary shutdown was not preserved: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("worker did not return after shutdown")
	}
}

// TestWorkerPreservesUnexpectedServeError checks that closing the listener outside HTTP shutdown is still reported as an error.
// RU: TestWorkerPreservesUnexpectedServeError проверяет, что закрытие listener вне HTTP shutdown по-прежнему возвращает ошибку.
func TestWorkerPreservesUnexpectedServeError(t *testing.T) {
	server, listener := newWorkerFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Worker()(ctx) }()
	awaitWorkerSignal(t, listener.entered)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("unexpected Serve error was hidden: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("worker did not report Serve failure")
	}
}
