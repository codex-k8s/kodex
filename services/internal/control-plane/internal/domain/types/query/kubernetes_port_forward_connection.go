package query

// KubernetesPortForwardConnectionRead selects one exact connection or the original Open idempotency receipt; it never returns private TCP bytes.
// KubernetesPortForwardConnectionRead выбирает exact connection либо original Open idempotency receipt; private TCP bytes не возвращаются.
type KubernetesPortForwardConnectionRead struct{ InvocationRef, StreamRef, ConnectionRef, OpenIdempotencyKey string }

// KubernetesPortForwardConnectionList pages only within one authenticated original stream; no scalar-first connection is selected.
// KubernetesPortForwardConnectionList выполняет paging внутри authenticated original stream; scalar-first connection не выбирается.
type KubernetesPortForwardConnectionList struct {
	InvocationRef, StreamRef string
	Page                     Page
}
