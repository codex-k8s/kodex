package query

// KubernetesFileCleanupRead selects exact original invocation/source metadata, never a storage object or authority.
// KubernetesFileCleanupRead выбирает exact original invocation/source metadata, не storage object либо authority.
type KubernetesFileCleanupRead struct {
	InvocationRef, SourceRef, CleanupIdempotencyKey string
}
