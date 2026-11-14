package gateway

import (
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

// TestTelegramListenerPinsReachabilityAndReadiness uses actual CONNECT parsing with synthetic peers; no DNS or external dial is permitted before a valid exact TLS ClientHello.
// TestTelegramListenerPinsReachabilityAndReadiness использует настоящий CONNECT parser с synthetic peers; DNS и внешний dial запрещены до верного exact TLS ClientHello.
func TestTelegramListenerPinsReachabilityAndReadiness(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", "deploy", "k8s", "base", "egress-gateway", "policy.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := policy.DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	active, err := policy.Load(raw, "2026-09-05.1", digest)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := active.ForProfile(policy.TelegramProfileName)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []string{"GET /readyz HTTP/1.1\r\nHost: egress-gateway.kodex-system.svc.cluster.local:8086\r\n\r\n", "CONNECT api.telegram.org:443 HTTP/1.1\r\nHost: api.telegram.org:443\r\n\r\n",
		"CONNECT api.telegram.org:443 HTTP/1.1\r\nHost: api.telegram.org:443\r\nX-Kodex-Egress-Profile: default\r\n\r\n"} {
		resolver, dialer := &fakeResolver{}, &fakeDialer{}
		server, err := New(context.Background(), "unused", profile, resolver, dialer, readyStub(true), newTestMetrics(t))
		if err != nil {
			t.Fatal(err)
		}
		response := exchangeProfileRequest(t, server, request)
		if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK {
			t.Fatal("Telegram listener rejected fixed profile")
		}
		if response.Header.Get("X-Kodex-Egress-Profile") != policy.TelegramProfileName || response.Header.Get("X-Kodex-Egress-Workload") != policy.TelegramWorkload ||
			response.Header.Get("X-Kodex-Egress-Operation") != policy.TelegramOperation || response.Header.Get("X-Kodex-Egress-Digest") != digest || resolver.calls != 0 || len(dialer.targets) != 0 {
			t.Fatal("Telegram readiness lost exact profile or performed external work")
		}
	}
	for _, request := range []string{
		"CONNECT api.github.com:443 HTTP/1.1\r\nHost: api.github.com:443\r\n\r\n",
		"CONNECT api.telegram.org:80 HTTP/1.1\r\nHost: api.telegram.org:80\r\n\r\n",
		"CONNECT api.github.com:443 HTTP/1.1\r\nHost: api.github.com:443\r\nX-Kodex-Egress-Profile: default\r\n\r\n",
	} {
		resolver, dialer := &fakeResolver{}, &fakeDialer{}
		server, err := New(context.Background(), "unused", profile, resolver, dialer, readyStub(true), newTestMetrics(t))
		if err != nil {
			t.Fatal(err)
		}
		local, peer := net.Pipe()
		done := make(chan struct{})
		go func() { server.handle(local); _ = local.Close(); close(done) }()
		_ = peer.SetDeadline(time.Now().Add(time.Second))
		_, _ = io.WriteString(peer, request)
		response, err := io.ReadAll(peer)
		_ = peer.Close()
		<-done
		if err != nil || len(response) != 0 || resolver.calls != 0 || len(dialer.targets) != 0 {
			t.Fatal("rejected Telegram target crossed zero-effect boundary", err)
		}
	}
}
