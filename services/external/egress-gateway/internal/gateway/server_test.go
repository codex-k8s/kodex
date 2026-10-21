package gateway

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
	"github.com/prometheus/client_golang/prometheus"
)

// TestHandlePinsDialToValidatedLiteralAndForwardsOriginalHello verifies DNS pinning and byte-exact TLS forwarding.
// TestHandlePinsDialToValidatedLiteralAndForwardsOriginalHello проверяет фиксацию DNS-адреса и точную передачу TLS-байтов.
func TestHandlePinsDialToValidatedLiteralAndForwardsOriginalHello(t *testing.T) {
	resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, ExpiresAt: time.Now().Add(time.Minute)}}
	dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
	server, err := New(context.Background(), "unused", fakePolicy{}, resolver, dialer, readyStub(true), newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	serverSide, clientSide := net.Pipe()
	done := make(chan struct{})
	go func() { server.handle(serverSide); close(done) }()
	request := "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n"
	if _, err := io.WriteString(clientSide, request); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(connectEstablished))
	if _, err := io.ReadFull(clientSide, response); err != nil || string(response) != connectEstablished {
		t.Fatalf("unexpected CONNECT response: %q, %v", response, err)
	}
	hello := gatewayClientHello("api.openai.com")
	go func() { _, _ = clientSide.Write(hello) }()
	upstream := <-dialer.peers
	forwarded := make([]byte, len(hello))
	if _, err := io.ReadFull(upstream, forwarded); err != nil {
		t.Fatal(err)
	}
	if string(forwarded) != string(hello) {
		t.Fatal("ClientHello was not forwarded byte-for-byte")
	}
	if resolver.calls != 1 || len(dialer.targets) != 1 || dialer.targets[0] != netip.MustParseAddrPort("93.184.216.34:443") {
		t.Fatalf("dial was not pinned to one validated literal: calls=%d targets=%v", resolver.calls, dialer.targets)
	}
	_ = clientSide.Close()
	_ = upstream.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connection handler did not join")
	}
}

// TestHandleRejectsSNIMismatchBeforeResolutionAndDial keeps mismatched TLS names outside the DNS and dial boundary.
// TestHandleRejectsSNIMismatchBeforeResolutionAndDial не допускает несовпадающее TLS-имя до разрешения DNS и соединения.
func TestHandleRejectsSNIMismatchBeforeResolutionAndDial(t *testing.T) {
	resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, ExpiresAt: time.Now().Add(time.Minute)}}
	dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
	server, _ := New(context.Background(), "unused", fakePolicy{}, resolver, dialer, readyStub(true), newTestMetrics(t))
	serverSide, clientSide := net.Pipe()
	done := make(chan struct{})
	go func() { server.handle(serverSide); close(done) }()
	_, _ = io.WriteString(clientSide, "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n")
	response := make([]byte, len(connectEstablished))
	_, _ = io.ReadFull(clientSide, response)
	_, _ = clientSide.Write(gatewayClientHello("github.com"))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rejected handler did not finish")
	}
	if resolver.calls != 0 || len(dialer.targets) != 0 {
		t.Fatalf("SNI rejection crossed zero-dial boundary: resolver=%d dial=%d", resolver.calls, len(dialer.targets))
	}
}

// TestHandleRejectsDNSFailureWithoutExternalDial ensures resolver errors cannot initiate an external connection.
// TestHandleRejectsDNSFailureWithoutExternalDial проверяет, что ошибка DNS не приводит к внешнему соединению.
func TestHandleRejectsDNSFailureWithoutExternalDial(t *testing.T) {
	resolver := &fakeResolver{err: &dnsresolver.Error{Reason: dnsresolver.ReasonTimeout}}
	dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
	server, err := New(context.Background(), "unused", fakePolicy{}, resolver, dialer, readyStub(true), newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	serverSide, clientSide := net.Pipe()
	done := make(chan struct{})
	go func() { server.handle(serverSide); close(done) }()
	_, _ = io.WriteString(clientSide, "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n")
	response := make([]byte, len(connectEstablished))
	if _, err := io.ReadFull(clientSide, response); err != nil || string(response) != connectEstablished {
		t.Fatalf("unexpected CONNECT response: %q, %v", response, err)
	}
	_, _ = clientSide.Write(gatewayClientHello("api.openai.com"))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("DNS rejection did not close the connection")
	}
	if resolver.calls != 1 || len(dialer.targets) != 0 {
		t.Fatalf("DNS rejection crossed zero-dial boundary: resolver=%d dial=%d", resolver.calls, len(dialer.targets))
	}
	_ = clientSide.Close()
}

// TestDialFallsBackAcrossAddressFamiliesWithinOneBudget checks IPv4 fallback and cancellation under one deadline.
// TestDialFallsBackAcrossAddressFamiliesWithinOneBudget проверяет переход на IPv4 и отмену в пределах одного срока.
func TestDialFallsBackAcrossAddressFamiliesWithinOneBudget(t *testing.T) {
	dialer := newDualStackDialer()
	server, err := New(context.Background(), "unused", fakePolicy{}, &fakeResolver{}, dialer, readyStub(true), newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := dnsresolver.Snapshot{
		Addresses: []netip.Addr{
			netip.MustParseAddr("2606:4700:4700::1111"),
			netip.MustParseAddr("1.1.1.1"),
		},
		ExpiresAt: time.Now().Add(time.Minute),
	}
	startedAt := time.Now()
	connection, err := server.dial(snapshot, 443, 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if elapsed := time.Since(startedAt); elapsed >= 250*time.Millisecond {
		t.Fatalf("IPv4 fallback consumed the shared dial budget: %s", elapsed)
	}
	select {
	case <-dialer.ipv6Done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("losing IPv6 dial was not cancelled")
	}
	peer := <-dialer.peers
	defer peer.Close()
}

// TestShutdownCancelsPendingClientHelloAndJoins checks that shutdown joins a stalled TLS handshake.
// TestShutdownCancelsPendingClientHelloAndJoins проверяет завершение зависшего TLS-рукопожатия при остановке.
func TestShutdownCancelsPendingClientHelloAndJoins(t *testing.T) {
	resolver := &fakeResolver{healthy: true}
	dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
	server, serveDone, client := startedServerConnection(t, resolver, dialer)
	_, _ = io.WriteString(client, "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n")
	_, _ = bufio.NewReader(client).ReadString('\n')
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
}

// TestDrainPreservesEstablishedTunnelUntilEffectCompletes keeps an accepted connection usable during drain.
// TestDrainPreservesEstablishedTunnelUntilEffectCompletes проверяет работу принятого соединения во время drain.
func TestDrainPreservesEstablishedTunnelUntilEffectCompletes(t *testing.T) {
	resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{
		Addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, ExpiresAt: time.Now().Add(time.Minute),
	}}
	dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
	server, serveDone, client := startedServerConnection(t, resolver, dialer)
	defer client.Close()
	if _, err := io.WriteString(client, "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(connectEstablished))
	if _, err := io.ReadFull(client, response); err != nil || string(response) != connectEstablished {
		t.Fatalf("unexpected CONNECT response: %q, %v", response, err)
	}
	hello := gatewayClientHello("api.openai.com")
	if _, err := client.Write(hello); err != nil {
		t.Fatal(err)
	}
	upstream := <-dialer.peers
	defer upstream.Close()
	forwarded := make([]byte, len(hello))
	if _, err := io.ReadFull(upstream, forwarded); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		server.mu.Lock()
		established := false
		for _, value := range server.active {
			established = established || value
		}
		server.mu.Unlock()
		if established {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("CONNECT tunnel did not become established")
		}
		time.Sleep(time.Millisecond)
	}
	server.Drain()
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
	_ = client.SetDeadline(time.Now().Add(time.Second))
	_ = upstream.SetDeadline(time.Now().Add(time.Second))
	if _, err := client.Write([]byte("effect")); err != nil {
		t.Fatalf("drain closed an established effect: %v", err)
	}
	payload := make([]byte, len("effect"))
	if _, err := io.ReadFull(upstream, payload); err != nil || string(payload) != "effect" {
		t.Fatalf("effect was interrupted during drain: %q, %v", payload, err)
	}
	_ = client.Close()
	_ = upstream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

// startedServerConnection starts a local listener and returns its server, Serve result, and CONNECT client.
// startedServerConnection запускает локальный listener и возвращает сервер, результат Serve и CONNECT-клиент.
func startedServerConnection(t *testing.T, resolver *fakeResolver, dialer *fakeDialer) (*Server, <-chan error, net.Conn) {
	t.Helper()
	server, err := New(context.Background(), "127.0.0.1:0", fakePolicy{}, resolver, dialer, readyStub(true), newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Listen(); err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve() }()
	client, err := net.Dial("tcp", server.Address().String())
	if err != nil {
		t.Fatal(err)
	}
	return server, serveDone, client
}

// TestShutdownDeadlineForcesEstablishedTunnelClosed checks bounded shutdown of a blocked established tunnel.
// TestShutdownDeadlineForcesEstablishedTunnelClosed проверяет ограниченную по времени остановку занятого туннеля.
func TestShutdownDeadlineForcesEstablishedTunnelClosed(t *testing.T) {
	server, err := New(context.Background(), "unused", fakePolicy{}, &fakeResolver{}, &fakeDialer{}, readyStub(true), newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	owned, peer := net.Pipe()
	defer peer.Close()
	if !server.acquire(owned) || !server.markEstablished(owned) {
		t.Fatal("test tunnel was not established")
	}
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		defer server.wait.Done()
		defer server.release(owned)
		buffer := make([]byte, 1)
		_, _ = owned.Read(buffer)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := server.Shutdown(ctx); err == nil {
		t.Fatal("stuck tunnel exceeded shutdown budget without an error")
	}
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("forced tunnel close did not join")
	}
}

// TestCompatibilityReadinessUsesEffectiveStateAndNeverDials reads readiness without touching DNS or external peers.
// TestCompatibilityReadinessUsesEffectiveStateAndNeverDials читает готовность без DNS и внешних соединений.
func TestCompatibilityReadinessUsesEffectiveStateAndNeverDials(t *testing.T) {
	for _, test := range []struct {
		ready    bool
		expected string
	}{
		{true, readinessReady},
		{false, readinessNotReady},
	} {
		resolver := &fakeResolver{}
		dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
		server, err := New(context.Background(), "unused", fakePolicy{}, resolver, dialer, readyStub(test.ready), newTestMetrics(t))
		if err != nil {
			t.Fatal(err)
		}
		serverSide, clientSide := net.Pipe()
		done := make(chan struct{})
		go func() {
			server.handle(serverSide)
			_ = serverSide.Close()
			close(done)
		}()
		_, _ = io.WriteString(clientSide, "GET /readyz HTTP/1.1\r\nHost: egress-gateway.kodex-system.svc.cluster.local:8080\r\nConnection: close\r\n\r\n")
		response, readErr := io.ReadAll(clientSide)
		_ = clientSide.Close()
		<-done
		if readErr != nil || string(response) != test.expected {
			t.Fatalf("unexpected compatibility response: %q, %v", response, readErr)
		}
		if resolver.calls != 0 || len(dialer.targets) != 0 {
			t.Fatalf("readiness crossed zero-dial boundary: resolver=%d dial=%d", resolver.calls, len(dialer.targets))
		}
	}
}

// TestCompatibilityListenerRejectsOtherPathsWithoutChangingCONNECT rejects noncanonical readiness requests without dialing.
// TestCompatibilityListenerRejectsOtherPathsWithoutChangingCONNECT отклоняет иной путь готовности без соединения.
func TestCompatibilityListenerRejectsOtherPathsWithoutChangingCONNECT(t *testing.T) {
	resolver := &fakeResolver{}
	dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
	server, err := New(context.Background(), "unused", fakePolicy{}, resolver, dialer, readyStub(true), newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	serverSide, clientSide := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.handle(serverSide)
		_ = serverSide.Close()
		close(done)
	}()
	_, _ = io.WriteString(clientSide, "GET /readyz?details=1 HTTP/1.1\r\nHost: egress-gateway.kodex-system.svc.cluster.local:8080\r\n\r\n")
	response, _ := io.ReadAll(clientSide)
	_ = clientSide.Close()
	<-done
	if len(response) != 0 || resolver.calls != 0 || len(dialer.targets) != 0 {
		t.Fatalf("unexpected rejected path effect: response=%q resolver=%d dial=%d", response, resolver.calls, len(dialer.targets))
	}
}

// TestReadinessOnlyListenerReturns503AndRejectsCONNECT keeps a degraded listener incapable of proxying traffic.
// TestReadinessOnlyListenerReturns503AndRejectsCONNECT не допускает проксирования через listener с нарушенной готовностью.
func TestReadinessOnlyListenerReturns503AndRejectsCONNECT(t *testing.T) {
	server, err := NewReadinessOnly(context.Background(), "unused", readyStub(false), newTestMetrics(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		request  string
		response string
	}{
		{"GET /readyz HTTP/1.1\r\nHost: egress-gateway.kodex-system.svc.cluster.local:8080\r\n\r\n", readinessNotReady},
		{"CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n", ""},
	} {
		serverSide, clientSide := net.Pipe()
		done := make(chan struct{})
		go func() {
			server.handle(serverSide)
			_ = serverSide.Close()
			close(done)
		}()
		_, _ = io.WriteString(clientSide, test.request)
		response, _ := io.ReadAll(clientSide)
		_ = clientSide.Close()
		<-done
		if string(response) != test.response {
			t.Fatalf("unexpected readiness-only response: %q", response)
		}
	}
}

// readyStub exposes a fixed readiness state for listener tests.
// readyStub задаёт фиксированное состояние готовности в тестах listener.
type readyStub bool

// Ready returns the fixture state without consulting runtime dependencies.
// Ready возвращает состояние заглушки без обращения к runtime-зависимостям.
func (value readyStub) Ready() (bool, string) { return bool(value), "test" }

// scopedReadyStub permits readiness for only one destination hostname.
// scopedReadyStub разрешает готовность только для одного имени назначения.
type scopedReadyStub struct{ hostname string }

// Ready exposes generic readiness while destination-specific checks remain separate.
// Ready возвращает общую готовность отдельно от проверки конкретного назначения.
func (value scopedReadyStub) Ready() (bool, string) { return true, "ready" }

// ReadyFor compares the requested destination with the allowed fixture hostname.
// ReadyFor сравнивает назначение с разрешённым именем из тестовой заглушки.
func (value scopedReadyStub) ReadyFor(hostname string) (bool, string) {
	return hostname == value.hostname, "destination readiness"
}

// TestConnectReadinessUsesExactDestination checks that a healthy host cannot authorize another CONNECT destination.
// TestConnectReadinessUsesExactDestination проверяет, что готовность одного хоста не разрешает другое CONNECT-назначение.
func TestConnectReadinessUsesExactDestination(t *testing.T) {
	server := &Server{readiness: scopedReadyStub{hostname: "healthy.example.test"}}
	if !server.readyFor("healthy.example.test") || server.readyFor("unhealthy.example.test") {
		t.Fatal("CONNECT readiness escaped destination boundary")
	}
}

// newTestMetrics isolates gateway metrics in a fresh registry for each test.
// newTestMetrics изолирует метрики шлюза в отдельном реестре для каждого теста.
func newTestMetrics(t *testing.T) *observability.Metrics {
	t.Helper()
	registry := prometheus.NewRegistry()
	metrics, err := observability.New(func(collectors ...prometheus.Collector) error {
		for _, collector := range collectors {
			if err := registry.Register(collector); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return metrics
}

// fakePolicy permits only the fixture provider endpoint and supplies bounded listener limits.
// fakePolicy разрешает только тестовую точку провайдера и задаёт ограниченные лимиты listener.
type fakePolicy struct{}

// pinnedIntegrationPolicy narrows the fixture policy to one approved literal address.
// pinnedIntegrationPolicy ограничивает тестовую политику одним разрешённым IP-адресом.
type pinnedIntegrationPolicy struct {
	fakePolicy
	address netip.Addr
}

// AllowsLiteral accepts only the configured provider host, port, and pinned address.
// AllowsLiteral принимает только заданные хост, порт и закреплённый адрес провайдера.
func (value pinnedIntegrationPolicy) AllowsLiteral(host string, port int, address netip.Addr) bool {
	return host == "api.openai.com" && port == 443 && address == value.address
}

// TLSMode requires implicit TLS for the approved provider endpoint.
// TLSMode требует неявный TLS для разрешённой точки провайдера.
func (value pinnedIntegrationPolicy) TLSMode(host string, port int) string {
	if host == "api.openai.com" && port == 443 {
		return "implicit"
	}
	return ""
}

// TestIntegrationCONNECTDialsOnlyFreshPinnedDNSIntersection rejects private or unpinned fresh DNS answers.
// TestIntegrationCONNECTDialsOnlyFreshPinnedDNSIntersection отклоняет частные и незакреплённые свежие DNS-ответы.
func TestIntegrationCONNECTDialsOnlyFreshPinnedDNSIntersection(t *testing.T) {
	pin := netip.MustParseAddr("8.8.8.8")
	newAddress := netip.MustParseAddr("9.9.9.9")
	for _, test := range []struct {
		name      string
		addresses []netip.Addr
		wantDial  bool
	}{
		{name: "overlap", addresses: []netip.Addr{pin, newAddress}, wantDial: true},
		{name: "no overlap", addresses: []netip.Addr{newAddress}, wantDial: false},
		{name: "private answer", addresses: []netip.Addr{pin, netip.MustParseAddr("10.0.0.1")}, wantDial: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{Addresses: test.addresses, ExpiresAt: time.Now().Add(time.Minute)}}
			dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
			server, err := New(context.Background(), "unused", pinnedIntegrationPolicy{address: pin}, resolver, dialer, readyStub(true), newTestMetrics(t))
			if err != nil {
				t.Fatal(err)
			}
			serverSide, clientSide := net.Pipe()
			done := make(chan struct{})
			go func() { server.handle(serverSide); close(done) }()
			defer clientSide.Close()
			_, _ = io.WriteString(clientSide, "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n")
			response := make([]byte, len(connectEstablished))
			if _, err := io.ReadFull(clientSide, response); err != nil || string(response) != connectEstablished {
				t.Fatal("CONNECT handshake failed", err)
			}
			go func() { _, _ = clientSide.Write(gatewayClientHello("api.openai.com")) }()
			if test.wantDial {
				select {
				case peer := <-dialer.peers:
					defer peer.Close()
				case <-time.After(time.Second):
					t.Fatal("pinned DNS intersection did not dial")
				}
				if len(dialer.targets) != 1 || dialer.targets[0].Addr() != pin {
					t.Fatal("unpinned DNS address reached literal dial")
				}
			} else {
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("empty pinned intersection did not close")
				}
				if len(dialer.targets) != 0 {
					t.Fatal("fresh but unpinned address reached literal dial")
				}
			}
		})
	}
}

// Allows restricts fixture traffic to the expected provider host and HTTPS port.
// Allows ограничивает тестовый трафик ожидаемым хостом провайдера и портом HTTPS.
func (fakePolicy) Allows(hostname string, port int) bool {
	return hostname == "api.openai.com" && port == 443
}

// Limits supplies small finite time and connection budgets for gateway tests.
// Limits задаёт конечные бюджеты времени и соединений для тестов шлюза.
func (fakePolicy) Limits() policy.Limits {
	return policy.Limits{
		MaximumHeaderBytes: 4096, MaximumClientHelloBytes: 64 << 10,
		MaximumConnections: 8, MaximumConnectionsPerSource: 4,
		HeaderTimeoutMilliseconds: 1000, ClientHelloTimeoutMilliseconds: 5000,
		DialTimeoutMilliseconds: 1000, IdleTimeoutMilliseconds: 1000,
		WriteTimeoutMilliseconds: 1000, ShutdownTimeoutMilliseconds: 1000,
	}
}

// fakeResolver records resolution calls while returning a controlled snapshot or error.
// fakeResolver считает обращения к DNS и возвращает заданный снимок либо ошибку.
type fakeResolver struct {
	mu       sync.Mutex
	snapshot dnsresolver.Snapshot
	err      error
	calls    int
	healthy  bool
}

// Resolve records one lookup and returns the fixture result under a mutex.
// Resolve учитывает одно разрешение имени и под mutex возвращает тестовый результат.
func (resolver *fakeResolver) Resolve(context.Context, string) (dnsresolver.Snapshot, error) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	resolver.calls++
	if resolver.err != nil {
		return dnsresolver.Snapshot{}, resolver.err
	}
	resolver.healthy = true
	return resolver.snapshot, nil
}

// Healthy reports whether the fixture resolver completed a successful lookup.
// Healthy сообщает, выполнил ли тестовый DNS resolver успешное разрешение.
func (resolver *fakeResolver) Healthy() bool { return resolver.healthy }

// fakeDialer records literal targets and connects each request to an in-memory peer.
// fakeDialer записывает IP-назначения и соединяет каждый запрос с локальной стороной net.Pipe.
type fakeDialer struct {
	mu      sync.Mutex
	targets []netip.AddrPort
	peers   chan net.Conn
}

// dualStackDialer holds IPv6 until cancellation while allowing the IPv4 fallback to connect.
// dualStackDialer задерживает IPv6 до отмены, позволяя резервному IPv4-соединению состояться.
type dualStackDialer struct {
	ipv6Started chan struct{}
	ipv6Done    chan struct{}
	peers       chan net.Conn
	startedOnce sync.Once
	doneOnce    sync.Once
}

// newDualStackDialer initializes synchronization channels for the fallback fixture.
// newDualStackDialer создаёт каналы синхронизации для теста резервного соединения.
func newDualStackDialer() *dualStackDialer {
	return &dualStackDialer{
		ipv6Started: make(chan struct{}),
		ipv6Done:    make(chan struct{}),
		peers:       make(chan net.Conn, 1),
	}
}

// DialContext blocks IPv6 and establishes IPv4 only after the competing attempt begins.
// DialContext блокирует IPv6 и устанавливает IPv4 лишь после начала конкурирующей попытки.
func (dialer *dualStackDialer) DialContext(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
	if target.Addr().Is6() {
		dialer.startedOnce.Do(func() { close(dialer.ipv6Started) })
		<-ctx.Done()
		dialer.doneOnce.Do(func() { close(dialer.ipv6Done) })
		return nil, ctx.Err()
	}
	select {
	case <-dialer.ipv6Started:
		server, peer := net.Pipe()
		dialer.peers <- peer
		return server, nil
	case <-ctx.Done():
		return nil, errors.New("IPv4 fallback was not attempted in time")
	}
}

// DialContext records the literal endpoint and returns an in-memory connection.
// DialContext сохраняет точное назначение и возвращает локальное соединение.
func (dialer *fakeDialer) DialContext(_ context.Context, target netip.AddrPort) (net.Conn, error) {
	dialer.mu.Lock()
	dialer.targets = append(dialer.targets, target)
	dialer.mu.Unlock()
	server, peer := net.Pipe()
	dialer.peers <- peer
	return server, nil
}

// gatewayClientHello builds a minimal TLS ClientHello with one SNI name for proxy tests.
// gatewayClientHello создаёт минимальный TLS ClientHello с одним SNI-именем для тестов прокси.
func gatewayClientHello(hostname string) []byte {
	body := append([]byte{3, 3}, make([]byte, 32)...)
	body = append(body, 0, 0, 2, 0x13, 0x01, 1, 0)
	name := []byte(hostname)
	listLength := 3 + len(name)
	sni := []byte{byte(listLength >> 8), byte(listLength), 0, byte(len(name) >> 8), byte(len(name))}
	sni = append(sni, name...)
	extension := []byte{0, 0, byte(len(sni) >> 8), byte(len(sni))}
	extension = append(extension, sni...)
	body = append(body, byte(len(extension)>>8), byte(len(extension)))
	body = append(body, extension...)
	handshake := []byte{1, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	handshake = append(handshake, body...)
	record := []byte{22, 3, 3, 0, 0}
	binary.BigEndian.PutUint16(record[3:], uint16(len(handshake)))
	return append(record, handshake...)
}
