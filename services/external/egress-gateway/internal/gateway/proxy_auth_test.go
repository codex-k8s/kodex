package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/connect"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/runtimepolicy"
)

type challengePolicyFixture struct {
	AuthenticatedAccessPolicy
	calls        atomic.Int32
	writeTimeout int
}

func (fixture *challengePolicyFixture) AuthorizeAuthenticated(host string, port int, credential string) (runtimecontract.RuntimeProxyAccess, bool) {
	fixture.calls.Add(1)
	return fixture.AuthenticatedAccessPolicy.AuthorizeAuthenticated(host, port, credential)
}

func (fixture *challengePolicyFixture) Limits() policy.Limits {
	limits := fixture.AuthenticatedAccessPolicy.Limits()
	if fixture.writeTimeout != 0 {
		limits.WriteTimeoutMilliseconds = fixture.writeTimeout
	}
	return limits
}

func authenticatedChallengeFixture(t *testing.T) (*Server, *challengePolicyFixture, *fakeResolver, *fakeDialer, string) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", "deploy/k8s/base/egress-gateway/policy.json")
	digest, err := policy.DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	base, err := policy.LoadFile(path, "2026-09-05.1", digest)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("synthetic", 8))
	active, err := runtimepolicy.New(base, key)
	if err != nil {
		t.Fatal(err)
	}
	access := runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessAllowlistReadOnly,
		Rules: []runtimecontract.RuntimeWebAccessRule{{DomainPattern: "example.com", Protocol: "HTTPS", Port: 443, HTTPMethods: []string{"GET", "HEAD"}}}}
	grant, err := runtimecontract.SignRuntimeWebAccessGrant(key, "synthetic-execution", strings.Repeat("a", 64), access)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &challengePolicyFixture{AuthenticatedAccessPolicy: active}
	resolver, dialer := &fakeResolver{}, &fakeDialer{peers: make(chan net.Conn, 1)}
	ca, _ := proxyAuthorityFixture(t)
	server, err := NewAuthenticated(t.Context(), "127.0.0.1:0", fixture, resolver, dialer, readyStub(true), newTestMetrics(t), ca)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.cancel)
	return server, fixture, resolver, dialer, grant
}

func proxyAuthenticationHeader(credential string) string {
	return "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("kodex:"+credential)) + "\r\n"
}

func TestAuthenticatedProxyChallengesWithoutAuthorityOrUpstreamDial(t *testing.T) {
	const envelope = "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n"
	for _, name := range []string{"absent", "invalid_signature", "retired_signer", "wrong_host", "malformed", "duplicate", "body", "oversized"} {
		t.Run(name, func(t *testing.T) {
			server, active, resolver, dialer, grant := authenticatedChallengeFixture(t)
			request := envelope + "\r\n"
			switch name {
			case "invalid_signature":
				request = envelope + proxyAuthenticationHeader(grant+"x") + "\r\n"
			case "retired_signer":
				claims, err := runtimecontract.VerifyRuntimeWebAccessGrant([]byte(strings.Repeat("synthetic", 8)), grant)
				if err != nil {
					t.Fatal(err)
				}
				stale, err := runtimecontract.SignRuntimeWebAccessGrant([]byte(strings.Repeat("retired", 8)), claims.WorkloadRef, claims.NetworkDigest, claims.WebAccess)
				if err != nil {
					t.Fatal(err)
				}
				request = envelope + proxyAuthenticationHeader(stale) + "\r\n"
			case "wrong_host":
				request = "CONNECT other.example:443 HTTP/1.1\r\nHost: other.example:443\r\n" + proxyAuthenticationHeader(grant) + "\r\n"
			case "malformed":
				request = envelope + "Proxy-Authorization: malformed\r\n\r\n"
			case "duplicate":
				request = envelope + proxyAuthenticationHeader(grant) + proxyAuthenticationHeader(grant) + "\r\n"
			case "body":
				request = envelope + "Content-Length: 0\r\n\r\n"
			case "oversized":
				request = envelope + "X-Fill: " + strings.Repeat("a", active.Limits().MaximumHeaderBytes) + "\r\n\r\n"
			}
			peer, client := net.Pipe()
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(time.Second))
			done := make(chan struct{})
			go func() { defer close(done); defer peer.Close(); server.handle(peer) }()
			written := make(chan struct{})
			go func() { defer close(written); _, _ = io.WriteString(client, request) }()
			response, readErr := http.ReadResponse(bufio.NewReader(client), nil)
			if name == "absent" {
				if readErr != nil || response.StatusCode != http.StatusProxyAuthRequired || response.Header.Get("Proxy-Authenticate") != `Basic realm="kodex-runtime"` || !response.Close || response.ContentLength != 0 {
					t.Fatal("missing proxy authentication did not receive the bounded Basic challenge")
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil || len(body) != 0 || active.calls.Load() != 0 {
					t.Fatal("challenge disclosed a body or evaluated authority")
				}
			} else if readErr == nil {
				_ = response.Body.Close()
				t.Fatal("invalid credentials or envelope received a challenge or tunnel")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("authentication handler did not join")
			}
			select {
			case <-written:
			case <-time.After(time.Second):
				t.Fatal("request writer did not join")
			}
			if resolver.calls != 0 || len(dialer.targets) != 0 {
				t.Fatal("authentication failure crossed the zero-DNS/zero-dial boundary")
			}
		})
	}
}

func TestProxyAuthenticationChallengeWriteIsBounded(t *testing.T) {
	server, active, resolver, dialer, _ := authenticatedChallengeFixture(t)
	active.writeTimeout = 20
	peer, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() { defer close(done); defer peer.Close(); server.handle(peer) }()
	_, _ = io.WriteString(client, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	// Клиент намеренно не читает challenge: Write обязан закончиться по deadline.
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("challenge write exceeded its bounded budget")
	}
	if resolver.calls != 0 || len(dialer.targets) != 0 {
		t.Fatal("blocked challenge dialed upstream")
	}
}

func TestDefaultGitNegotiatesProxyChallengeWithinExactTLSAndGrant(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git executable is unavailable for the loopback compatibility check")
	}
	server, active, resolver, _, grant := authenticatedChallengeFixture(t)
	certificate, roots := serverCertificateFixture(t, "example.com")
	var requests atomic.Int32
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.Host != "example.com" || request.URL.Path != "/synthetic.git/info/refs" || request.URL.RawQuery != "service=git-upload-pack" || request.Header.Get("Proxy-Authorization") != "" {
			writer.WriteHeader(http.StatusForbidden)
			return
		}
		requests.Add(1)
		writer.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		const advertisement = "# service=git-upload-pack\n"
		_, _ = fmt.Fprintf(writer, "%04x%s00000000", len(advertisement)+4, advertisement)
	}))
	upstream.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	upstream.StartTLS()
	defer upstream.Close()
	server.upstreamRoots = roots
	server.dialer = discoveryHTTP2Dialer{address: upstream.Listener.Addr().String()}
	resolver.snapshot = dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, ExpiresAt: time.Now().Add(time.Minute)}
	if err := server.Listen(); err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve() }()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			t.Error(err)
		}
		if err := <-serveDone; err != nil {
			t.Error(err)
		}
	}()
	caPath := filepath.Join(t.TempDir(), "proxy-ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.interceptCA.certificate.Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, git, "-c", "protocol.version=0", "-c", "credential.helper=", "-c", "http.sslCAInfo="+caPath, "ls-remote", "https://example.com/synthetic.git")
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "https_proxy=http://kodex:" + grant + "@" + server.Address().String(), "NO_PROXY="}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("default Git loopback exit=%v output_bytes=%d", err, len(output))
	}
	if len(output) != 0 || requests.Load() != 1 || active.calls.Load() != 1 {
		t.Fatal("Git did not negotiate one authenticated, exact read-only request")
	}
}

type challengeWriteNotice struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (connection *challengeWriteNotice) Write(data []byte) (int, error) {
	connection.once.Do(func() { close(connection.started) })
	return connection.Conn.Write(data)
}

func TestShutdownCancelsAndJoinsPendingAuthenticationChallenge(t *testing.T) {
	server, active, resolver, dialer, _ := authenticatedChallengeFixture(t)
	peer, client := net.Pipe()
	defer client.Close()
	connection := &challengeWriteNotice{Conn: peer, started: make(chan struct{})}
	if !server.acquire(connection) {
		t.Fatal("challenge connection was not tracked")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.wait.Done()
		defer server.release(connection)
		server.handle(connection)
	}()
	_, _ = io.WriteString(client, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	select {
	case <-connection.started:
	case <-time.After(time.Second):
		t.Fatal("challenge write did not start")
	}
	shutdown, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled challenge handler did not join")
	}
	if active.calls.Load() != 0 || resolver.calls != 0 || len(dialer.targets) != 0 || len(server.active) != 0 || len(server.global) != 0 {
		t.Fatal("shutdown retained challenge resources or crossed the zero-authority/zero-dial boundary")
	}
}

func TestAuthenticationChallengeKeepsClosedCredentialMetricReason(t *testing.T) {
	if connectReason(&connect.Error{Reason: connect.ReasonAuthenticationRequired}) != string(connect.ReasonCredentials) {
		t.Fatal("authentication challenge changed the closed credential metric reason")
	}
}
