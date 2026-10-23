package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHTTPDrainPreservesAcceptedRequestAfterProcessSignal runs a loopback HTTP server, cancels the process context and proves an accepted request can finish successfully before explicit serving cancellation. It tests graceful local drain, not a real OS signal or downstream service.
// TestHTTPDrainPreservesAcceptedRequestAfterProcessSignal запускает loopback HTTP server, отменяет process context и доказывает успешное завершение принятого запроса до явной serving cancellation. Проверяется локальный graceful drain, а не реальный OS signal и downstream service.
func TestHTTPDrainPreservesAcceptedRequestAfterProcessSignal(t *testing.T) {
	lifecycle, stop := context.WithCancel(context.Background())
	defer stop()
	requests, cancelRequests := servingContext(lifecycle)
	defer cancelRequests()
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- r.Context()
		select {
		case <-release:
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	server.Config.BaseContext = func(net.Listener) context.Context { return requests }
	server.Start()
	defer server.Close()
	result := make(chan int, 1)
	go func() {
		response, err := server.Client().Get(server.URL)
		if err != nil {
			result <- 0
			return
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
		result <- response.StatusCode
	}()
	var active context.Context
	select {
	case active = <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	stop()
	if active.Err() != nil {
		t.Fatal("process signal canceled an accepted request")
	}
	shutdown := make(chan error, 1)
	go func() { shutdown <- shutdownHTTPServer(lifecycle, server.Config, time.Second) }()
	close(release)
	select {
	case status := <-result:
		if status != http.StatusOK {
			t.Fatalf("request status = %d", status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request did not finish")
	}
	select {
	case err := <-shutdown:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	cancelRequests()
	if requests.Err() == nil {
		t.Fatal("request lifecycle was not closed")
	}
}

// TestHTTPDrainTimeoutClosesActiveRequest holds a loopback handler active, uses a twenty-millisecond drain budget and checks DeadlineExceeded plus eventual request/client termination after forced Close.
// TestHTTPDrainTimeoutClosesActiveRequest удерживает loopback handler активным, задаёт двадцатимиллисекундный drain budget и проверяет DeadlineExceeded и последующее завершение request и client после принудительного Close.
func TestHTTPDrainTimeoutClosesActiveRequest(t *testing.T) {
	lifecycle, stop := context.WithCancel(context.Background())
	defer stop()
	requests, cancelRequests := servingContext(lifecycle)
	defer cancelRequests()
	started, ended := make(chan struct{}), make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(ended)
	}))
	server.Config.BaseContext = func(net.Listener) context.Context { return requests }
	server.Start()
	defer server.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := server.Client().Get(server.URL)
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	stop()
	if err := shutdownHTTPServer(lifecycle, server.Config, 20*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected shutdown result: %v", err)
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("active request was not canceled after budget")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP client did not finish")
	}
}

// TestHTTPServingContextPreservesClientCancellation cancels a loopback client's request and checks the server handler context still observes cancellation despite the detached process serving context.
// TestHTTPServingContextPreservesClientCancellation отменяет запрос loopback client и проверяет сохранение отмены server handler context несмотря на отделённый process serving context.
func TestHTTPServingContextPreservesClientCancellation(t *testing.T) {
	requests, stop := servingContext(context.Background())
	defer stop()
	started, ended := make(chan struct{}), make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(ended) }))
	server.Config.BaseContext = func(net.Listener) context.Context { return requests }
	server.Start()
	defer server.Close()
	clientContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(clientContext, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, err := server.Client().Do(request)
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("client cancellation was lost")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP client did not finish")
	}
}
