package kubernetestarget

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/moby/spdystream"
	spdyframe "github.com/moby/spdystream/spdy"
	"k8s.io/apimachinery/pkg/util/httpstream"
	apispdy "k8s.io/apimachinery/pkg/util/httpstream/spdy"
	clientspdy "k8s.io/client-go/transport/spdy"
)

const (
	// maximumSPDYHeaders bounds decompressed metadata independently from encrypted wire and application bytes.
	// maximumSPDYHeaders ограничивает decompressed metadata независимо от encrypted wire и application bytes.
	maximumSPDYHeaders = 32
	// maximumSPDYHeaderField bounds every decoded header name/value; the SDK's data frame limit remains sixteen MiB.
	// maximumSPDYHeaderField ограничивает каждое decoded header name/value; data frame limit SDK остаётся шестнадцать MiB.
	maximumSPDYHeaderField = 4096
	// maximumSPDYControlBytes rejects oversized compressed control frames before decompression.
	// maximumSPDYControlBytes отклоняет oversized compressed control frames до decompression.
	maximumSPDYControlBytes = 64 << 10
)

// nativeSPDYConnection owns the standard pinned multiplexor and positively joins its Serve loop and shutdown.
// Only the fixed admission initializer may create streams; remote push streams cancel the connection.
// nativeSPDYConnection владеет standard pinned multiplexor и положительно присоединяет Serve loop и shutdown.
// Streams создаёт лишь fixed admission initializer; remote push streams отменяют connection.
type nativeSPDYConnection struct {
	connection           *spdystream.Connection
	raw                  net.Conn
	ctx                  context.Context
	cancel               context.CancelFunc
	admissionUntil       time.Time
	maximumStreams       int
	mu                   sync.Mutex
	streams              []*spdystream.Stream
	initialized          bool
	closeOnce            sync.Once
	serveDone, watchDone chan struct{}
	handshake            managedkubernetes.HandshakeReceipt
}

// CreateStream opens one bounded fixed logical stream before the admission deadline, without retry.
// CreateStream открывает один bounded fixed logical stream до admission deadline без retry.
func (stream *nativeSPDYConnection) CreateStream(headers http.Header) (httpstream.Stream, error) {
	native, err := stream.createStream(headers, stream.admissionUntil, false)
	if err != nil {
		return nil, err
	}
	return native, nil
}

// createStream admits dynamic pairs only for the explicitly pinned port-forward profile and one current INPUT deadline.
// createStream допускает dynamic pairs лишь для explicitly pinned port-forward profile и одного current INPUT deadline.
func (stream *nativeSPDYConnection) createStream(headers http.Header, until time.Time, dynamic bool) (*spdystream.Stream, error) {
	stream.mu.Lock()
	hard, bounded := stream.ctx.Deadline()
	if dynamic && (!bounded || until.After(hard)) {
		stream.mu.Unlock()
		return nil, errNativeStreamUnknown
	}
	if dynamic && stream.handshake.Protocol != string(PrivilegedPortForwardSPDYV1) || stream.initialized && !dynamic || len(stream.streams) >= stream.maximumStreams || stream.ctx.Err() != nil || !until.After(time.Now()) {
		stream.mu.Unlock()
		return nil, errNativeStreamUnknown
	}
	// The only callers initialize their fixed headers serially before the transport is published.
	// Единственные callers последовательно инициализируют fixed headers до публикации transport.
	native, err := stream.connection.CreateStream(headers.Clone(), nil, false)
	if err == nil {
		stream.streams = append(stream.streams, native)
	}
	stream.mu.Unlock()
	if err != nil {
		return nil, errNativeStreamUnknown
	}
	remaining := time.Until(until)
	if remaining <= 0 || native.WaitTimeout(remaining) != nil || stream.ctx.Err() != nil {
		_ = native.Reset()
		return nil, errNativeStreamUnknown
	}
	return native, nil
}

// shutdown closes the socket before resetting streams, then drains pending SDK reset acknowledgements and joins every owned worker.
// The pinned SDK sends a pre-reply reset through an unbuffered waiter; a timed-out opener must not strand that frame worker.
// shutdown закрывает socket до reset streams, затем дренирует pending SDK reset acknowledgements и присоединяет owned workers.
// Pinned SDK отправляет pre-reply reset через unbuffered waiter; истёкший opener не должен оставлять frame worker заблокированным.
func (stream *nativeSPDYConnection) shutdown() {
	stream.closeOnce.Do(func() {
		stream.cancel()
		_ = stream.raw.Close()
		stream.mu.Lock()
		var resetWaiters sync.WaitGroup
		for _, native := range stream.streams {
			resetWaiters.Add(1)
			go func() {
				defer resetWaiters.Done()
				for native.WaitTimeout(10*time.Millisecond) == spdystream.ErrTimeout {
					select {
					case <-stream.serveDone:
						return
					default:
					}
				}
			}()
			_ = native.Reset()
		}
		stream.mu.Unlock()
		_ = stream.connection.Close()
		_ = stream.connection.Wait(0)
		<-stream.serveDone
		resetWaiters.Wait()
	})
}

// Close cancels the original connection and joins the watcher without asserting remote process termination.
// Close отменяет original connection и присоединяет watcher без утверждения remote process termination.
func (stream *nativeSPDYConnection) Close() error { stream.shutdown(); <-stream.watchDone; return nil }

// CloseChan exposes only the standard transport closure signal, never an owner terminal receipt.
// CloseChan выдаёт лишь standard transport closure signal, не owner terminal receipt.
func (stream *nativeSPDYConnection) CloseChan() <-chan bool { return stream.connection.CloseChan() }

// SetIdleTimeout retains the SDK's transport limit; the immutable socket deadline still bounds the complete lifetime.
// SetIdleTimeout сохраняет transport limit SDK; immutable socket deadline ограничивает полный lifetime.
func (stream *nativeSPDYConnection) SetIdleTimeout(timeout time.Duration) {
	stream.connection.SetIdleTimeout(timeout)
}

// RemoveStreams resets only already owned streams; no new operation or connection is created.
// RemoveStreams сбрасывает лишь уже owned streams; новая operation либо connection не создаётся.
func (stream *nativeSPDYConnection) RemoveStreams(streams ...httpstream.Stream) {
	for _, native := range streams {
		if native != nil {
			_ = native.Reset()
		}
	}
}

// HandshakeReceipt attaches only the original owner origin to observed TLS and negotiated SPDY metadata.
// HandshakeReceipt добавляет лишь original owner origin к observed TLS и negotiated SPDY metadata.
func (stream *nativeSPDYConnection) HandshakeReceipt(origin managedkubernetes.StreamReceiptOrigin) (managedkubernetes.HandshakeReceipt, error) {
	receipt := stream.handshake
	receipt.Origin = origin
	if origin.Operation != stream.handshake.Origin.Operation || origin.TargetRef != stream.handshake.Origin.TargetRef || origin.TargetRevision != stream.handshake.Origin.TargetRevision || receipt.Validate() != nil {
		return managedkubernetes.HandshakeReceipt{}, errNativeStreamUnknown
	}
	return receipt, nil
}

// String conceals private stream buffers, identities and endpoints.
// String скрывает private stream buffers, identities и endpoints.
func (*nativeSPDYConnection) String() string { return "[managed Kubernetes native SPDY transport]" }

// GoString preserves redaction during detailed formatting.
// GoString сохраняет redaction при подробном форматировании.
func (stream *nativeSPDYConnection) GoString() string { return stream.String() }

// spdyBufferedConn preserves HTTP parser read-ahead for the standard SPDY decoder.
// spdyBufferedConn сохраняет HTTP parser read-ahead для standard SPDY decoder.
type spdyBufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

// Read drains already buffered wire bytes before reading the pinned socket.
// Read выдаёт уже buffered wire bytes до чтения pinned socket.
func (connection *spdyBufferedConn) Read(raw []byte) (int, error) { return connection.reader.Read(raw) }

// nativeSPDYUpgrade layers bounded HTTP and framer options over the pinned SDK Dial and Negotiate APIs.
// It avoids stock Upgrader's unbounded error-body read and large decompressed-header defaults.
// nativeSPDYUpgrade добавляет bounded HTTP и framer options к pinned SDK Dial и Negotiate APIs.
// Исключает unbounded error-body read и большие decompressed-header defaults stock Upgrader.
type nativeSPDYUpgrade struct {
	dialer                     *apispdy.SpdyRoundTripper
	ctx                        context.Context
	cancel                     context.CancelFunc
	admissionUntil             time.Time
	method, endpoint, protocol string
	maximumStreams             int
	attempted                  atomic.Bool
	raw                        net.Conn
	buffered                   *bufio.Reader
	peerDigest                 string
	ready                      *nativeSPDYConnection
	handshake                  managedkubernetes.HandshakeReceipt
}

// RoundTrip makes one exact POST upgrade and rejects redirects, protocol aliases and bodies before SDK connection creation.
// RoundTrip выполняет один exact POST upgrade и отклоняет redirects, protocol aliases и bodies до SDK connection creation.
func (upgrade *nativeSPDYUpgrade) RoundTrip(request *http.Request) (*http.Response, error) {
	if !upgrade.attempted.CompareAndSwap(false, true) || request.Method != upgrade.method || request.URL.String() != upgrade.endpoint || request.Body != nil || len(request.Header.Values(httpstream.HeaderProtocolVersion)) != 1 || request.Header.Get(httpstream.HeaderProtocolVersion) != upgrade.protocol {
		return nil, errNativeStreamUnknown
	}
	owned := request.Clone(request.Context())
	owned.Header = request.Header.Clone()
	owned.Header.Set(httpstream.HeaderConnection, httpstream.HeaderUpgrade)
	owned.Header.Set(httpstream.HeaderUpgrade, apispdy.HeaderSpdy31)
	connection, err := upgrade.dialer.Dial(owned)
	if err != nil {
		return nil, errNativeStreamUnknown
	}
	upgrade.raw = connection
	tlsConnection, ok := connection.(*tls.Conn)
	if !ok {
		_ = connection.Close()
		return nil, errNativeStreamUnknown
	}
	state := tlsConnection.ConnectionState()
	if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
		_ = connection.Close()
		return nil, errNativeStreamUnknown
	}
	peer := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
	upgrade.peerDigest = hex.EncodeToString(peer[:])
	limited := &io.LimitedReader{R: connection, N: maximumSPDYControlBytes + 1}
	upgrade.buffered = bufio.NewReader(limited)
	response, err := http.ReadResponse(upgrade.buffered, owned)
	if err != nil || limited.N <= 0 || response.StatusCode != http.StatusSwitchingProtocols || !strings.EqualFold(response.Header.Get(httpstream.HeaderConnection), httpstream.HeaderUpgrade) || !strings.EqualFold(response.Header.Get(httpstream.HeaderUpgrade), apispdy.HeaderSpdy31) || len(response.Header.Values(httpstream.HeaderProtocolVersion)) != 1 || response.Header.Get(httpstream.HeaderProtocolVersion) != upgrade.protocol || response.Header.Get("Content-Encoding") != "" || response.ContentLength > 0 {
		_ = connection.Close()
		return nil, errNativeStreamUnknown
	}
	limited.N = 1 << 62
	response.Body = http.NoBody
	return response, nil
}

// NewConnection starts the bounded standard multiplexor only after the exact successful HTTP negotiation.
// NewConnection запускает bounded standard multiplexor лишь после exact successful HTTP negotiation.
func (upgrade *nativeSPDYUpgrade) NewConnection(response *http.Response) (httpstream.Connection, error) {
	if upgrade.raw == nil || upgrade.buffered == nil || upgrade.ready != nil || upgrade.ctx.Err() != nil || response == nil || response.StatusCode != http.StatusSwitchingProtocols || response.Header.Get(httpstream.HeaderProtocolVersion) != upgrade.protocol {
		return nil, errNativeStreamUnknown
	}
	raw := &spdyBufferedConn{Conn: upgrade.raw, reader: upgrade.buffered}
	connection, err := spdystream.NewConnectionWithOptions(raw, false, spdyframe.WithMaxControlFramePayloadSize(maximumSPDYControlBytes), spdyframe.WithMaxHeaderCount(maximumSPDYHeaders), spdyframe.WithMaxHeaderFieldSize(maximumSPDYHeaderField))
	if err != nil {
		return nil, errNativeStreamUnknown
	}
	receipt := upgrade.handshake
	receipt.PeerSPKISHA256 = upgrade.peerDigest
	native := &nativeSPDYConnection{connection: connection, raw: raw, ctx: upgrade.ctx, cancel: upgrade.cancel, admissionUntil: upgrade.admissionUntil, maximumStreams: upgrade.maximumStreams, serveDone: make(chan struct{}), watchDone: make(chan struct{}), handshake: receipt}
	go func() {
		defer close(native.serveDone)
		connection.Serve(func(push *spdystream.Stream) { native.cancel(); _ = native.raw.Close(); _ = push.Reset() })
		native.cancel()
	}()
	go func() { defer close(native.watchDone); <-native.ctx.Done(); native.shutdown() }()
	upgrade.ready = native
	return native, nil
}

// validNativeSPDYOperation preserves the explicit registered protocol and operation pairing.
// validNativeSPDYOperation сохраняет explicit registered пару protocol и operation.
func validNativeSPDYOperation(operation string, protocol PrivilegedProtocol) bool {
	switch operation {
	case "kubernetes.exec", "kubernetes.attach", "kubernetes.cp.upload", "kubernetes.cp.download":
		return protocol == PrivilegedSPDYV4
	case "kubernetes.port-forward":
		return protocol == PrivilegedPortForwardSPDYV1
	default:
		return false
	}
}

// openPodSPDY performs one owner-admitted pinned SDK upgrade, initializes fixed streams, then hands lifetime to the worker.
// No credentials, generic flags, replay or automatic transport/version fallback are introduced.
// openPodSPDY выполняет один owner-admitted pinned SDK upgrade, инициализирует fixed streams и передаёт lifetime worker.
// Credentials, generic flags, replay либо automatic transport/version fallback не добавляются.
func (credential Credential) openPodSPDY(ctx, attempt context.Context, pin PrivilegedTransportPin, now time.Time, operation string, target PodStreamTarget, options PodStreamOptions, expiresAt, admissionUntil time.Time, maximum uint64, streams int, initialize func(*nativeSPDYConnection) error) (*nativeSPDYConnection, error) {
	if ctx == nil || attempt == nil || ctx.Err() != nil || attempt.Err() != nil || !admissionUntil.After(now) || admissionUntil.After(expiresAt) || !expiresAt.After(now) || expiresAt.After(pin.ExpiresAt) || expiresAt.Sub(now) > maximumNativeDuration || maximum == 0 || maximum > managedkubernetes.MaximumStreamBytes || streams < 1 || streams > 32 || initialize == nil || !validNativeSPDYOperation(operation, pin.Protocol) {
		return nil, errNativeStreamUnknown
	}
	request, _, err := credential.StreamRequest(ctx, pin, now, operation, target, options)
	if err != nil || request.Method != http.MethodPost {
		return nil, errNativeStreamUnknown
	}
	client, closeIdle, err := credential.newPrivilegedClient(pin, now, maximumNativeDuration)
	if err != nil {
		return nil, errNativeStreamUnknown
	}
	defer closeIdle()
	wrapper, ok := client.Transport.(*credentialTransport)
	if !ok {
		return nil, errNativeStreamUnknown
	}
	transport, ok := wrapper.base.(*http.Transport)
	if !ok || transport.DialContext == nil || transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify || transport.Proxy != nil {
		return nil, errNativeStreamUnknown
	}
	transport = transport.Clone()
	lifetime, cancel := context.WithDeadline(ctx, expiresAt)
	admission, finishAdmission := context.WithDeadline(lifetime, admissionUntil)
	defer finishAdmission()
	attemptDone := make(chan struct{})
	stopAttempt := context.AfterFunc(attempt, func() { finishAdmission(); close(attemptDone) })
	defer func() {
		if !stopAttempt() {
			<-attemptDone
		}
	}()
	var wire *nativeWireConn
	var stopWire func() bool
	var wireDone chan struct{}
	defer func() {
		if stopWire != nil && !stopWire() {
			<-wireDone
		}
	}()
	dial := transport.DialContext
	transport.DialContext = func(_ context.Context, network, address string) (net.Conn, error) {
		if wire != nil {
			return nil, errNativeStreamUnknown
		}
		raw, err := dial(admission, network, address)
		if err != nil {
			return nil, errNativeStreamUnknown
		}
		wire = &nativeWireConn{Conn: raw, maximum: maximum*2 + maximumNativeHandshakeBytes}
		if raw.SetDeadline(admissionUntil) != nil {
			_ = raw.Close()
			return nil, errNativeStreamUnknown
		}
		wireDone = make(chan struct{})
		stopWire = context.AfterFunc(admission, func() { _ = raw.Close(); close(wireDone) })
		return wire, nil
	}
	stock, err := apispdy.NewRoundTripperWithConfig(apispdy.RoundTripperConfig{UpgradeTransport: transport})
	if err != nil {
		cancel()
		return nil, errNativeStreamUnknown
	}
	upgrade := &nativeSPDYUpgrade{dialer: stock, ctx: lifetime, cancel: cancel, admissionUntil: admissionUntil, method: request.Method, endpoint: request.URL.String(), protocol: pin.Protocol.WireProtocol(), maximumStreams: streams, handshake: managedkubernetes.HandshakeReceipt{Origin: managedkubernetes.StreamReceiptOrigin{Operation: operation, TargetRef: credential.target.ID, TargetRevision: int64(credential.target.Revision)}, TransportRef: pin.Ref, TransportGeneration: int64(pin.Generation), Protocol: string(pin.Protocol), HTTPStatus: 101, NegotiatedProtocol: pin.Protocol.WireProtocol()}}
	if credential.token != "" {
		request.Header.Set("Authorization", "Bearer "+credential.token)
	}
	request = request.WithContext(admission)
	negotiated, protocol, err := clientspdy.Negotiate(upgrade, &http.Client{Transport: upgrade, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, request, pin.Protocol.WireProtocol())
	if err == nil && (negotiated != upgrade.ready || protocol != pin.Protocol.WireProtocol() || wire == nil) {
		err = errNativeStreamUnknown
	}
	if err == nil {
		wire.upgraded.Store(true)
		err = initialize(upgrade.ready)
	}
	if err != nil || attempt.Err() != nil || admission.Err() != nil || !admissionUntil.After(time.Now()) || upgrade.ready == nil || upgrade.ready.ctx.Err() != nil {
		cancel()
		if upgrade.ready != nil {
			_ = upgrade.ready.Close()
		} else if upgrade.raw != nil {
			_ = upgrade.raw.Close()
		} else if wire != nil {
			_ = wire.Close()
		}
		return nil, errNativeStreamUnknown
	}
	upgrade.ready.mu.Lock()
	upgrade.ready.initialized = true
	upgrade.ready.mu.Unlock()
	if upgrade.raw.SetDeadline(expiresAt) != nil {
		_ = upgrade.ready.Close()
		return nil, errNativeStreamUnknown
	}
	return upgrade.ready, nil
}

// forgetForwardStreams removes already-reset joined pairs from the bounded active tracker; it does not reopen or reuse SDK stream IDs.
// forgetForwardStreams удаляет already-reset joined pairs из bounded active tracker; не открывает и не переиспользует SDK stream IDs.
func (stream *nativeSPDYConnection) forgetForwardStreams(left, right *spdystream.Stream) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	owned := stream.streams[:0]
	for _, entry := range stream.streams {
		if entry != left && entry != right {
			owned = append(owned, entry)
		}
	}
	clear(stream.streams[len(owned):])
	stream.streams = owned
}
