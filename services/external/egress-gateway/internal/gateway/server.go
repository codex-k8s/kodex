// Package gateway связывает CONNECT, ClientHello, DNS и literal dial lifecycle.
package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/connect"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/tlshello"
)

const connectEstablished = "HTTP/1.1 200 Connection Established\r\n\r\n"

const (
	readinessReady    = "HTTP/1.1 204 No Content\r\nCache-Control: no-store\r\nConnection: close\r\nX-Content-Type-Options: nosniff\r\n\r\n"
	readinessNotReady = "HTTP/1.1 503 Service Unavailable\r\nCache-Control: no-store\r\nConnection: close\r\nX-Content-Type-Options: nosniff\r\n\r\n"
)

// AccessPolicy — минимальная immutable policy surface.
type AccessPolicy interface {
	Allows(string, int) bool
	Limits() policy.Limits
}

// MailAccess принадлежит listener, не заголовкам CONNECT; TLS остаётся у bridge.
type MailAccess interface {
	TLSMode(string, int) string
	AllowsLiteral(string, int, netip.Addr) bool
}

// Resolver возвращает только validated literal snapshots.
type Resolver interface {
	Resolve(context.Context, string) (dnsresolver.Snapshot, error)
}

// LiteralDialer запрещает hostname на границе dial.
type LiteralDialer interface {
	DialContext(context.Context, netip.AddrPort) (net.Conn, error)
}

// Readiness предоставляет тот же effective state, что technical `/readyz`.
type Readiness interface {
	Ready() (bool, string)
}

// Server владеет listener, active connections и cancel/join boundary.
type Server struct {
	address   string
	policy    AccessPolicy
	resolver  Resolver
	dialer    LiteralDialer
	readiness Readiness
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
func New(parent context.Context, address string, accessPolicy AccessPolicy, resolver Resolver, dialer LiteralDialer, readiness Readiness, metrics *observability.Metrics) (*Server, error) {
	if parent == nil || address == "" || accessPolicy == nil || resolver == nil || dialer == nil || readiness == nil || metrics == nil {
		return nil, errors.New("gateway server configuration is invalid")
	}
	limits := accessPolicy.Limits()
	lifecycleContext, cancel := context.WithCancel(parent)
	return &Server{
		address: address, policy: accessPolicy, resolver: resolver, dialer: dialer, readiness: readiness, metrics: metrics,
		context: lifecycleContext, cancel: cancel, global: make(chan struct{}, limits.MaximumConnections),
		active: make(map[net.Conn]struct{}), perSource: make(map[string]int),
	}, nil
}

// NewReadinessOnly создаёт fail-closed listener для compatibility readiness без CONNECT authority.
func NewReadinessOnly(parent context.Context, address string, readiness Readiness, metrics *observability.Metrics) (*Server, error) {
	if parent == nil || address == "" || readiness == nil || metrics == nil {
		return nil, errors.New("gateway readiness listener configuration is invalid")
	}
	accessPolicy := readinessOnlyPolicy{}
	lifecycleContext, cancel := context.WithCancel(parent)
	limits := accessPolicy.Limits()
	return &Server{
		address: address, policy: accessPolicy, readiness: readiness, metrics: metrics,
		context: lifecycleContext, cancel: cancel, global: make(chan struct{}, limits.MaximumConnections),
		active: make(map[net.Conn]struct{}), perSource: make(map[string]int),
	}, nil
}

// ShareConnectionLimit сохраняет общий бюджет до запуска обоих listener.
func (server *Server) ShareConnectionLimit(primary *Server) error {
	if primary == nil || primary == server || server.listener != nil || primary.listener != nil ||
		cap(server.global) != cap(primary.global) || server.draining.Load() || primary.draining.Load() {
		return errors.New("gateway shared connection budget is invalid")
	}
	server.global = primary.global
	return nil
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
		go func() {
			defer server.wait.Done()
			defer server.release(connection)
			server.handle(connection)
		}()
	}
}

// Drain закрывает accept и tunnels до ожидания других listener.
func (server *Server) Drain() {
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
}

// Shutdown останавливает accept, закрывает tunnels и ограниченно ожидает join.
func (server *Server) Shutdown(ctx context.Context) error {
	server.Drain()
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
	request, reader, err := connect.Parse(client, limits.MaximumHeaderBytes, duration(limits.HeaderTimeoutMilliseconds), server.policy.Allows)
	if err != nil {
		server.metrics.Connection("rejected", "connect", connectReason(err))
		return
	}
	if request.Kind == connect.KindReadiness {
		server.writeCompatibilityReadiness(client, duration(limits.WriteTimeoutMilliseconds))
		return
	}
	if ready, _ := server.readiness.Ready(); !ready || server.draining.Load() {
		server.metrics.Connection("rejected", "connect", "not_ready")
		server.writeResponse(client, readinessNotReady, duration(limits.WriteTimeoutMilliseconds))
		return
	}
	target := request.Target
	if err := client.SetWriteDeadline(time.Now().Add(duration(limits.WriteTimeoutMilliseconds))); err != nil {
		server.metrics.Connection("failed", "connect", "io")
		return
	}
	if _, err := io.WriteString(client, server.withPolicyReadback(connectEstablished)); err != nil {
		server.metrics.Connection("failed", "connect", "io")
		return
	}
	_ = client.SetWriteDeadline(time.Time{})
	var buffered []byte
	mail, isMail := server.policy.(MailAccess)
	if !isMail || mail.TLSMode(target.Hostname, target.Port) != "starttls" {
		buffered, err = tlshello.ReadAndVerify(client, reader, limits.MaximumClientHelloBytes, duration(limits.ClientHelloTimeoutMilliseconds), target.Hostname)
		if err != nil {
			server.metrics.Connection("rejected", "clienthello", tlsReason(err))
			return
		}
	}
	if ready, _ := server.readiness.Ready(); !ready || server.draining.Load() {
		server.metrics.Connection("rejected", "connect", "not_ready")
		return
	}
	snapshot, err := server.resolver.Resolve(server.context, target.Hostname)
	if err != nil {
		server.metrics.Connection("rejected", "dns", dnsReason(err))
		return
	}
	if isMail {
		for _, address := range snapshot.Addresses {
			if !mail.AllowsLiteral(target.Hostname, target.Port, address) {
				server.metrics.Connection("rejected", "connect", "policy")
				return
			}
		}
	}
	if ready, _ := server.readiness.Ready(); !ready || server.draining.Load() {
		server.metrics.Connection("rejected", "connect", "not_ready")
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
	if len(buffered) > 0 {
		if _, err := upstream.Write(buffered); err != nil {
			server.metrics.Connection("failed", "tunnel", "io")
			return
		}
	}
	_ = upstream.SetWriteDeadline(time.Time{})
	server.tunnel(client, upstream, duration(limits.IdleTimeoutMilliseconds), duration(limits.WriteTimeoutMilliseconds))
}

func (server *Server) writeCompatibilityReadiness(connection net.Conn, timeout time.Duration) {
	response := readinessNotReady
	if ready, _ := server.readiness.Ready(); ready && !server.draining.Load() {
		response = readinessReady
	}
	server.writeResponse(connection, response, timeout)
}

func (server *Server) writeResponse(connection net.Conn, response string, timeout time.Duration) {
	if err := connection.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return
	}
	_, _ = io.WriteString(connection, server.withPolicyReadback(response))
}

func (server *Server) withPolicyReadback(response string) string {
	active, ok := server.policy.(interface {
		Revision() string
		Digest() string
		ProfileIdentity() (string, string, string)
	})
	if !ok {
		return response
	}
	name, workload, operation := active.ProfileIdentity()
	headers := "X-Kodex-Egress-Revision: " + active.Revision() + "\r\n" +
		"X-Kodex-Egress-Digest: " + active.Digest() + "\r\n" +
		"X-Kodex-Egress-Profile: " + name + "\r\n"
	if workload != "" {
		headers += "X-Kodex-Egress-Workload: " + workload + "\r\n" +
			"X-Kodex-Egress-Operation: " + operation + "\r\n"
	}
	if source, ok := server.policy.(interface{ ConfigurationIdentity() (string, string) }); ok {
		revision, digest := source.ConfigurationIdentity()
		headers += "X-Kodex-Egress-Configuration-Revision: " + revision + "\r\n" +
			"X-Kodex-Egress-Configuration-Digest: " + digest + "\r\n"
	}
	return strings.TrimSuffix(response, "\r\n") + headers + "\r\n"
}

func (server *Server) dial(snapshot dnsresolver.Snapshot, port int, timeout time.Duration) (net.Conn, error) {
	if !time.Now().Before(snapshot.ExpiresAt) || dnsresolver.ValidateAddresses(snapshot.Addresses) != nil {
		return nil, errors.New("DNS snapshot is not valid for dial")
	}
	dialContext, cancel := context.WithTimeout(server.context, timeout)
	defer cancel()
	addressFamilies := make([][]netip.Addr, 0, 2)
	var ipv4, ipv6 []netip.Addr
	for _, address := range snapshot.Addresses {
		if address.Is4() {
			ipv4 = append(ipv4, address)
		} else {
			ipv6 = append(ipv6, address)
		}
	}
	if len(ipv4) > 0 {
		addressFamilies = append(addressFamilies, ipv4)
	}
	if len(ipv6) > 0 {
		addressFamilies = append(addressFamilies, ipv6)
	}
	results := make(chan dialResult)
	for _, addresses := range addressFamilies {
		go server.dialFamily(dialContext, addresses, port, results)
	}
	for range addressFamilies {
		select {
		case result := <-results:
			if result.connection != nil {
				cancel()
				return result.connection, nil
			}
		case <-dialContext.Done():
			return nil, errors.New("all literal dial attempts failed")
		}
	}
	return nil, errors.New("all literal dial attempts failed")
}

type dialResult struct {
	connection net.Conn
}

func (server *Server) dialFamily(ctx context.Context, addresses []netip.Addr, port int, results chan<- dialResult) {
	for _, address := range addresses {
		connection, err := server.dialer.DialContext(ctx, netip.AddrPortFrom(address, uint16(port)))
		if err != nil {
			server.metrics.Dial("failure", "dial_failure")
			if ctx.Err() != nil {
				return
			}
			continue
		}
		server.metrics.Dial("success", "none")
		select {
		case results <- dialResult{connection: connection}:
		case <-ctx.Done():
			_ = connection.Close()
		}
		return
	}
	select {
	case results <- dialResult{}:
	case <-ctx.Done():
	}
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
	// Add находится под тем же lock, что drain: Wait не обгонит новый handler.
	server.wait.Add(1)
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

type readinessOnlyPolicy struct{}

func (readinessOnlyPolicy) Allows(string, int) bool { return false }

func (readinessOnlyPolicy) Limits() policy.Limits {
	return policy.Limits{
		MaximumHeaderBytes: 16 << 10, MaximumClientHelloBytes: 16 << 10,
		MaximumConnections: 64, MaximumConnectionsPerSource: 16,
		HeaderTimeoutMilliseconds: 5_000, ClientHelloTimeoutMilliseconds: 5_000,
		DialTimeoutMilliseconds: 1_000, IdleTimeoutMilliseconds: 1_000,
		WriteTimeoutMilliseconds: 5_000, ShutdownTimeoutMilliseconds: 5_000,
	}
}
