package enum

// ManagedKubernetesInputFrameDisposition distinguishes first private delivery from metadata-only uncertainty.
// ManagedKubernetesInputFrameDisposition отличает первую private delivery от metadata-only uncertainty.
type ManagedKubernetesInputFrameDisposition string

const (
	// KubernetesInputFrameNone reports no queued bytes and grants no effect or delivery credit.
	// KubernetesInputFrameNone сообщает отсутствие queued bytes без effect либо delivery credit.
	KubernetesInputFrameNone ManagedKubernetesInputFrameDisposition = "NONE"
	// KubernetesInputFrameDeliver transfers owned bytes only for the durable first claim attempt.
	// KubernetesInputFrameDeliver передаёт owned bytes лишь для durable first claim attempt.
	KubernetesInputFrameDeliver ManagedKubernetesInputFrameDisposition = "DELIVER"
	// KubernetesInputFrameUnknown refuses to repeat bytes after an unreported or ambiguous claim.
	// KubernetesInputFrameUnknown не повторяет bytes после unreported либо ambiguous claim.
	KubernetesInputFrameUnknown ManagedKubernetesInputFrameDisposition = "UNKNOWN"
	// KubernetesInputFrameClosed reports closed owner execution without inventing remote process stop.
	// KubernetesInputFrameClosed сообщает closed owner execution без выдуманного remote process stop.
	KubernetesInputFrameClosed ManagedKubernetesInputFrameDisposition = "CLOSED"
)

// Known excludes omitted and future dispositions before a consumer can use payload or credit.
// Known отвергает omitted/future dispositions до использования payload либо credit.
func (d ManagedKubernetesInputFrameDisposition) Known() bool {
	switch d {
	case KubernetesInputFrameNone, KubernetesInputFrameDeliver, KubernetesInputFrameUnknown, KubernetesInputFrameClosed:
		return true
	}
	return false
}
