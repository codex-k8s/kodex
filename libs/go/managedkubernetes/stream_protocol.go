package managedkubernetes

// PrivilegedProtocol selects one explicit streaming profile without version or transport fallback.
// PrivilegedProtocol выбирает один явный streaming profile без version/transport fallback.
type PrivilegedProtocol string

const (
	// WebSocketV5 requires the close-capable remote-command WebSocket protocol.
	// WebSocketV5 требует remote-command WebSocket protocol с поддержкой close.
	WebSocketV5 PrivilegedProtocol = "WEBSOCKET_V5"
	// SPDYV4 requires remote-command v4 on a POST upgrade.
	// SPDYV4 требует remote-command v4 при POST upgrade.
	SPDYV4 PrivilegedProtocol = "SPDY_V4"
	// PortForwardWebSocketV4 selects native kubelet port-forward channel framing.
	// PortForwardWebSocketV4 выбирает native kubelet port-forward channel framing.
	PortForwardWebSocketV4 PrivilegedProtocol = "PORT_FORWARD_WEBSOCKET_V4"
	// PortForwardSPDYV1 requires exact per-stream port headers under portforward.k8s.io.
	// PortForwardSPDYV1 требует exact per-stream port headers в portforward.k8s.io.
	PortForwardSPDYV1 PrivilegedProtocol = "PORT_FORWARD_SPDY_V1"
)

// WireProtocol returns one registered negotiation name; unknown profiles have no usable protocol.
// Native kubelet port forwarding is distinct from the API server WebSocket/SPDY tunneling protocol.
// WireProtocol возвращает registered negotiation name; unknown profiles не получают protocol.
// Native kubelet port forwarding отличается от API server WebSocket/SPDY tunneling protocol.
func (protocol PrivilegedProtocol) WireProtocol() string {
	switch protocol {
	case WebSocketV5:
		return "v5.channel.k8s.io"
	case SPDYV4, PortForwardWebSocketV4:
		return "v4.channel.k8s.io"
	case PortForwardSPDYV1:
		return "portforward.k8s.io"
	default:
		return ""
	}
}
