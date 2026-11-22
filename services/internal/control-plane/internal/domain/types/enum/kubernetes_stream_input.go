package enum

// KubernetesStreamInputSubmissionState describes one private input's owner-assigned delivery facts, not remote execution success.
// KubernetesStreamInputSubmissionState описывает назначенные owner факты доставки одного private input, не успех remote execution.
type KubernetesStreamInputSubmissionState string

const (
	// KubernetesInputQueued retains validated private bytes awaiting one exact original-claim delivery.
	// KubernetesInputQueued хранит проверенные private bytes до одной exact original-claim delivery.
	KubernetesInputQueued KubernetesStreamInputSubmissionState = "QUEUED"
	// KubernetesInputClaimed records a durable one-attempt intent before bytes leave owner storage.
	// KubernetesInputClaimed фиксирует durable one-attempt intent до выдачи bytes из owner storage.
	KubernetesInputClaimed KubernetesStreamInputSubmissionState = "CLAIMED"
	// KubernetesInputAcknowledged records joined local transport acceptance without asserting remote command effects.
	// KubernetesInputAcknowledged фиксирует joined local transport acceptance без утверждения remote command effects.
	KubernetesInputAcknowledged KubernetesStreamInputSubmissionState = "ACKNOWLEDGED"
	// KubernetesInputUnknown preserves uncertainty and permanently refuses automatic payload redelivery.
	// KubernetesInputUnknown сохраняет uncertainty и запрещает automatic payload redelivery.
	KubernetesInputUnknown KubernetesStreamInputSubmissionState = "UNKNOWN"
	// KubernetesInputRejected closes malformed or ineligible stored input without a new effect attempt.
	// KubernetesInputRejected закрывает malformed либо ineligible stored input без новой effect attempt.
	KubernetesInputRejected KubernetesStreamInputSubmissionState = "REJECTED"
	// KubernetesInputCancelled closes future delivery while retaining earlier unknown or acknowledged facts.
	// KubernetesInputCancelled закрывает future delivery с сохранением прежних unknown либо acknowledged facts.
	KubernetesInputCancelled KubernetesStreamInputSubmissionState = "CANCELLED"
)

// Known rejects missing and future states before public metadata is used.
// Known отвергает отсутствующие и future states до использования public metadata.
func (state KubernetesStreamInputSubmissionState) Known() bool {
	switch state {
	case KubernetesInputQueued, KubernetesInputClaimed, KubernetesInputAcknowledged, KubernetesInputUnknown, KubernetesInputRejected, KubernetesInputCancelled:
		return true
	default:
		return false
	}
}
