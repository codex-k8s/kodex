package value

// KubernetesStreamRead owns one bounded received chunk and separately observed terminal evidence.
// A nonnil read error may accompany valid bytes; callers must retain authorized partial output.
// KubernetesStreamRead владеет bounded received chunk и отдельно наблюдённым terminal evidence.
// Read error может сопровождать valid bytes; caller сохраняет разрешённый partial output.
type KubernetesStreamRead struct {
	Channel          string
	Payload          []byte `json:"-"`
	ExitCode         *int32
	CleanEOF         bool
	RemoteStatusCode int
}

// String hides private output from ordinary diagnostics.
// String скрывает private output из обычной диагностики.
func (KubernetesStreamRead) String() string { return "[managed Kubernetes private stream read]" }

// GoString preserves redaction under detailed formatting.
// GoString сохраняет redaction при подробном форматировании.
func (read KubernetesStreamRead) GoString() string { return read.String() }

// KubernetesStreamExchange contains only admitted output copies and authoritative terminal readback.
// KubernetesStreamExchange содержит только admitted output copies и authoritative terminal readback.
type KubernetesStreamExchange struct {
	AcknowledgedInput                uint64
	Output                           []KubernetesStreamFrame
	State, RemoteOutcome, ReasonCode string
}
