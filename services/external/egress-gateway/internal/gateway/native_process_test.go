package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// nativeProcessResolver forbids ambient DNS in the synthetic cross-process fixture.
// nativeProcessResolver запрещает ambient DNS в synthetic межпроцессной fixture.
type nativeProcessResolver struct{}

// Resolve refuses every request, making accidental DNS fallback fail the HTTPS scenario.
// Resolve отклоняет каждый запрос, чтобы случайный DNS fallback провалил HTTPS-сценарий.
func (nativeProcessResolver) Resolve(context.Context, string) (dnsresolver.Snapshot, error) {
	return dnsresolver.Snapshot{}, errors.New("fixture ambient DNS is forbidden")
}

// nativeProcessDialer maps one asserted synthetic operator pin to a local TLS fixture only.
// It is compiled exclusively into tests and is absent from the shipped gateway binary.
// nativeProcessDialer отображает один проверенный synthetic pin только в локальную TLS fixture.
// Он собирается исключительно в тестах и отсутствует в поставляемом gateway binary.
type nativeProcessDialer struct{ upstream string }

// DialContext rejects any different address or port before connecting to the loopback fixture.
// DialContext отклоняет другой адрес либо port до соединения с loopback fixture.
func (d nativeProcessDialer) DialContext(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
	if target != netip.MustParseAddrPort("10.77.0.4:443") {
		return nil, errors.New("fixture literal pin mismatch")
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", d.upstream)
}

// telegramProcessResolver supplies one public synthetic literal only for the fixed Telegram DNS name; ambient DNS is never called.
// telegramProcessResolver возвращает один public synthetic literal только для fixed Telegram DNS name; ambient DNS не вызывается.
type telegramProcessResolver struct{}

// Resolve rejects another hostname and preserves the production public-address and expiry checks before the test-only loopback mapping.
// Resolve отклоняет другое имя и сохраняет production public-address и expiry checks до test-only loopback mapping.
func (telegramProcessResolver) Resolve(_ context.Context, host string) (dnsresolver.Snapshot, error) {
	if host != "api.telegram.org" {
		return dnsresolver.Snapshot{}, errors.New("fixture Telegram DNS target mismatch")
	}
	return dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

// telegramProcessDialer maps only the validated synthetic public literal to the exact local TLS provider in the test binary.
// telegramProcessDialer отображает только проверенный synthetic public literal в точный local TLS provider тестового binary.
type telegramProcessDialer struct{ upstream string }

// DialContext forbids every other literal or port; this mapping is absent from the production gateway binary.
// DialContext запрещает любой иной literal либо port; mapping отсутствует в production gateway binary.
func (dialer telegramProcessDialer) DialContext(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
	if target != netip.MustParseAddrPort("93.184.216.34:443") {
		return nil, errors.New("fixture Telegram literal pin mismatch")
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", dialer.upstream)
}

// TestNativeGatewayProcessFixture runs the real gateway in a separate test-only process for
// the integration adapter's HTTPS scenario. Only a canonical loopback upstream is accepted.
// TestNativeGatewayProcessFixture запускает настоящий gateway в отдельном test-only процессе
// для HTTPS-сценария integration adapter. Допустим лишь canonical loopback upstream.
func TestNativeGatewayProcessFixture(t *testing.T) {
	if os.Getenv("KODEX_TEST_NATIVE_GATEWAY_PROCESS") != "synthetic-only" {
		t.Skip("cross-process fixture is started by the native HTTPS qualification")
	}
	var input struct {
		Upstream string `json:"upstream"`
		Profile  string `json:"profile"`
	}
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		t.Fatal("invalid fixture input")
	}
	address, err := netip.ParseAddrPort(input.Upstream)
	if err != nil || !address.Addr().IsLoopback() || address.Port() == 0 || address.String() != input.Upstream {
		t.Fatal("fixture upstream must be exact loopback")
	}
	var server *Server
	var digest string
	if input.Profile == "telegram" {
		path := filepath.Join("..", "..", "..", "..", "..", "deploy", "k8s", "base", "egress-gateway", "telegram-policy.json")
		digest, err = policy.DigestFile(path)
		if err != nil {
			t.Fatal("read Telegram fixture policy")
		}
		active, loadErr := policy.LoadFile(path, "2026-09-05.1", digest)
		if loadErr != nil {
			t.Fatal("load Telegram fixture policy")
		}
		profile, profileErr := active.ForProfile(policy.TelegramProfileName)
		if profileErr != nil {
			t.Fatal("select Telegram fixture profile")
		}
		server, err = New(t.Context(), "127.0.0.1:0", profile, telegramProcessResolver{}, telegramProcessDialer{upstream: input.Upstream}, readyStub(true), newTestMetrics(t))
	} else if input.Profile == "" {
		active := nativeFixturePolicy(t)
		digest = active.Digest()
		server, err = New(t.Context(), "127.0.0.1:0", active, nativeProcessResolver{}, nativeProcessDialer{upstream: input.Upstream}, active, newTestMetrics(t))
	} else {
		t.Fatal("unknown fixture profile")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Listen(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve() }()
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"listen": server.Address().String(), "digest": digest}); err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("fixture server did not join")
	}
}
