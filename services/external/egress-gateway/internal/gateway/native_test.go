package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/integrationpolicy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// nativeFixturePolicy loads a real strict projection containing only one synthetic private target.
// nativeFixturePolicy загружает настоящую строгую проекцию с одной synthetic private целью.
func nativeFixturePolicy(t *testing.T) *integrationpolicy.NativeActive {
	t.Helper()
	path := "../../../../../deploy/k8s/base/egress-gateway/policy.json"
	digest, err := policy.DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	base, err := policy.LoadFile(path, "2026-09-05.1", digest)
	if err != nil {
		t.Fatal(err)
	}
	document := shared.NativeDocument{Schema: shared.NativeSchema, Generation: 1, GatewayPolicyDigest: base.Digest(), Destinations: []shared.NativeDestination{{Hostname: "gitlab.example.invalid", Port: 443, AddressCIDRs: []string{"10.77.0.4/32"}}}}
	raw, _ := json.Marshal(document)
	active, err := integrationpolicy.LoadNative(raw, document.Digest(), base)
	if err != nil {
		t.Fatal(err)
	}
	return active
}

// TestNativeCONNECTUsesOnlyOperatorPinsAfterExactHostAndSNI proves the real policy bypasses
// ambient DNS only for its registered literal; a different target or SNI cannot cause a dial.
// TestNativeCONNECTUsesOnlyOperatorPinsAfterExactHostAndSNI доказывает обход ambient DNS
// лишь для зарегистрированного literal; другая цель либо SNI не вызывает dial.
func TestNativeCONNECTUsesOnlyOperatorPinsAfterExactHostAndSNI(t *testing.T) {
	for _, scenario := range []struct {
		name, host, sni string
		allowed         bool
	}{{"allowed", "gitlab.example.invalid", "gitlab.example.invalid", true}, {"wrong-sni", "gitlab.example.invalid", "other.example.invalid", false}, {"wrong-host", "other.example.invalid", "other.example.invalid", false}} {
		t.Run(scenario.name, func(t *testing.T) {
			active := nativeFixturePolicy(t)
			resolver := &fakeResolver{}
			dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
			server, err := New(t.Context(), "unused", active, resolver, dialer, active, newTestMetrics(t))
			if err != nil {
				t.Fatal(err)
			}
			serverSide, client := net.Pipe()
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			done := make(chan struct{})
			go func() { server.handle(serverSide); close(done) }()
			_, _ = io.WriteString(client, "CONNECT "+scenario.host+":443 HTTP/1.1\r\nHost: "+scenario.host+":443\r\n\r\n")
			response, err := http.ReadResponse(bufio.NewReader(client), &http.Request{Method: http.MethodConnect})
			if scenario.host == "gitlab.example.invalid" {
				if err != nil || response.StatusCode != 200 || response.Header.Get("X-Kodex-Egress-Digest") != active.Digest() {
					t.Fatal("native CONNECT readback mismatch", err)
				}
				go func() { _, _ = client.Write(gatewayClientHello(scenario.sni)) }()
			}
			if scenario.allowed {
				select {
				case upstream := <-dialer.peers:
					forwarded := make([]byte, len(gatewayClientHello(scenario.sni)))
					if _, err := io.ReadFull(upstream, forwarded); err != nil {
						t.Fatal(err)
					}
					if string(forwarded) != string(gatewayClientHello(scenario.sni)) {
						t.Fatal("native proxy changed ClientHello")
					}
					_ = upstream.Close()
				case <-time.After(3 * time.Second):
					t.Fatal("native exact route did not dial")
				}
			}
			_ = client.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("native connection did not join")
			}
			if resolver.calls != 0 || scenario.allowed && (len(dialer.targets) != 1 || dialer.targets[0] != netip.MustParseAddrPort("10.77.0.4:443")) || !scenario.allowed && len(dialer.targets) != 0 {
				t.Fatal("native CONNECT crossed exact literal boundary")
			}
		})
	}
}

// TestInvalidNativeProjectionSharesBudgetWithoutStoppingCore preserves core capacity while
// the optional listener stays closed, including invalid or absent projected policy files.
// TestInvalidNativeProjectionSharesBudgetWithoutStoppingCore сохраняет core capacity при
// закрытом optional listener, включая неверный либо отсутствующий projected policy файл.
func TestInvalidNativeProjectionSharesBudgetWithoutStoppingCore(t *testing.T) {
	active := nativeFixturePolicy(t)
	core, err := New(context.Background(), "unused", active, &fakeResolver{}, &fakeDialer{}, active, newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	var unavailable *integrationpolicy.NativeActive
	closed, err := NewReadinessOnly(context.Background(), "unused", unavailable, newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.ShareConnectionLimit(core); err != nil || closed.global != core.global {
		t.Fatal("optional native failure changed core capacity", err)
	}
	response := exchangeProfileRequest(t, closed, "GET /readyz HTTP/1.1\r\nHost: native.example.invalid:8085\r\n\r\n")
	if response.StatusCode != 503 {
		t.Fatal("invalid native projection is ready")
	}
}

// malformedNativePolicy simulates a policy implementation that violates its route contract.
// It must not be able to trigger the public-DNS fallback even if Allows returned true.
// malformedNativePolicy имитирует policy с нарушенным route contract. Она не должна
// запускать public-DNS fallback даже после положительного Allows.
type malformedNativePolicy struct {
	fakePolicy
	route *shared.NativeDestination
}

// NativeIntegrationRoute deliberately returns the malformed fixture route for a closed-boundary test.
// NativeIntegrationRoute намеренно возвращает неверный fixture route для проверки закрытой границы.
func (p malformedNativePolicy) NativeIntegrationRoute(string) *shared.NativeDestination {
	return p.route
}

// TestMalformedNativeRouteNeverFallsBackToDNS validates the second boundary after ClientHello,
// preventing nil, foreign host/port and broad-CIDR routes from reaching any resolver or dialer.
// TestMalformedNativeRouteNeverFallsBackToDNS проверяет вторую границу после ClientHello,
// не допуская nil, чужие host/port и широкие CIDR до resolver либо dialer.
func TestMalformedNativeRouteNeverFallsBackToDNS(t *testing.T) {
	for _, route := range []*shared.NativeDestination{nil, {Hostname: "other.example.invalid", Port: 443, AddressCIDRs: []string{"10.77.0.4/32"}}, {Hostname: "api.openai.com", Port: 80, AddressCIDRs: []string{"10.77.0.4/32"}}, {Hostname: "api.openai.com", Port: 443, AddressCIDRs: []string{"10.77.0.0/24"}}} {
		resolver := &fakeResolver{}
		dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
		server, err := New(t.Context(), "unused", malformedNativePolicy{route: route}, resolver, dialer, readyStub(true), newTestMetrics(t))
		if err != nil {
			t.Fatal(err)
		}
		serverSide, client := net.Pipe()
		_ = client.SetDeadline(time.Now().Add(2 * time.Second))
		done := make(chan struct{})
		go func() { server.handle(serverSide); close(done) }()
		_, _ = io.WriteString(client, "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n")
		if response, err := http.ReadResponse(bufio.NewReader(client), &http.Request{Method: http.MethodConnect}); err != nil || response.StatusCode != 200 {
			t.Fatal("fixture CONNECT failed", err)
		}
		_, _ = client.Write(gatewayClientHello("api.openai.com"))
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("malformed native route did not close")
		}
		if resolver.calls != 0 || len(dialer.targets) != 0 {
			t.Fatal("malformed native route reached fallback")
		}
	}
}
