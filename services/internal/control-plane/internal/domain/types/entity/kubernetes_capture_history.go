package entity

// KubernetesCapturePage exposes safe retained metadata and an opaque continuation, never payload or task credit.
// KubernetesCapturePage выдаёт safe retained metadata и opaque continuation, не payload либо task credit.
type KubernetesCapturePage struct {
	Items []KubernetesCaptureStatus
	Next  string
}
