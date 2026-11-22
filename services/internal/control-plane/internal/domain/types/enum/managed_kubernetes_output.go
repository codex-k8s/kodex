package enum

// ManagedKubernetesOutputFrameState describes private byte durability, never artifact publication or effect success.
// ManagedKubernetesOutputFrameState описывает private byte durability, не artifact publication либо effect success.
type ManagedKubernetesOutputFrameState string

const (
	// KubernetesOutputAttempting retains an unreported one-write intent without permitting another PUT.
	// KubernetesOutputAttempting хранит unreported one-write intent без разрешения повторного PUT.
	KubernetesOutputAttempting ManagedKubernetesOutputFrameState = "ATTEMPTING"
	// KubernetesOutputStaged retains known private content without a currently accepted delivery credit.
	// KubernetesOutputStaged хранит known private content без current accepted delivery credit.
	KubernetesOutputStaged ManagedKubernetesOutputFrameState = "STAGED"
	// KubernetesOutputAccepted proves exact durable bytes under one original ordinal deadline.
	// KubernetesOutputAccepted доказывает exact durable bytes под original ordinal deadline.
	KubernetesOutputAccepted ManagedKubernetesOutputFrameState = "ACCEPTED"
	// KubernetesOutputUnknown retains storage uncertainty without acknowledgement or blind rewrite.
	// KubernetesOutputUnknown сохраняет storage uncertainty без ACK либо blind rewrite.
	KubernetesOutputUnknown ManagedKubernetesOutputFrameState = "UNKNOWN"
	// KubernetesOutputRejected quarantines a damaged successful storage witness permanently.
	// KubernetesOutputRejected окончательно quarantines damaged successful storage witness.
	KubernetesOutputRejected ManagedKubernetesOutputFrameState = "REJECTED"
	// KubernetesOutputCancelled closes byte admission without asserting remote object absence.
	// KubernetesOutputCancelled закрывает byte admission без утверждения remote object absence.
	KubernetesOutputCancelled ManagedKubernetesOutputFrameState = "CANCELLED"
)

// Known rejects omitted and future state values before semantic projection.
// Known отвергает omitted/future state values до semantic projection.
func (state ManagedKubernetesOutputFrameState) Known() bool {
	switch state {
	case KubernetesOutputAttempting, KubernetesOutputStaged, KubernetesOutputAccepted, KubernetesOutputUnknown, KubernetesOutputRejected, KubernetesOutputCancelled:
		return true
	default:
		return false
	}
}
