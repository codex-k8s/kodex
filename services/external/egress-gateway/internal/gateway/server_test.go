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

func TestShutdownCancelsPendingClientHelloAndJoins(t *testing.T) {
	resolver := &fakeResolver{healthy: true}
	dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
	server, err := New(context.Background(), "127.0.0.1:0", fakePolicy{}, resolver, dialer, readyStub(true), newTestMetrics(t))
	if err != nil || server.Listen() != nil {
		t.Fatalf("listen failed: %v", err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve() }()
	client, err := net.Dial("tcp", server.Address().String())
	if err != nil {
		t.Fatal(err)
	}
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

func TestDrainPreservesEstablishedTunnelUntilEffectCompletes(t *testing.T) {
	resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{
		Addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, ExpiresAt: time.Now().Add(time.Minute),
	}}
	dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
	server, err := New(context.Background(), "127.0.0.1:0", fakePolicy{}, resolver, dialer, readyStub(true), newTestMetrics(t))
	if err != nil || server.Listen() != nil {
		t.Fatalf("listen failed: %v", err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve() }()
	client, err := net.Dial("tcp", server.Address().String())
	if err != nil {
		t.Fatal(err)
	}
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

type readyStub bool

func (value readyStub) Ready() (bool, string) { return bool(value), "test" }

type scopedReadyStub struct{ hostname string }

func (value scopedReadyStub) Ready() (bool, string) { return true, "ready" }
func (value scopedReadyStub) ReadyFor(hostname string) (bool, string) {
	return hostname == value.hostname, "destination readiness"
}

func TestConnectReadinessUsesExactDestination(t *testing.T) {
	server := &Server{readiness: scopedReadyStub{hostname: "healthy.example.test"}}
	if !server.readyFor("healthy.example.test") || server.readyFor("unhealthy.example.test") {
		t.Fatal("CONNECT readiness escaped destination boundary")
	}
}

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

type fakePolicy struct{}

type pinnedIntegrationPolicy struct {
	fakePolicy
	address netip.Addr
}

func (value pinnedIntegrationPolicy) AllowsLiteral(host string, port int, address netip.Addr) bool {
	return host == "api.openai.com" && port == 443 && address == value.address
}

func (value pinnedIntegrationPolicy) TLSMode(host string, port int) string {
	if host == "api.openai.com" && port == 443 {
		return "implicit"
	}
	return ""
}

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

func (fakePolicy) Allows(hostname string, port int) bool {
	return hostname == "api.openai.com" && port == 443
}
func (fakePolicy) Limits() policy.Limits {
	return policy.Limits{
		MaximumHeaderBytes: 4096, MaximumClientHelloBytes: 64 << 10,
		MaximumConnections: 8, MaximumConnectionsPerSource: 4,
		HeaderTimeoutMilliseconds: 1000, ClientHelloTimeoutMilliseconds: 5000,
		DialTimeoutMilliseconds: 1000, IdleTimeoutMilliseconds: 1000,
		WriteTimeoutMilliseconds: 1000, ShutdownTimeoutMilliseconds: 1000,
	}
}

type fakeResolver struct {
	mu       sync.Mutex
	snapshot dnsresolver.Snapshot
	err      error
	calls    int
	healthy  bool
}

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
func (resolver *fakeResolver) Healthy() bool { return resolver.healthy }

type fakeDialer struct {
	mu      sync.Mutex
	targets []netip.AddrPort
	peers   chan net.Conn
}

type dualStackDialer struct {
	ipv6Started chan struct{}
	ipv6Done    chan struct{}
	peers       chan net.Conn
	startedOnce sync.Once
	doneOnce    sync.Once
}

func newDualStackDialer() *dualStackDialer {
	return &dualStackDialer{
		ipv6Started: make(chan struct{}),
		ipv6Done:    make(chan struct{}),
		peers:       make(chan net.Conn, 1),
	}
}

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

func (dialer *fakeDialer) DialContext(_ context.Context, target netip.AddrPort) (net.Conn, error) {
	dialer.mu.Lock()
	dialer.targets = append(dialer.targets, target)
	dialer.mu.Unlock()
	server, peer := net.Pipe()
	dialer.peers <- peer
	return server, nil
}

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
