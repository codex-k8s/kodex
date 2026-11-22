package query

// KubernetesCaptureList selects one exact run/node history page without accepting actor or source authority.
// KubernetesCaptureList выбирает exact run/node history page без caller-assigned actor либо source authority.
type KubernetesCaptureList struct {
	RunRef, NodeRef string
	Page            Page
}
