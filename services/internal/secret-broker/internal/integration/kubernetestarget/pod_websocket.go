package kubernetestarget

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/gorilla/websocket"
)

// nativePodWebSocket owns the shared pinned TLS upgrade and cancellation watcher, without a protocol parser.
// nativePodWebSocket владеет общими pinned TLS upgrade и cancellation watcher без protocol parser.
type nativePodWebSocket struct {
	connection *websocket.Conn
	ctx        context.Context
	cancel     context.CancelFunc
	joined     chan struct{}
	closeOnce  sync.Once
	handshake  managedkubernetes.HandshakeReceipt
}

// validNativeWebSocketOperation closes operation/protocol pairs before any credential-bearing dial.
// validNativeWebSocketOperation закрывает пары operation/protocol до любого dial с credentials.
func validNativeWebSocketOperation(operation string, protocol PrivilegedProtocol) bool {
	switch operation {
	case "kubernetes.exec", "kubernetes.attach", "kubernetes.cp.upload", "kubernetes.cp.download":
		return protocol == PrivilegedWebSocketV5
	case "kubernetes.port-forward":
		return protocol == PrivilegedPortForwardWebSocketV4
	default:
		return false
	}
}

// openPodWebSocket opens one exact owner-selected Pod stream, preserving the original identity and positive watcher join.
// The caller owns fresh admission and RBAC; neither this dial nor its TLS receipt supplies execution authority.
// openPodWebSocket открывает один exact owner-selected Pod stream с исходной identity и positive watcher join.
// Caller владеет fresh admission и RBAC; dial и TLS receipt не назначают execution authority.
func (credential Credential) openPodWebSocket(ctx, attempt context.Context, pin PrivilegedTransportPin, now time.Time, operation string,
	target PodStreamTarget, options PodStreamOptions, expiresAt, admissionUntil time.Time, maximum uint64) (*nativePodWebSocket, error) {
	if ctx == nil || attempt == nil || ctx.Err() != nil || attempt.Err() != nil || !admissionUntil.After(now) || admissionUntil.After(expiresAt) || !expiresAt.After(now) || expiresAt.After(pin.ExpiresAt) ||
		expiresAt.Sub(now) > maximumNativeDuration || maximum == 0 || maximum > managedkubernetes.MaximumStreamBytes {
		return nil, errNativeStreamUnknown
	}
	request, _, err := credential.StreamRequest(ctx, pin, now, operation, target, options)
	if err != nil || request.Method != http.MethodGet || !validNativeWebSocketOperation(operation, pin.Protocol) {
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
	if !ok || transport.DialContext == nil || transport.TLSClientConfig == nil || transport.Proxy != nil {
		return nil, errNativeStreamUnknown
	}
	streamContext, cancel := context.WithDeadline(ctx, expiresAt)
	admissionContext, finishAdmission := context.WithDeadline(streamContext, admissionUntil)
	defer finishAdmission()
	// The unary attempt cancels only admission; an accepted transport belongs to the worker lifetime.
	// Unary attempt отменяет только admission; принятый transport принадлежит lifetime worker.
	attemptJoined := make(chan struct{})
	stopAttempt := context.AfterFunc(attempt, func() {
		finishAdmission()
		close(attemptJoined)
	})
	defer func() {
		if !stopAttempt() {
			<-attemptJoined
		}
	}()
	var wire *nativeWireConn
	var stopWireWatch func() bool
	var wireWatchJoined <-chan struct{}
	defer func() {
		if stopWireWatch != nil && !stopWireWatch() {
			<-wireWatchJoined
		}
	}()
	dialer := websocket.Dialer{TLSClientConfig: transport.TLSClientConfig.Clone(), Subprotocols: []string{pin.Protocol.WireProtocol()},
		HandshakeTimeout: 10 * time.Second, ReadBufferSize: managedkubernetes.MaximumFrameBytes, WriteBufferSize: managedkubernetes.MaximumFrameBytes,
		NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			connection, err := transport.DialContext(ctx, network, address)
			if err != nil {
				return nil, errNativeStreamUnknown
			}
			// DialContext alone does not interrupt an already connected peer's HTTP upgrade read.
			// DialContext сам не прерывает HTTP upgrade read уже подключённого peer.
			joined := make(chan struct{})
			wireWatchJoined = joined
			stopWireWatch = context.AfterFunc(admissionContext, func() {
				_ = connection.Close()
				close(joined)
			})
			wire = &nativeWireConn{Conn: connection, maximum: maximum*2 + maximumNativeHandshakeBytes}
			return wire, nil
		}}
	endpoint := *request.URL
	endpoint.Scheme = "wss"
	headers := make(http.Header)
	if credential.token != "" {
		headers.Set("Authorization", "Bearer "+credential.token)
	}
	connection, response, err := dialer.DialContext(admissionContext, endpoint.String(), headers)
	if err != nil || attempt.Err() != nil || admissionContext.Err() != nil || !time.Now().Before(admissionUntil) || connection == nil || wire == nil || response == nil || response.StatusCode != http.StatusSwitchingProtocols || connection.Subprotocol() != pin.Protocol.WireProtocol() {
		if connection != nil {
			_ = connection.Close()
		}
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		failure := errNativeStreamUnknown
		if streamContext.Err() != nil {
			failure = streamContext.Err()
		}
		cancel()
		return nil, failure
	}
	handshake, err := observedPodWebSocketHandshake(connection, response.StatusCode, credential.target, pin, operation)
	if err != nil {
		_ = connection.Close()
		cancel()
		return nil, errNativeStreamUnknown
	}
	wire.upgraded.Store(true)
	messageLimit := maximum + 1
	if messageLimit < maximumExecStatusBytes+1 {
		messageLimit = maximumExecStatusBytes + 1
	}
	connection.SetReadLimit(int64(messageLimit))
	if connection.SetReadDeadline(expiresAt) != nil {
		_ = connection.Close()
		cancel()
		return nil, errNativeStreamUnknown
	}
	connection.SetPingHandler(func(data string) error {
		deadline := time.Now().Add(time.Second)
		if expiresAt.Before(deadline) {
			deadline = expiresAt
		}
		return connection.WriteControl(websocket.PongMessage, []byte(data), deadline)
	})
	stream := &nativePodWebSocket{connection: connection, ctx: streamContext, cancel: cancel, joined: make(chan struct{}), handshake: handshake}
	go func() {
		defer close(stream.joined)
		<-streamContext.Done()
		_ = connection.Close()
	}()
	return stream, nil
}

// Close cancels and joins the adapter's only watcher and closes its exact transport.
// It never asserts the remote process stopped or releases a second owner effect attempt.
// Close отменяет и join-ит единственный watcher adapter и закрывает его exact transport.
// Не утверждает remote process stop и не выдаёт вторую owner effect attempt.
func (stream *nativePodWebSocket) Close() error {
	stream.closeOnce.Do(func() { stream.cancel(); _ = stream.connection.Close() })
	<-stream.joined
	return nil
}
