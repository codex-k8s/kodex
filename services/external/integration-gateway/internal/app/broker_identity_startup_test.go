package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sharedhttp "github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

// TestBrokerIdentityStartupReleasesOnlyAcquiredDependencies tests every acquisition prefix and verifies cleanup errors neither cancel later cleanup nor close unacquired resources.
// TestBrokerIdentityStartupReleasesOnlyAcquiredDependencies проверяет каждый acquisition prefix: cleanup errors не отменяют последующую очистку и не закрывают unacquired resources.
func TestBrokerIdentityStartupReleasesOnlyAcquiredDependencies(t *testing.T) {
	names := []string{"error reporting", "tracing", "control-plane client", "broker client"}
	failure := errors.New("synthetic close failure")
	for count := 0; count <= len(names); count++ {
		actual := []string{}
		operations := []serviceruntime.ShutdownOperation{}
		for index := 0; index < count; index++ {
			name := names[index]
			operations = append(operations, serviceruntime.ShutdownOperation{Name: name, Timeout: time.Second, Run: func(ctx context.Context) error {
				if ctx.Err() != nil {
					t.Error("cleanup inherited cancellation")
				}
				actual = append(actual, name)
				if name == "tracing" {
					return failure
				}
				return nil
			}})
		}
		err := finishIntegrationOwnedResources(context.Background(), time.Second, nil, nil, nil, nil, operations)
		expected := append([]string{}, names[:count]...)
		slices.Reverse(expected)
		if !slices.Equal(actual, expected) || errors.Is(err, failure) != (count >= 2) {
			t.Fatal("partial startup cleanup order or failure changed")
		}
	}
}

// TestBrokerIdentityTechnicalTimeoutIsNotDependencyJoin uses a real HTTP handler and existing ShutdownWorker: Serve may join while a handler remains active after shutdown timeout, so clients stay open until a later actual handler join.
// TestBrokerIdentityTechnicalTimeoutIsNotDependencyJoin использует real HTTP handler и existing ShutdownWorker: Serve может завершиться при active handler после timeout; clients открыты до actual handler join.
func TestBrokerIdentityTechnicalTimeoutIsNotDependencyJoin(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered, held := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(held) }) }
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(entered)
		<-held
		_, _ = io.WriteString(writer, "ok")
	})}
	var handlersJoined atomic.Bool
	shutdown := func(ctx context.Context) error {
		err := server.Shutdown(ctx)
		if err == nil {
			handlersJoined.Store(true)
		}
		return err
	}
	lifecycle, cancel := context.WithCancel(t.Context())
	defer cancel()
	workers := serviceruntime.StartWorkers(lifecycle, sharedhttp.ShutdownWorker(func() error { return server.Serve(listener) }, shutdown, 10*time.Millisecond))
	transport := &http.Transport{Proxy: nil}
	client := &http.Client{Timeout: time.Second, Transport: transport}
	completed := make(chan error, 1)
	go func() {
		response, err := client.Get("http://" + listener.Addr().String())
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		completed <- err
	}()
	defer func() {
		release()
		cancel()
		_ = server.Close()
		transport.CloseIdleConnections()
		ctx, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		_ = workers.Wait(ctx)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("technical request did not start")
	}
	cancel()
	ctx, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	if workers.Wait(ctx) == nil || !workers.Joined() || handlersJoined.Load() {
		t.Fatal("fixture did not retain an active handler after Serve join")
	}
	closes := 0
	dependencies := []serviceruntime.ShutdownOperation{{Name: "protected clients", Timeout: time.Second, Run: func(context.Context) error { closes++; return nil }}}
	if err := finishIntegrationOwnedResources(context.Background(), 10*time.Millisecond, workers, nil, shutdown, handlersJoined.Load, dependencies); !errors.Is(err, errIntegrationWorkUnjoined) || closes != 0 {
		t.Fatal("HTTP timeout released live handler dependencies")
	}
	release()
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("technical handler did not join")
	}
	err = finishIntegrationOwnedResources(context.Background(), time.Second, workers, nil, shutdown, handlersJoined.Load, dependencies)
	if errors.Is(err, errIntegrationWorkUnjoined) || !errors.Is(err, context.DeadlineExceeded) || closes != 1 || !handlersJoined.Load() {
		t.Fatal("later HTTP join did not preserve failed outcome and safely close clients")
	}
}
