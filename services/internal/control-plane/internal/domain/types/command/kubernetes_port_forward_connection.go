package command

// KubernetesPortForwardConnectionOpen requests one approved slot; the owner assigns connection ref/generation and the exact queued OPEN bytes.
// KubernetesPortForwardConnectionOpen запрашивает approved slot; owner назначает connection ref/generation и exact queued OPEN bytes.
type KubernetesPortForwardConnectionOpen struct {
	InvocationRef, StreamRef string
	Slot                     uint32
}

// KubernetesPortForwardConnectionClose selects an existing owner connection for closure without accepting a fence, target or remote TCP address.
// KubernetesPortForwardConnectionClose выбирает existing owner connection для closure без fence, target либо remote TCP address.
type KubernetesPortForwardConnectionClose struct{ InvocationRef, StreamRef, ConnectionRef string }
