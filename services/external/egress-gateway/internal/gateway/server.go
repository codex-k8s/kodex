// Package gateway связывает CONNECT, ClientHello, DNS и literal dial lifecycle.
package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/connect"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/dnsresolver"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/policy"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/tlshello"
)

const connectEstablished = "HTTP/1.1 200 Connection Established\r\n\r\n"

// AccessPolicy — минимальная immutable policy surface.
type AccessPolicy interface {
	Allows(string, int) bool
	Limits() policy.Limits
}

// Resolver возвращает только validated literal snapshots.
type Resolver interface {
	Resolve(context.Context, string) (dnsresolver.Snapshot, error)
}

// LiteralDialer запрещает hostname на границе dial.
type LiteralDialer interface {
	DialContext(context.Context, netip.AddrPort) (net.Conn, error)
}

// Server владеет listener, active connections и cancel/join boundary.
type Server struct {
	address   string
	policy    AccessPolicy
	resolver  Resolver
	dialer    LiteralDialer
	metrics   *observability.Metrics
	context   context.Context
	cancel    context.CancelFunc
	listener  net.Listener
	draining  atomic.Bool
	global    chan struct{}
	wait      sync.WaitGroup
	mu        sync.Mutex
	active    map[net.Conn]struct{}
	perSource map[string]int
}

// New создаёт CONNECT server без фоновых goroutine.
func New(parent context.Context, address string, accessPolicy AccessPolicy, resolver Resolver, dialer LiteralDialer, metrics *observability.Metrics) (*Server, error) {
	if parent == nil || address == "" || accessPolicy == nil || resolver == nil || dialer == nil || metrics == nil {
		return nil, errors.New("gateway server configuration is invalid")
	}
	limits := accessPolicy.Limits()
	lifecycleContext, cancel := context.WithCancel(parent)
	return &Server{
		address: address, policy: accessPolicy, resolver: resolver, dialer: dialer, metrics: metrics,
		context: lifecycleContext, cancel: cancel, global: make(chan struct{}, limits.MaximumConnections),
		active: make(map[net.Conn]struct{}), perSource: make(map[string]int),
	}, nil
}

// Listen резервирует listener до readiness barrier.
func (server *Server) Listen() error {
	if server.listener != nil {
		return errors.New("gateway listener lifecycle is invalid")
	}
	listener, err := net.Listen("tcp", server.address)
	if err != nil {
		return errors.New("listen CONNECT server")
	}
	server.listener = listener
	return nil
}

// Address возвращает фактический listen address для targeted tests.
func (server *Server) Address() net.Addr {
	if server.listener == nil {
		return nil
	}
	return server.listener.Addr()
}

// Serve принимает соединения до drain.
func (server *Server) Serve() error {
	if server.listener == nil {
		return errors.New("CONNECT server is not listening")
	}
	for {
		connection, err := server.listener.Accept()
		if err != nil {
			if server.draining.Load() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return errors.New("accept CONNECT connection")
		}
		if !server.acquire(connection) {
			server.metrics.Connection("rejected", "accept", "connection_limit")
			_ = connection.Close()
			continue
		}
		server.wait.Add(1)
		go func() {
			defer server.wait.Done()
			defer server.release(connection)
			server.handle(connection)
		}()
	}
}

// Shutdown останавливает accept, закрывает tunnels и ограниченно ожидает join.
func (server *Server) Shutdown(ctx context.Context) error {
	server.draining.Store(true)
	server.cancel()
	if server.listener != nil {
		_ = server.listener.Close()
	}
	server.mu.Lock()
	for connection := range server.active {
		_ = connection.Close()
	}
	server.mu.Unlock()
	done := make(chan struct{})
	go func() {
		server.wait.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return errors.New("gateway connection join deadline exceeded")
	case <-done:
		return nil
	}
}

func (server *Server) handle(client net.Conn) {
	limits := server.policy.Limits()
	target, reader, err := connect.Parse(client, limits.MaximumHeaderBytes, duration(limits.HeaderTimeoutMilliseconds), server.policy.Allows)
	if err != nil {
		server.metrics.Connection("rejected", "connect", connectReason(err))
		return
	}
	if err := client.SetWriteDeadline(time.Now().Add(duration(limits.WriteTimeoutMilliseconds))); err != nil {
		server.metrics.Connection("failed", "connect", "io")
		return
	}
	if _, err := io.WriteString(client, connectEstablished); err != nil {
		server.metrics.Connection("failed", "connect", "io")
		return
	}
	_ = client.SetWriteDeadline(time.Time{})
	buffered, err := tlshello.ReadAndVerify(client, reader, limits.MaximumClientHelloBytes, duration(limits.ClientHelloTimeoutMilliseconds), target.Hostname)
	if err != nil {
		server.metrics.Connection("rejected", "clienthello", tlsReason(err))
		return
	}
	snapshot, err := server.resolver.Resolve(server.context, target.Hostname)
	if err != nil {
		server.metrics.Connection("rejected", "dns", dnsReason(err))
		return
	}
	upstream, err := server.dial(snapshot, target.Port, duration(limits.DialTimeoutMilliseconds))
	if err != nil {
		server.metrics.Connection("failed", "dial", "dial_failure")
		return
	}
	defer upstream.Close()
	if err := upstream.SetWriteDeadline(time.Now().Add(duration(limits.WriteTimeoutMilliseconds))); err != nil {
		server.metrics.Connection("failed", "tunnel", "io")
		return
	}
	if _, err := upstream.Write(buffered); err != nil {
		server.metrics.Connection("failed", "tunnel", "io")
		return
	}
	_ = upstream.SetWriteDeadline(time.Time{})
	server.tunnel(client, upstream, duration(limits.IdleTimeoutMilliseconds), duration(limits.WriteTimeoutMilliseconds))
}

func (server *Server) dial(snapshot dnsresolver.Snapshot, port int, timeout time.Duration) (net.Conn, error) {
	if !time.Now().Before(snapshot.ExpiresAt) || dnsresolver.ValidateAddresses(snapshot.Addresses) != nil {
		return nil, errors.New("DNS snapshot is not valid for dial")
	}
	dialContext, cancel := context.WithTimeout(server.context, timeout)
	defer cancel()
	for _, address := range snapshot.Addresses {
		if dnsresolver.ValidateAddresses([]netip.Addr{address}) != nil {
			return nil, errors.New("DNS address is not valid for dial")
		}
		connection, err := server.dialer.DialContext(dialContext, netip.AddrPortFrom(address, uint16(port)))
		if err == nil {
			server.metrics.Dial("success", "none")
			return connection, nil
		}
		server.metrics.Dial("failure", "dial_failure")
		if dialContext.Err() != nil {
			break
		}
	}
	return nil, errors.New("all literal dial attempts failed")
}

func (server *Server) tunnel(left, right net.Conn, idleTimeout, writeTimeout time.Duration) {
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = left.Close()
			_ = right.Close()
		})
	}
	results := make(chan error, 2)
	refreshIdle := func() {
		deadline := time.Now().Add(idleTimeout)
		_ = left.SetReadDeadline(deadline)
		_ = right.SetReadDeadline(deadline)
	}
	refreshIdle()
	go func() { results <- pump(right, left, idleTimeout, writeTimeout, refreshIdle) }()
	go func() { results <- pump(left, right, idleTimeout, writeTimeout, refreshIdle) }()
	first := <-results
	closeBoth()
	second := <-results
	if !benignTunnelClose(first) || !benignTunnelClose(second) {
		server.metrics.Connection("failed", "tunnel", "io")
		return
	}
	server.metrics.Connection("completed", "tunnel", "none")
}

func pump(destination, source net.Conn, idleTimeout, writeTimeout time.Duration, refreshIdle func()) error {
	buffer := make([]byte, 32<<10)
	for {
		if err := source.SetReadDeadline(time.Now().Add(idleTimeout)); err != nil {
			return err
		}
		read, err := source.Read(buffer)
		if read > 0 {
			refreshIdle()
			if deadlineErr := destination.SetWriteDeadline(time.Now().Add(writeTimeout)); deadlineErr != nil {
				return deadlineErr
			}
			written := 0
			for written < read {
				count, writeErr := destination.Write(buffer[written:read])
				written += count
				if writeErr != nil {
					return writeErr
				}
			}
		}
		if err != nil {
			return err
		}
	}
}

func benignTunnelClose(err error) bool {
	return err == nil || errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF)
}

func (server *Server) acquire(connection net.Conn) bool {
	if server.draining.Load() {
		return false
	}
	select {
	case server.global <- struct{}{}:
	default:
		return false
	}
	source := sourceKey(connection.RemoteAddr())
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.draining.Load() {
		<-server.global
		return false
	}
	if server.perSource[source] >= server.policy.Limits().MaximumConnectionsPerSource {
		<-server.global
		return false
	}
	server.perSource[source]++
	server.active[connection] = struct{}{}
	server.metrics.AddActive(1)
	return true
}

func (server *Server) release(connection net.Conn) {
	_ = connection.Close()
	source := sourceKey(connection.RemoteAddr())
	server.mu.Lock()
	delete(server.active, connection)
	server.perSource[source]--
	if server.perSource[source] == 0 {
		delete(server.perSource, source)
	}
	server.mu.Unlock()
	<-server.global
	server.metrics.AddActive(-1)
}

func sourceKey(address net.Addr) string {
	if address == nil {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return "unknown"
	}
	parsed, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	return parsed.Unmap().String()
}

func duration(milliseconds int) time.Duration { return time.Duration(milliseconds) * time.Millisecond }

func connectReason(err error) string {
	var value *connect.Error
	if errors.As(err, &value) {
		return string(value.Reason)
	}
	return "malformed"
}

func tlsReason(err error) string {
	var value *tlshello.Error
	if errors.As(err, &value) {
		return string(value.Reason)
	}
	return "malformed"
}

func dnsReason(err error) string {
	var value *dnsresolver.Error
	if errors.As(err, &value) {
		return string(value.Reason)
	}
	return "malformed"
}

// NetDialer реализует literal-only TCP dial.
type NetDialer struct{ Dialer net.Dialer }

// DialContext передаёт net.Dialer только literal AddrPort string.
func (dialer *NetDialer) DialContext(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
	return dialer.Dialer.DialContext(ctx, "tcp", target.String())
}
