package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"testing"
	"time"

	sharedhttp "github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

// kubernetesUploadLifecycleFixture creates an actual TLS upload worker with controlled synchronous cleanup.
// kubernetesUploadLifecycleFixture создаёт настоящий TLS upload worker с управляемой синхронной очисткой.
func kubernetesUploadLifecycleFixture(t *testing.T, handler http.Handler) (*kubernetesIdentityUpload, *http.Client) {
	t.Helper()
	certificate := brokerTLSCertificate(t)
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	gate, err := sharedhttp.NewRequestGate(handler, 8)
	if err != nil {
		t.Fatal(err)
	}
	upload := &kubernetesIdentityUpload{server: &http.Server{Addr: "127.0.0.1:0", Handler: gate, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}}, ReadHeaderTimeout: time.Second}, gate: gate, shutdown: 50 * time.Millisecond}
	if err := upload.bind(); err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "broker.example.invalid"}}
	t.Cleanup(transport.CloseIdleConnections)
	t.Cleanup(upload.close)
	return upload, &http.Client{Transport: transport, Timeout: 3 * time.Second}
}

// TestKubernetesUploadShutdownWaitsForHandlers requires positive cleanup join independently of TLS Serve return.
// TestKubernetesUploadShutdownWaitsForHandlers требует завершения очистки независимо от возврата TLS Serve.
func TestKubernetesUploadShutdownWaitsForHandlers(t *testing.T) {
	for _, delayed := range []bool{false, true} {
		name := "cooperative"
		if delayed {
			name = "forced-unjoined"
		}
		t.Run(name, func(t *testing.T) {
			entered, exited, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			upload, client := kubernetesUploadLifecycleFixture(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				close(entered)
				defer close(exited)
				<-request.Context().Done()
				if delayed {
					<-release
				}
				writer.WriteHeader(http.StatusNoContent)
			}))
			application, cancel := context.WithCancel(t.Context())
			defer cancel()
			workers := serviceruntime.StartWorkers(application, upload.Worker())
			requestDone := make(chan struct{})
			go func() {
				defer close(requestDone)
				response, err := client.Get("https://" + upload.listener.Addr().String() + "/synthetic")
				if err == nil {
					response.Body.Close()
				}
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				close(release)
				t.Fatal("TLS upload handler did not enter")
			}
			closed := false
			if closeBrokerDependency(func() error { closed = true; return nil }, upload) == nil || closed {
				close(release)
				t.Fatal("active upload lost dependency")
			}
			cancel()
			budget, stop := context.WithTimeout(t.Context(), time.Second)
			defer stop()
			err := workers.Wait(budget)
			if budget.Err() != nil || (err != nil) != delayed {
				close(release)
				t.Fatal("upload shutdown was unbounded or mislabeled")
			}
			if delayed {
				if upload.joined() || closeBrokerDependency(func() error { closed = true; return nil }, upload) == nil || closed {
					close(release)
					t.Fatal("forced Close substituted for handler join")
				}
				close(release)
			}
			select {
			case <-exited:
			case <-time.After(time.Second):
				t.Fatal("handler failed to exit")
			}
			<-requestDone
			join, finish := context.WithTimeout(t.Context(), time.Second)
			defer finish()
			if upload.gate.Wait(join) != nil || !upload.joined() || closeBrokerDependency(func() error { closed = true; return nil }, upload) != nil || !closed {
				t.Fatal("positive upload join did not release dependencies")
			}
		})
	}
}

// TestKubernetesUploadUnexpectedServeExitIsFatal closes admission when the listener fails independently.
// TestKubernetesUploadUnexpectedServeExitIsFatal закрывает приём при независимом отказе listener.
func TestKubernetesUploadUnexpectedServeExitIsFatal(t *testing.T) {
	upload, _ := kubernetesUploadLifecycleFixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if err := upload.listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := upload.Worker()(t.Context()); err == nil || !upload.joined() {
		t.Fatal("failed listener remained healthy or unjoined")
	}
}
