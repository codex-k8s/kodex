package enum

// KubernetesToolSafeReasonCode is a closed display-only reason; it carries no native-effect or retry authority.
// KubernetesToolSafeReasonCode — закрытая причина только для отображения; полномочия native effect либо retry отсутствуют.
type KubernetesToolSafeReasonCode uint8

const (
	// KubernetesToolReasonNone represents no registered display reason; arbitrary provider diagnostics remain undisclosed.
	// KubernetesToolReasonNone обозначает отсутствие зарегистрированной display reason; произвольные provider diagnostics не раскрываются.
	KubernetesToolReasonNone KubernetesToolSafeReasonCode = iota
	// KubernetesToolReasonPortForwardProfileUnsupported identifies the exact persisted preflight rejection before any stream exists.
	// KubernetesToolReasonPortForwardProfileUnsupported обозначает exact persisted preflight отказ до создания stream.
	KubernetesToolReasonPortForwardProfileUnsupported
)
