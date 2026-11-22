package kubernetestarget

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"math"
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/gorilla/websocket"
)

// observedPodWebSocketHandshake extracts negotiated facts from the actual authenticated TLS connection.
// The partial origin stores only the transport-selected operation and target; owner references are
// added by the orchestration service after it verifies its original immutable effect admission.
// observedPodWebSocketHandshake извлекает negotiated facts из actual authenticated TLS connection.
// Partial origin хранит лишь transport-selected operation и target; owner references добавляет
// orchestration service после проверки original immutable effect admission.
func observedPodWebSocketHandshake(connection *websocket.Conn, status int, target Target, pin PrivilegedTransportPin, operation string) (managedkubernetes.HandshakeReceipt, error) {
	var result managedkubernetes.HandshakeReceipt
	if connection == nil || status != http.StatusSwitchingProtocols || target.Revision > math.MaxInt64 || pin.Generation > math.MaxInt64 {
		return result, errNativeStreamUnknown
	}
	tlsConnection, ok := connection.NetConn().(*tls.Conn)
	if !ok {
		return result, errNativeStreamUnknown
	}
	state := tlsConnection.ConnectionState()
	if !state.HandshakeComplete || len(state.PeerCertificates) == 0 || len(state.VerifiedChains) == 0 {
		return result, errNativeStreamUnknown
	}
	digest := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
	observed := hex.EncodeToString(digest[:])
	if observed != pin.ServerPublicKeyHash || connection.Subprotocol() != pin.Protocol.WireProtocol() {
		return result, errNativeStreamUnknown
	}
	result = managedkubernetes.HandshakeReceipt{Origin: managedkubernetes.StreamReceiptOrigin{Operation: operation, TargetRef: target.ID, TargetRevision: int64(target.Revision)},
		TransportRef: pin.Ref, TransportGeneration: int64(pin.Generation), Protocol: string(pin.Protocol), HTTPStatus: status, NegotiatedProtocol: connection.Subprotocol(), PeerSPKISHA256: observed}
	return result, nil
}

// HandshakeReceipt binds observed TLS/WebSocket facts to a server-owned effect origin, never caller prompts.
// The control-plane independently recomputes its digest and compares the full origin and transport pin.
// HandshakeReceipt связывает observed TLS/WebSocket facts с server-owned effect origin, не caller prompts.
// Control-plane независимо пересчитывает digest и сравнивает full origin и transport pin.
func (stream *nativePodWebSocket) HandshakeReceipt(origin managedkubernetes.StreamReceiptOrigin) (managedkubernetes.HandshakeReceipt, error) {
	if stream == nil || origin.Validate() != nil || origin.Operation != stream.handshake.Origin.Operation || origin.TargetRef != stream.handshake.Origin.TargetRef || origin.TargetRevision != stream.handshake.Origin.TargetRevision {
		return managedkubernetes.HandshakeReceipt{}, errNativeStreamUnknown
	}
	result := stream.handshake
	result.Origin = origin
	if result.Validate() != nil {
		return managedkubernetes.HandshakeReceipt{}, errNativeStreamUnknown
	}
	return result, nil
}
