// Package gateway связывает CONNECT, ClientHello, DNS и literal dial lifecycle.
package gateway

import (
	"bufio"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/connect"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/tlshello"
)

// connectEstablished acknowledges a CONNECT tunnel only after policy and readiness checks.
// connectEstablished подтверждает CONNECT-туннель только после проверок policy и готовности.
const connectEstablished = "HTTP/1.1 200 Connection Established\r\n\r\n"

// proxyAuthenticationRequired challenges an unauthenticated client without granting CONNECT authority.
// proxyAuthenticationRequired запрашивает аутентификацию, не предоставляя полномочий CONNECT.
const proxyAuthenticationRequired = "HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"kodex-runtime\"\r\nContent-Length: 0\r\nCache-Control: no-store\r\nConnection: close\r\nX-Content-Type-Options: nosniff\r\n\r\n"

const (
	// readinessReady is the no-body compatibility response for a ready listener.
	// readinessReady — совместимый ответ без тела для готового listener.
	readinessReady = "HTTP/1.1 204 No Content\r\nCache-Control: no-store\r\nConnection: close\r\nX-Content-Type-Options: nosniff\r\n\r\n"
	// readinessNotReady is the fail-closed response while the listener cannot accept traffic.
	// readinessNotReady — закрытый отказ, пока listener не может принимать трафик.
	readinessNotReady = "HTTP/1.1 503 Service Unavailable\r\nCache-Control: no-store\r\nConnection: close\r\nX-Content-Type-Options: nosniff\r\n\r\n"
)

// AccessPolicy exposes only destination eligibility and immutable connection limits.
// AccessPolicy предоставляет только допуск назначения и неизменяемые лимиты соединений.
type AccessPolicy interface {
	// Allows checks an unauthenticated hostname and port against the active policy.
	// Allows проверяет хост и порт неаутентифицированного соединения по активной policy.
	Allows(string, int) bool
	// Limits returns the fixed budgets used throughout one connection lifecycle.
	// Limits возвращает фиксированные лимиты на весь жизненный цикл соединения.
	Limits() policy.Limits
}

// AuthenticatedAccessPolicy requires an exact signed runtime credential for each requested destination.
// AuthenticatedAccessPolicy требует точный подписанный runtime-реквизит для каждого назначения.
type AuthenticatedAccessPolicy interface {
	AccessPolicy
	// AuthorizeAuthenticated derives scoped proxy access from the verified credential and target.
	// AuthorizeAuthenticated выводит ограниченный доступ прокси из проверенного реквизита и цели.
	AuthorizeAuthenticated(string, int, string) (runtimecontract.RuntimeProxyAccess, bool)
}

// MailAccess belongs only to the mail listener and requires complete DNS pinning for its target.
// MailAccess принадлежит только почтовому listener и требует полного закрепления DNS-адресов цели.
// A generic TLS mode alone does not turn another profile into a mail profile.
// Общий TLS-режим сам по себе не превращает иной профиль в почтовый.
type MailAccess interface {
	// TLSMode selects implicit TLS or STARTTLS behavior for the exact mail endpoint.
	// TLSMode выбирает implicit TLS или STARTTLS для точной почтовой точки.
	TLSMode(string, int) string
	// AllowsLiteral checks each resolved address against the mail policy.
	// AllowsLiteral проверяет каждый разрешённый адрес по почтовой policy.
	AllowsLiteral(string, int, netip.Addr) bool
	// RequireCompleteDNSPinning marks the policy as requiring every DNS answer to be allowed.
	// RequireCompleteDNSPinning отмечает требование разрешить каждый адрес DNS-ответа.
	RequireCompleteDNSPinning()
}

// literalAccess restricts each dial target to a previously approved IP address.
// literalAccess ограничивает назначение dial ранее разрешённым IP-адресом.
type literalAccess interface {
	// AllowsLiteral checks a resolved address without treating its hostname as proof of authority.
	// AllowsLiteral проверяет адрес без признания одного лишь имени доказательством полномочий.
	AllowsLiteral(string, int, netip.Addr) bool
}

// Resolver returns validated literal-address snapshots with an expiry.
// Resolver возвращает проверенные снимки IP-адресов со сроком действия.
type Resolver interface {
	// Resolve obtains one current DNS snapshot for a destination name.
	// Resolve получает один актуальный DNS-снимок для имени назначения.
	Resolve(context.Context, string) (dnsresolver.Snapshot, error)
}

// LiteralDialer forbids hostname resolution at the final connection boundary.
// LiteralDialer запрещает разрешение имени на конечной границе соединения.
type LiteralDialer interface {
	// DialContext connects to an already validated literal address and port.
	// DialContext соединяется с уже проверенными точными IP-адресом и портом.
	DialContext(context.Context, netip.AddrPort) (net.Conn, error)
}

// Readiness exposes the same effective state as the technical readiness endpoint.
// Readiness предоставляет то же эффективное состояние, что техническая точка готовности.
type Readiness interface {
	// Ready reports whether the listener may serve traffic and a bounded reason.
	// Ready сообщает, может ли listener обслуживать трафик, и ограниченную причину.
	Ready() (bool, string)
}

// destinationReadiness optionally narrows readiness to one requested hostname.
// destinationReadiness при необходимости сужает готовность до запрошенного имени.
type destinationReadiness interface {
	// ReadyFor reports effective readiness for the exact destination hostname.
	// ReadyFor сообщает эффективную готовность для точного имени назначения.
	ReadyFor(string) (bool, string)
}

// readyFor prefers destination-specific readiness and falls back to overall listener readiness.
// readyFor предпочитает готовность конкретной цели и иначе проверяет общую готовность listener.
func (server *Server) readyFor(host string) bool {
	if scoped, ok := server.readiness.(destinationReadiness); ok {
		ready, _ := scoped.ReadyFor(host)
		return ready
	}
	ready, _ := server.readiness.Ready()
	return ready
}

// Server owns the listener, tracked connections, and their cancel-and-join boundary.
// Server владеет listener, учтёнными соединениями и границей их отмены и завершения.
type Server struct {
	address       string
	policy        AccessPolicy
	resolver      Resolver
	dialer        LiteralDialer
	readiness     Readiness
	metrics       *observability.Metrics
	context       context.Context
	cancel        context.CancelFunc
	listener      net.Listener
	draining      atomic.Bool
	global        chan struct{}
	wait          sync.WaitGroup
	mu            sync.Mutex
	active        map[net.Conn]bool
	perSource     map[string]int
	authenticated bool
	interceptCA   *TLSInterceptAuthority
	upstreamRoots *x509.CertPool
}

// New validates required dependencies and creates a CONNECT server without starting workers.
// New проверяет обязательные зависимости и создаёт CONNECT-сервер без запуска workers.
func New(parent context.Context, address string, accessPolicy AccessPolicy, resolver Resolver, dialer LiteralDialer, readiness Readiness, metrics *observability.Metrics) (*Server, error) {
	if parent == nil || address == "" || accessPolicy == nil || resolver == nil || dialer == nil || readiness == nil || metrics == nil {
		return nil, errors.New("gateway server configuration is invalid")
	}
	limits := accessPolicy.Limits()
	lifecycleContext, cancel := context.WithCancel(parent)
	return &Server{
		address: address, policy: accessPolicy, resolver: resolver, dialer: dialer, readiness: readiness, metrics: metrics,
		context: lifecycleContext, cancel: cancel, global: make(chan struct{}, limits.MaximumConnections),
		active: make(map[net.Conn]bool), perSource: make(map[string]int),
	}, nil
}

// NewAuthenticated creates a separate CONNECT listener requiring a runtime credential and TLS authority.
// NewAuthenticated создаёт отдельный CONNECT-listener с обязательными runtime-реквизитом и TLS authority.
// It never falls back to the unauthenticated static policy.
// Он никогда не понижает полномочия до неаутентифицированной статической policy.
func NewAuthenticated(parent context.Context, address string, accessPolicy AuthenticatedAccessPolicy, resolver Resolver, dialer LiteralDialer, readiness Readiness, metrics *observability.Metrics, interceptCA *TLSInterceptAuthority) (*Server, error) {
	if interceptCA == nil {
		return nil, errors.New("authenticated gateway TLS interception authority is missing")
	}
	server, err := New(parent, address, accessPolicy, resolver, dialer, readiness, metrics)
	if err != nil {
		return nil, err
	}
	server.authenticated = true
	server.interceptCA = interceptCA
	return server, nil
}

// NewReadinessOnly creates a compatibility readiness listener that can never authorize CONNECT.
// NewReadinessOnly создаёт совместимый listener готовности без права разрешать CONNECT.
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
		active: make(map[net.Conn]bool), perSource: make(map[string]int),
	}, nil
}

// ShareConnectionLimit makes two idle listeners share one connection budget before either starts.
// ShareConnectionLimit задаёт двум ещё не запущенным listener общий лимит соединений.
func (server *Server) ShareConnectionLimit(primary *Server) error {
	if primary == nil || primary == server || server.listener != nil || primary.listener != nil ||
		cap(server.global) != cap(primary.global) || server.draining.Load() || primary.draining.Load() {
		return errors.New("gateway shared connection budget is invalid")
	}
	server.global = primary.global
	return nil
}

// Listen reserves the TCP listener before the readiness barrier is released.
// Listen резервирует TCP-listener до открытия барьера готовности.
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

// Address returns the bound address or nil before Listen, including for targeted tests.
// Address возвращает фактический адрес либо nil до Listen, в том числе для точечных тестов.
func (server *Server) Address() net.Addr {
	if server.listener == nil {
		return nil
	}
	return server.listener.Addr()
}

// Serve accepts connections until drain, enforcing global and per-source limits before spawning handlers.
// Serve принимает соединения до drain и проверяет общий и адресный лимиты до запуска обработчиков.
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

// Drain closes the listener and pending handshakes while leaving established tunnels to a bounded shutdown.
// Drain закрывает listener и незавершённые рукопожатия, сохраняя действующие туннели до ограниченной остановки.
func (server *Server) Drain() {
	server.draining.Store(true)
	if server.listener != nil {
		_ = server.listener.Close()
	}
	server.mu.Lock()
	for connection, established := range server.active {
		if !established {
			_ = connection.Close()
		}
	}
	server.mu.Unlock()
}

// Shutdown joins tracked tunnels within the caller budget and force-closes them only after expiry.
// Shutdown ожидает учтённые туннели в пределах бюджета и принудительно закрывает их лишь по истечении срока.
func (server *Server) Shutdown(ctx context.Context) error {
	server.Drain()
	done := make(chan struct{})
	go func() {
		server.wait.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		server.cancel()
		server.mu.Lock()
		for connection := range server.active {
			_ = connection.Close()
		}
		server.mu.Unlock()
		return errors.New("gateway connection join deadline exceeded")
	case <-done:
		server.cancel()
		return nil
	}
}

// handle validates one CONNECT request, rechecks readiness, pins DNS, and owns the resulting tunnel.
// handle проверяет один CONNECT-запрос, повторно сверяет готовность, закрепляет DNS и ведёт туннель.
func (server *Server) handle(client net.Conn) {
	limits := server.policy.Limits()
	var request connect.Request
	var reader *bufio.Reader
	var err error
	var webAccess runtimecontract.RuntimeProxyAccess
	if server.authenticated {
		request, reader, err = connect.ParseAuthenticated(client, limits.MaximumHeaderBytes, duration(limits.HeaderTimeoutMilliseconds), func(hostname string, port int, credential string) bool {
			var allowed bool
			webAccess, allowed = server.policy.(AuthenticatedAccessPolicy).AuthorizeAuthenticated(hostname, port, credential)
			return allowed
		})
	} else {
		request, reader, err = connect.Parse(client, limits.MaximumHeaderBytes, duration(limits.HeaderTimeoutMilliseconds), server.policy.Allows)
	}
	if err != nil {
		server.metrics.Connection("rejected", "connect", connectReason(err))
		var rejected *connect.Error
		if server.authenticated && errors.As(err, &rejected) && rejected.Reason == connect.ReasonAuthenticationRequired {
			// Git/libcurl сначала узнаёт способ proxy authentication без grant.
			// Challenge не разрешает CONNECT: новый запрос проходит все проверки.
			server.writeResponse(client, proxyAuthenticationRequired, duration(limits.WriteTimeoutMilliseconds))
		}
		return
	}
	if request.Kind == connect.KindReadiness {
		server.writeCompatibilityReadiness(client, duration(limits.WriteTimeoutMilliseconds))
		return
	}
	if !server.readyFor(request.Target.Hostname) || server.draining.Load() {
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
	if server.authenticated {
		server.proxyTLS(client, reader, target, webAccess, limits)
		return
	}
	var buffered []byte
	mail, isMail := server.policy.(MailAccess)
	if !isMail || mail.TLSMode(target.Hostname, target.Port) != "starttls" {
		buffered, err = tlshello.ReadAndVerify(client, reader, limits.MaximumClientHelloBytes, duration(limits.ClientHelloTimeoutMilliseconds), target.Hostname)
		if err != nil {
			server.metrics.Connection("rejected", "clienthello", tlsReason(err))
			return
		}
	}
	if !server.readyFor(target.Hostname) || server.draining.Load() {
		server.metrics.Connection("rejected", "connect", "not_ready")
		return
	}
	snapshot, err := server.resolver.Resolve(server.context, target.Hostname)
	if err != nil {
		server.metrics.Connection("rejected", "dns", dnsReason(err))
		return
	}
	if err := dnsresolver.ValidateAddresses(snapshot.Addresses); err != nil {
		server.metrics.Connection("rejected", "dns", dnsReason(err))
		return
	}
	if !time.Now().Before(snapshot.ExpiresAt) {
		server.metrics.Connection("rejected", "dns", "timeout")
		return
	}
	if pinned, ok := server.policy.(literalAccess); ok {
		permitted := make([]netip.Addr, 0, len(snapshot.Addresses))
		for _, address := range snapshot.Addresses {
			if pinned.AllowsLiteral(target.Hostname, target.Port, address) {
				permitted = append(permitted, address)
			}
		}
		// Mail сохраняет полный DNS snapshot; динамический OpenAPI-origin
		// может использовать только пересечение свежего DNS с ранее
		// проверенными публичными адресами immutable policy.
		if len(permitted) == 0 || isMail && len(permitted) != len(snapshot.Addresses) {
			server.metrics.Connection("rejected", "connect", "policy")
			return
		}
		snapshot.Addresses = permitted
	}
	if !server.readyFor(target.Hostname) || server.draining.Load() {
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
	if !server.markEstablished(client) {
		server.metrics.Connection("rejected", "tunnel", "not_ready")
		return
	}
	server.tunnel(client, upstream, duration(limits.IdleTimeoutMilliseconds), duration(limits.WriteTimeoutMilliseconds))
}

// writeCompatibilityReadiness returns effective readiness without granting proxy authority.
// writeCompatibilityReadiness возвращает эффективную готовность без предоставления прав прокси.
func (server *Server) writeCompatibilityReadiness(connection net.Conn, timeout time.Duration) {
	response := readinessNotReady
	if ready, _ := server.readiness.Ready(); ready && !server.draining.Load() {
		response = readinessReady
	}
	server.writeResponse(connection, response, timeout)
}

// writeResponse applies the response write deadline and attaches active policy readback when available.
// writeResponse ограничивает запись ответа сроком и при наличии добавляет readback активной policy.
func (server *Server) writeResponse(connection net.Conn, response string, timeout time.Duration) {
	if err := connection.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return
	}
	_, _ = io.WriteString(connection, server.withPolicyReadback(response))
}

// withPolicyReadback adds revision, digest, and scoped identity headers without changing a policy-free response.
// withPolicyReadback добавляет revision, digest и профильные заголовки, не меняя ответ без policy.
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

// dial races validated IPv4 and IPv6 literals under one deadline and cancels losing attempts.
// dial конкурирует проверенными IPv4- и IPv6-адресами в одном сроке и отменяет проигравшие попытки.
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

// dialResult carries one successful literal connection or an exhausted address family.
// dialResult переносит успешное соединение с IP-адресом либо исчерпание семейства адресов.
type dialResult struct {
	connection net.Conn
}

// dialFamily tries one address family sequentially and closes a late connection after cancellation.
// dialFamily последовательно пробует одно семейство адресов и закрывает запоздалое соединение после отмены.
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

// tunnel copies both directions until one ends, closes both peers, and joins the other pump before reporting.
// tunnel копирует оба направления до завершения одного, закрывает стороны и дожидается второго pump.
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

// pump copies one tunnel direction with refreshed read and write deadlines, including partial writes.
// pump копирует одно направление туннеля с обновлением сроков чтения и записи, включая частичные записи.
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

// benignTunnelClose treats ordinary EOF and close races as expected terminal outcomes.
// benignTunnelClose считает обычные EOF и гонки закрытия ожидаемыми конечными исходами.
func benignTunnelClose(err error) bool {
	return err == nil || errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF)
}

// acquire reserves global and per-source capacity and tracks a handler atomically with drain.
// acquire резервирует общий и адресный лимиты и учитывает обработчик атомарно относительно drain.
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
	server.active[connection] = false
	// Add находится под тем же lock, что drain: Wait не обгонит новый handler.
	server.wait.Add(1)
	server.metrics.AddActive(1)
	return true
}

// markEstablished protects an admitted tunnel from drain's pending-handshake close path.
// markEstablished защищает допущенный туннель от закрытия drain для незавершённых рукопожатий.
func (server *Server) markEstablished(connection net.Conn) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.draining.Load() {
		return false
	}
	if _, tracked := server.active[connection]; tracked {
		server.active[connection] = true
	}
	return true
}

// release closes and untracks one client while returning its global and per-source capacity.
// release закрывает и снимает с учёта клиента, возвращая общий и адресный лимиты.
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

// sourceKey normalizes an IP source for per-source quotas and maps malformed addresses to one bounded key.
// sourceKey нормализует IP-источник для адресных квот, а неверный адрес сводит к одному ограниченному ключу.
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

// duration converts configured millisecond budgets into time.Duration without changing their value.
// duration переводит настроенные миллисекундные лимиты в time.Duration без изменения значения.
func duration(milliseconds int) time.Duration { return time.Duration(milliseconds) * time.Millisecond }

// connectReason reduces parser errors to a closed metric reason without exposing the credential.
// connectReason сводит ошибки парсера к закрытой метке метрики без раскрытия реквизита.
func connectReason(err error) string {
	var value *connect.Error
	if errors.As(err, &value) {
		if value.Reason == connect.ReasonAuthenticationRequired {
			return string(connect.ReasonCredentials)
		}
		return string(value.Reason)
	}
	return "malformed"
}

// tlsReason maps ClientHello validation failures to bounded metric reasons.
// tlsReason отображает ошибки проверки ClientHello в ограниченные причины метрик.
func tlsReason(err error) string {
	var value *tlshello.Error
	if errors.As(err, &value) {
		return string(value.Reason)
	}
	return "malformed"
}

// dnsReason maps resolver failures to bounded metric reasons without leaking DNS details.
// dnsReason отображает ошибки DNS в ограниченные причины метрик без раскрытия деталей DNS.
func dnsReason(err error) string {
	var value *dnsresolver.Error
	if errors.As(err, &value) {
		return string(value.Reason)
	}
	return "malformed"
}

// NetDialer performs TCP dials only to already selected literal address-port pairs.
// NetDialer устанавливает TCP-соединения только с уже выбранными точными IP-адресом и портом.
type NetDialer struct{ Dialer net.Dialer }

// DialContext passes the literal AddrPort string to net.Dialer without hostname lookup.
// DialContext передаёт net.Dialer точный AddrPort без разрешения имени.
func (dialer *NetDialer) DialContext(ctx context.Context, target netip.AddrPort) (net.Conn, error) {
	return dialer.Dialer.DialContext(ctx, "tcp", target.String())
}

// readinessOnlyPolicy provides finite listener budgets while denying every CONNECT destination.
// readinessOnlyPolicy задаёт конечные лимиты listener, запрещая любое CONNECT-назначение.
type readinessOnlyPolicy struct{}

// Allows always denies proxy access on the compatibility readiness listener.
// Allows всегда запрещает доступ прокси через совместимый listener готовности.
func (readinessOnlyPolicy) Allows(string, int) bool { return false }

// Limits bounds parsing, connections, and shutdown even on the readiness-only listener.
// Limits ограничивает парсинг, соединения и остановку даже для listener только с готовностью.
func (readinessOnlyPolicy) Limits() policy.Limits {
	return policy.Limits{
		MaximumHeaderBytes: 16 << 10, MaximumClientHelloBytes: 16 << 10,
		MaximumConnections: 64, MaximumConnectionsPerSource: 16,
		HeaderTimeoutMilliseconds: 5_000, ClientHelloTimeoutMilliseconds: 5_000,
		DialTimeoutMilliseconds: 1_000, IdleTimeoutMilliseconds: 1_000,
		WriteTimeoutMilliseconds: 5_000, ShutdownTimeoutMilliseconds: 5_000,
	}
}
