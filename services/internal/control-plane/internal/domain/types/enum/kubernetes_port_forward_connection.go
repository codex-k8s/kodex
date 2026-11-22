package enum

// KubernetesPortForwardConnectionState separates owner closure from native join; no state proves remote TCP delivery or process success.
// KubernetesPortForwardConnectionState отделяет owner closure от native join; ни один state не доказывает remote TCP delivery либо process success.
type KubernetesPortForwardConnectionState string

const (
	// KubernetesPortForwardConnectionPrepared retains one owner-assigned connection and immutable queued OPEN before its native admission.
	// KubernetesPortForwardConnectionPrepared хранит owner-assigned connection и immutable queued OPEN до native admission.
	KubernetesPortForwardConnectionPrepared KubernetesPortForwardConnectionState = "PREPARED"
	// KubernetesPortForwardConnectionOpening records the single admitted attempt before native stream creation.
	// KubernetesPortForwardConnectionOpening фиксирует single admitted attempt до native stream creation.
	KubernetesPortForwardConnectionOpening KubernetesPortForwardConnectionState = "OPENING"
	// KubernetesPortForwardConnectionOpen records a local OPEN acknowledgement without promising remote TCP application success.
	// KubernetesPortForwardConnectionOpen фиксирует local OPEN acknowledgement без обещания remote TCP application success.
	KubernetesPortForwardConnectionOpen KubernetesPortForwardConnectionState = "OPEN"
	// KubernetesPortForwardConnectionClosed prevents further data under owner state; NativeJoined remains an independent receipt fact.
	// KubernetesPortForwardConnectionClosed запрещает дальнейшие data в owner state; NativeJoined остаётся отдельным receipt fact.
	KubernetesPortForwardConnectionClosed KubernetesPortForwardConnectionState = "CLOSED"
	// KubernetesPortForwardConnectionUnknown preserves an uncertain one-attempt tail without permitting another OPEN or payload replay.
	// KubernetesPortForwardConnectionUnknown сохраняет uncertain one-attempt tail без нового OPEN либо payload replay.
	KubernetesPortForwardConnectionUnknown KubernetesPortForwardConnectionState = "UNKNOWN"
	// KubernetesPortForwardConnectionCancelled closes the original owner graph without rewriting known native receipts.
	// KubernetesPortForwardConnectionCancelled закрывает original owner graph без переписывания known native receipts.
	KubernetesPortForwardConnectionCancelled KubernetesPortForwardConnectionState = "CANCELLED"
	// KubernetesPortForwardConnectionExpired retains deadline closure without claiming remote quiescence.
	// KubernetesPortForwardConnectionExpired сохраняет deadline closure без утверждения remote quiescence.
	KubernetesPortForwardConnectionExpired KubernetesPortForwardConnectionState = "EXPIRED"
)

// Known rejects unspecified or future states before they influence a public view or native dispatch.
// Known отклоняет unspecified либо future states до их влияния на public view либо native dispatch.
func (state KubernetesPortForwardConnectionState) Known() bool {
	switch state {
	case KubernetesPortForwardConnectionPrepared, KubernetesPortForwardConnectionOpening, KubernetesPortForwardConnectionOpen, KubernetesPortForwardConnectionClosed, KubernetesPortForwardConnectionUnknown, KubernetesPortForwardConnectionCancelled, KubernetesPortForwardConnectionExpired:
		return true
	default:
		return false
	}
}
