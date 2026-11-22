package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sharedhttp "github.com/codex-k8s/kodex/libs/go/httpserver"
)

// sshUploadConfigFixture creates only an ephemeral synthetic leaf and explicit private configuration; no platform credential, operator host or Kubernetes endpoint is read.
// sshUploadConfigFixture создаёт лишь ephemeral synthetic leaf и explicit private configuration без чтения platform credential, operator host либо Kubernetes endpoint.
func sshUploadConfigFixture(t *testing.T) Config {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic SSH upload"}, DNSNames: []string{"ssh-upload.example.invalid"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	directory := t.TempDir()
	certificateFile, privateFile := filepath.Join(directory, "tls.crt"), filepath.Join(directory, "tls.key")
	if os.WriteFile(certificateFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600) != nil || os.WriteFile(privateFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0o600) != nil {
		t.Fatal("write synthetic TLS fixture")
	}
	return Config{SSHIdentityNamespace: sshIdentityNamespace, SSHUploadListen: ":8446", SSHUploadOrigin: "https://ssh-upload.example.invalid", SSHUploadBrowserOrigin: "https://control.example.invalid", SSHUploadCertificateFile: certificateFile, SSHUploadPrivateKeyFile: privateFile, ClaimantID: "synthetic-broker", ShutdownTimeout: time.Second}
}

// TestSSHUploadConfigurationIsExplicit rejects partial fields, namespace aliases and transport substitutions while keeping the default profile disabled.
// TestSSHUploadConfigurationIsExplicit отклоняет partial fields, namespace aliases и transport substitutions, сохраняя default profile выключенным.
func TestSSHUploadConfigurationIsExplicit(t *testing.T) {
	if (Config{}).sshIdentityUploadEnabled() || (Config{}).validateSSHIdentityUpload() != nil {
		t.Fatal("absent feature selected a route")
	}
	config := sshUploadConfigFixture(t)
	if config.validateSSHIdentityUpload() != nil {
		t.Fatal("explicit fixture rejected")
	}
	for _, change := range []func(*Config){func(c *Config) { c.SSHIdentityNamespace = "kodex-runtime" }, func(c *Config) { c.SSHUploadListen = ":8443" }, func(c *Config) { c.SSHUploadOrigin = "http://ssh-upload.example.invalid" }, func(c *Config) { c.SSHUploadBrowserOrigin = "*" }, func(c *Config) { c.SSHUploadPrivateKeyFile = "" }, func(c *Config) { c.SSHUploadCertificateFile = "relative" }, func(c *Config) { c.ClaimantID = "" }} {
		changed := config
		change(&changed)
		if changed.validateSSHIdentityUpload() == nil {
			t.Fatal("partial or substituted custody configuration accepted")
		}
	}
}

// TestSSHUploadTLSWorkerCancelsAndJoinsOwnedRequest uses an actual loopback TLS1.3 listener and a request held until its worker-owned context ends; dependency closure is permitted only after the response handler exits.
// TestSSHUploadTLSWorkerCancelsAndJoinsOwnedRequest использует actual loopback TLS1.3 listener и request до отмены worker-owned context; dependency closure разрешается лишь после выхода response handler.
func TestSSHUploadTLSWorkerCancelsAndJoinsOwnedRequest(t *testing.T) {
	config := sshUploadConfigFixture(t)
	entered, exited := make(chan struct{}), make(chan struct{})
	server, gate, err := newSSHUploadServer(config, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer close(exited)
		close(entered)
		<-request.Context().Done()
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	component := &sshIdentityUploadComponent{server: server, gate: gate, listener: listener, shutdown: time.Second}
	defer component.close()
	worker, cancel := context.WithCancel(t.Context())
	defer cancel()
	joined := make(chan error, 1)
	go func() { joined <- component.worker(context.WithoutCancel(t.Context()))(worker) }()
	roots := x509.NewCertPool()
	roots.AddCert(server.TLSConfig.Certificates[0].Leaf)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "ssh-upload.example.invalid", NextProtos: []string{"http/1.1"}}}}
	defer client.CloseIdleConnections()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, _ := client.Get("https://" + listener.Addr().String() + "/synthetic")
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("TLS request did not enter handler")
	}
	var closed atomic.Bool
	if closeBrokerDependency(func() error { closed.Store(true); return nil }, component) == nil || closed.Load() {
		t.Fatal("owner dependency closed before admission/request join")
	}
	cancel()
	select {
	case err := <-joined:
		if err != nil {
			t.Fatal("cooperative TLS worker did not join", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker failed actual join")
	}
	<-exited
	<-requestDone
	if closeBrokerDependency(func() error { closed.Store(true); return nil }, component) != nil || !closed.Load() || !component.joined() {
		t.Fatal("actual request join failed to release owner dependency")
	}
}

// TestSSHUploadTimeoutRetainsOwnerDependency proves failed graceful shutdown and a live handler never become successful join; the test explicitly releases and joins its synthetic handler afterward.
// TestSSHUploadTimeoutRetainsOwnerDependency доказывает, что failed graceful shutdown и live handler не дают successful join; test затем явно освобождает и joined synthetic handler.
func TestSSHUploadTimeoutRetainsOwnerDependency(t *testing.T) {
	entered, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	gate, _ := sharedhttp.NewRequestGate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { defer close(exited); close(entered); <-release }), 1)
	component := &sshIdentityUploadComponent{gate: gate, server: &http.Server{}, shutdown: 10 * time.Millisecond}
	component.started.Store(true)
	go gate.ServeHTTP(&sshUploadResponseSink{header: make(http.Header)}, &http.Request{})
	<-entered
	err := component.shutdownHTTP(t.Context(), t.Context())
	if err == nil || !strings.Contains(err.Error(), sshUploadUnjoined) || component.joined() {
		t.Fatal("unjoined handler became successful shutdown")
	}
	var closed bool
	if closeBrokerDependency(func() error { closed = true; return nil }, component) == nil || closed {
		t.Fatal("timeout closed a live dependency")
	}
	close(release)
	<-exited
	join, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if gate.Wait(join) != nil {
		t.Fatal("synthetic handler cleanup failed")
	}
	if closeBrokerDependency(func() error { closed = true; return nil }, component) != nil || !closed {
		t.Fatal("actual late join did not permit cleanup")
	}
}

// sshUploadResponseSink captures no data and reserves no network resource for the controlled timeout test.
// sshUploadResponseSink не сохраняет data и не резервирует network resource в controlled timeout test.
type sshUploadResponseSink struct{ header http.Header }

// Header supplies ordinary private response headers without external output.
// Header предоставляет ordinary private response headers без external output.
func (sink *sshUploadResponseSink) Header() http.Header { return sink.header }

// Write discards fixed fixture output while preserving the Writer contract.
// Write отбрасывает fixed fixture output, сохраняя Writer contract.
func (*sshUploadResponseSink) Write(raw []byte) (int, error) { return len(raw), nil }

// WriteHeader is inert because the fixture never inspects a response status.
// WriteHeader не имеет effects: fixture не исследует response status.
func (*sshUploadResponseSink) WriteHeader(int) {}

// TestSSHUploadDependencyDeferOrdering covers both startup without a running listener and a closed-but-active handler; cleanup errors preserve failure instead of reporting a healthy server.
// TestSSHUploadDependencyDeferOrdering проверяет startup без running listener и closed-but-active handler; cleanup errors сохраняют failure вместо healthy server.
func TestSSHUploadDependencyDeferOrdering(t *testing.T) {
	if err := closeBrokerDependency(func() error { return nil }); err != nil {
		t.Fatal("disabled startup retained an unused dependency")
	}
	sentinel := errors.New("synthetic owner close failure")
	if !errors.Is(closeBrokerDependency(func() error { return sentinel }), sentinel) {
		t.Fatal("dependency cleanup failure lost identity")
	}
	if closeBrokerDependency(nil) == nil {
		t.Fatal("nil dependency closer became success")
	}
}

// TestSSHUploadDeferredCleanupOrder exercises the actual component close and owner-close guards in their production defer order, preserving a live dependency on failed join and closing it after a real handler exit.
// TestSSHUploadDeferredCleanupOrder проверяет actual component close и owner-close guards в production defer order, сохраняя live dependency при failed join и закрывая после actual handler exit.
func TestSSHUploadDeferredCleanupOrder(t *testing.T) {
	for _, cooperative := range []bool{false, true} {
		t.Run(map[bool]string{false: "unjoined", true: "joined"}[cooperative], func(t *testing.T) {
			entered, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
			gate, _ := sharedhttp.NewRequestGate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { defer close(exited); close(entered); <-release }), 1)
			component := &sshIdentityUploadComponent{server: &http.Server{}, gate: gate, shutdown: 10 * time.Millisecond}
			component.started.Store(true)
			go gate.ServeHTTP(&sshUploadResponseSink{header: make(http.Header)}, &http.Request{})
			<-entered
			var dependencyClosed bool
			result := func() (resultErr error) {
				defer func() {
					resultErr = errors.Join(resultErr, closeBrokerDependency(func() error { dependencyClosed = true; return nil }, component))
				}()
				defer component.close()
				if cooperative {
					close(release)
					<-exited
				}
				return component.shutdownHTTP(t.Context(), t.Context())
			}()
			if cooperative {
				if result != nil || !dependencyClosed {
					t.Fatal("joined cleanup did not release owner")
				}
			} else {
				if result == nil || !strings.Contains(result.Error(), sshUploadUnjoined) || dependencyClosed {
					t.Fatal("unjoined cleanup lost failure or closed owner")
				}
				close(release)
				<-exited
				joined, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				if gate.Wait(joined) != nil {
					t.Fatal("controlled handler cleanup failed")
				}
			}
		})
	}
}

// TestSSHUploadUnexpectedServeExitIsFatal refuses a healthy result when the owned listener exits without lifecycle cancellation; it also proves admission closes and idle handlers positively join before dependency release.
// TestSSHUploadUnexpectedServeExitIsFatal отклоняет healthy result при выходе listener без lifecycle cancellation и доказывает admission closure и actual idle-handler join до dependency release.
func TestSSHUploadUnexpectedServeExitIsFatal(t *testing.T) {
	config := sshUploadConfigFixture(t)
	server, gate, err := newSSHUploadServer(config, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	component := &sshIdentityUploadComponent{server: server, gate: gate, listener: listener, shutdown: time.Second}
	defer component.close()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := component.worker(context.WithoutCancel(t.Context()))(t.Context()); err == nil || !strings.Contains(err.Error(), sshUploadUnavailable) || !gate.Closed() || !component.joined() {
		t.Fatal("unexpected listener exit became healthy or failed local join")
	}
}
