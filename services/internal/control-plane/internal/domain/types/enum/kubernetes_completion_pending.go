package enum

// ManagedKubernetesCompletionPendingReason distinguishes artifact waiting from permission to
// advance an untouched child suffix. It never authorizes target access or an effect replay.
// ManagedKubernetesCompletionPendingReason отличает artifact waiting от допуска следующего
// untouched child suffix. Target access либо effect replay не разрешает.
type ManagedKubernetesCompletionPendingReason string

const (
	// KubernetesCompletionPendingNone carries no artifact wait; ordinary lifecycle rules still apply.
	// KubernetesCompletionPendingNone не обозначает artifact wait; ordinary lifecycle rules сохраняются.
	KubernetesCompletionPendingNone ManagedKubernetesCompletionPendingReason = "NONE"
	// KubernetesCompletionCapturePending permits only exact original-plan metadata polling/finalization.
	// KubernetesCompletionCapturePending допускает только exact original-plan metadata polling/finalization.
	KubernetesCompletionCapturePending ManagedKubernetesCompletionPendingReason = "CAPTURE_PENDING"
)
