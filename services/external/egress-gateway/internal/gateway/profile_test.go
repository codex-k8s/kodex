package gateway

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

func TestNotReadyConnectDoesNotResolveOrDial(t *testing.T) {
	resolver, dialer := &fakeResolver{}, &fakeDialer{}
	server, err := New(context.Background(), "unused", fakePolicy{}, resolver, dialer, readyStub(false), newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	response := exchangeProfileRequest(t, server, "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n")
	if response.StatusCode != http.StatusServiceUnavailable || resolver.calls != 0 || len(dialer.targets) != 0 {
		t.Fatalf("not-ready CONNECT crossed zero-effect boundary: %d", response.StatusCode)
	}
}

func TestProfileReadinessAndConnectReadBackSameGeneration(t *testing.T) {
	profile := loadSTTProfile(t)
	for _, request := range []string{
		"GET /readyz HTTP/1.1\r\nHost: egress-gateway.kodex-system.svc.cluster.local:8081\r\n\r\n",
		"CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n",
	} {
		resolver, dialer := &fakeResolver{}, &fakeDialer{}
		server, err := New(context.Background(), "unused", profile, resolver, dialer, readyStub(true), newTestMetrics(t))
		if err != nil {
			t.Fatal(err)
		}
		response := exchangeProfileRequest(t, server, request)
		if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK {
			t.Fatalf("unexpected response: %d", response.StatusCode)
		}
		for key, expected := range map[string]string{
			"X-Kodex-Egress-Revision": profile.Revision(), "X-Kodex-Egress-Digest": profile.Digest(),
			"X-Kodex-Egress-Profile": policy.STTProfileName, "X-Kodex-Egress-Workload": policy.STTWorkload,
			"X-Kodex-Egress-Operation": policy.STTOperation,
		} {
			if response.Header.Get(key) != expected {
				t.Fatalf("incorrect %s readback", key)
			}
		}
		if resolver.calls != 0 || len(dialer.targets) != 0 {
			t.Fatal("readiness or absent ClientHello triggered external work")
		}
	}
}

func TestListenersShareOneConnectionBudget(t *testing.T) {
	first, err := New(context.Background(), "unused", fakePolicy{}, &fakeResolver{}, &fakeDialer{}, readyStub(true), newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(context.Background(), "unused", fakePolicy{}, &fakeResolver{}, &fakeDialer{}, readyStub(true), newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := second.ShareConnectionLimit(first); err != nil {
		t.Fatal(err)
	}
	for _, server := range []*Server{first, second} {
		for range (fakePolicy{}).Limits().MaximumConnectionsPerSource {
			connection, peer := net.Pipe()
			if !server.acquire(connection) {
				t.Fatal("connection rejected below shared capacity")
			}
			t.Cleanup(func() { server.release(connection); server.wait.Done(); _ = peer.Close() })
		}
	}
	if first.global != second.global || len(first.global) != (fakePolicy{}).Limits().MaximumConnections {
		t.Fatal("listener budgets are not shared")
	}
	for _, server := range []*Server{first, second} {
		connection, peer := net.Pipe()
		if server.acquire(connection) {
			t.Fatal("listener exceeded shared budget")
		}
		_ = connection.Close()
		_ = peer.Close()
	}
}

func TestSTTListenerRejectsOtherDestinationsAndCallerProfile(t *testing.T) {
	profile := loadSTTProfile(t)
	for _, request := range []string{
		"CONNECT auth.openai.com:443 HTTP/1.1\r\nHost: auth.openai.com:443\r\nX-Kodex-Egress-Profile: default\r\n\r\n",
		"CONNECT github.com:443 HTTP/1.1\r\nHost: github.com:443\r\n\r\n",
		"CONNECT api.openai.com:587 HTTP/1.1\r\nHost: api.openai.com:587\r\n\r\n",
		"CONNECT 1.1.1.1:443 HTTP/1.1\r\nHost: 1.1.1.1:443\r\n\r\n",
	} {
		resolver, dialer := &fakeResolver{}, &fakeDialer{}
		server, err := New(context.Background(), "unused", profile, resolver, dialer, readyStub(true), newTestMetrics(t))
		if err != nil {
			t.Fatal(err)
		}
		serverSide, clientSide := net.Pipe()
		done := make(chan struct{})
		go func() { server.handle(serverSide); _ = serverSide.Close(); close(done) }()
		_ = clientSide.SetDeadline(time.Now().Add(time.Second))
		_, _ = io.WriteString(clientSide, request)
		response, err := io.ReadAll(clientSide)
		_ = clientSide.Close()
		<-done
		if err != nil || len(response) != 0 || resolver.calls != 0 || len(dialer.targets) != 0 {
			t.Fatalf("rejected STT request crossed zero-effect boundary: %v", err)
		}
	}
}

func exchangeProfileRequest(t *testing.T, server *Server, request string) *http.Response {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	done := make(chan struct{})
	go func() { server.handle(serverSide); _ = serverSide.Close(); close(done) }()
	_ = clientSide.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.WriteString(clientSide, request); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(clientSide), nil)
	_ = clientSide.Close()
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("profile handler did not join")
	}
	return response
}

func loadSTTProfile(t *testing.T) *policy.Active {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", "deploy", "k8s", "base", "egress-gateway", "policy.json")
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := policy.DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	active, err := policy.Load(value, "2026-09-05.1", digest)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := active.ForProfile(policy.STTProfileName)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
