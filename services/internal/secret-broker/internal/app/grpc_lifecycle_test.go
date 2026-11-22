package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	health "google.golang.org/grpc/health/grpc_health_v1"
)

// brokerLifecycleHealth keeps one real TLS RPC alive until cancellation or an explicit test release; it does not replace transport shutdown with a mock.
// brokerLifecycleHealth удерживает один actual TLS RPC до cancellation либо explicit test release, не подменяя transport shutdown mock.
type brokerLifecycleHealth struct {
	health.UnimplementedHealthServer
	entered chan struct{}
	exited  chan struct{}
	release <-chan struct{}
}

// Check either cooperates with the app context or holds the handler beyond forced Stop to test the positive dependency-join boundary.
// Check реагирует на app context либо удерживает handler после forced Stop для проверки positive dependency-join boundary.
func (handler *brokerLifecycleHealth) Check(ctx context.Context, _ *health.HealthCheckRequest) (*health.HealthCheckResponse, error) {
	close(handler.entered)
	defer close(handler.exited)
	if handler.release == nil {
		<-ctx.Done()
	} else {
		<-handler.release
	}
	return &health.HealthCheckResponse{Status: health.HealthCheckResponse_SERVING}, nil
}

// brokerTLSLifecycleFixture builds the real component and ephemeral loopback TLS client with exact synthetic CA/SNI; no platform identity or insecure transport is used.
// brokerTLSLifecycleFixture создаёт actual component и ephemeral loopback TLS client с exact synthetic CA/SNI без platform identity либо insecure transport.
func brokerTLSLifecycleFixture(t *testing.T, release <-chan struct{}) (*brokerGRPCComponent, *brokerLifecycleHealth, health.HealthClient, func()) {
	t.Helper()
	certificate := brokerTLSCertificate(t)
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	component, err := newBrokerGRPCComponent(t.Context(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	component.listener = listener
	component.server = grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}})), grpc.ChainUnaryInterceptor(component.unary))
	handler := &brokerLifecycleHealth{entered: make(chan struct{}), exited: make(chan struct{}), release: release}
	health.RegisterHealthServer(component.server, handler)
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "broker.example.invalid"})))
	if err != nil {
		t.Fatal(err)
	}
	return component, handler, health.NewHealthClient(connection), func() { _ = connection.Close(); _ = listener.Close() }
}

// TestBrokerRPCCancelAndJoin exercises the real worker inside WorkerGroup and requires canceled request completion before its owner dependency can close.
// TestBrokerRPCCancelAndJoin проверяет actual worker внутри WorkerGroup и требует завершения canceled request до closure owner dependency.
func TestBrokerRPCCancelAndJoin(t *testing.T) {
	component, handler, client, cleanup := brokerTLSLifecycleFixture(t, nil)
	defer cleanup()
	application, cancel := context.WithCancel(t.Context())
	defer cancel()
	workers := serviceruntime.StartWorkers(application, component.worker(context.WithoutCancel(t.Context())))
	requestDone := make(chan struct{})
	go func() { defer close(requestDone); _, _ = client.Check(t.Context(), &health.HealthCheckRequest{}) }()
	select {
	case <-handler.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("TLS RPC did not enter")
	}
	closed := false
	if closeBrokerDependency(func() error { closed = true; return nil }, component) == nil || closed {
		t.Fatal("dependency closed while handler was active")
	}
	cancel()
	budget, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if err := workers.Wait(budget); err != nil {
		t.Fatal(err)
	}
	<-handler.exited
	<-requestDone
	if !component.joined() || closeBrokerDependency(func() error { closed = true; return nil }, component) != nil || !closed {
		t.Fatal("cooperative RPC failed positive join")
	}
}

// TestBrokerRPCForcedStopRetainsDependencies proves Stop and Serve return cannot substitute for a real handler release; WorkerGroup returns a fatal bounded result instead of hanging before RunShutdown.
// TestBrokerRPCForcedStopRetainsDependencies доказывает, что Stop и Serve return не заменяют actual handler release; WorkerGroup возвращает fatal bounded result вместо зависания до RunShutdown.
func TestBrokerRPCForcedStopRetainsDependencies(t *testing.T) {
	release := make(chan struct{})
	component, handler, client, cleanup := brokerTLSLifecycleFixture(t, release)
	defer cleanup()
	application, cancel := context.WithCancel(t.Context())
	defer cancel()
	workers := serviceruntime.StartWorkers(application, component.worker(context.WithoutCancel(t.Context())))
	requestDone := make(chan struct{})
	go func() { defer close(requestDone); _, _ = client.Check(t.Context(), &health.HealthCheckRequest{}) }()
	select {
	case <-handler.entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("TLS RPC did not enter")
	}
	cancel()
	budget, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if err := workers.Wait(budget); err == nil || budget.Err() != nil {
		close(release)
		t.Fatal("worker hung or returned success instead of owned shutdown failure")
	}
	closed := false
	if component.joined() || closeBrokerDependency(func() error { closed = true; return nil }, component) == nil || closed {
		close(release)
		t.Fatal("forced stop closed an active dependency")
	}
	close(release)
	<-handler.exited
	<-requestDone
	join, stopJoin := context.WithTimeout(t.Context(), time.Second)
	defer stopJoin()
	if component.requests.Wait(join) != nil {
		t.Fatal("released request failed join")
	}
	for !component.joined() && join.Err() == nil {
		time.Sleep(time.Millisecond)
	}
	if component.wait(join) != nil || closeBrokerDependency(func() error { closed = true; return nil }, component) != nil || !closed {
		t.Fatal("actual late handler/stop join failed")
	}
}

// TestBrokerRPCUnexpectedServeExitIsFatal checks a closed listener cannot leave an apparently healthy worker, while startup-only components may release unused dependencies.
// TestBrokerRPCUnexpectedServeExitIsFatal проверяет, что closed listener не оставляет apparently healthy worker, а startup-only component освобождает unused dependencies.
func TestBrokerRPCUnexpectedServeExitIsFatal(t *testing.T) {
	component, _, _, cleanup := brokerTLSLifecycleFixture(t, nil)
	defer cleanup()
	if err := component.listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := component.worker(t.Context())(t.Context()); err == nil || !component.joined() {
		t.Fatal("unexpected listener exit was healthy or failed cleanup")
	}
	if _, err := newBrokerGRPCComponent(nil, time.Second); err == nil {
		t.Fatal("nil app accepted")
	}
	if _, err := newBrokerGRPCComponent(t.Context(), 0); err == nil {
		t.Fatal("unbounded shutdown accepted")
	}
}
