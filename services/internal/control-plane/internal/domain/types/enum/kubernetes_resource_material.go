package enum

// KubernetesResourceMaterialState separates private artifact storage from target Secret effects.
// KubernetesResourceMaterialState отделяет private artifact storage от target Secret effects.
type KubernetesResourceMaterialState string

const (
	// KubernetesResourceMaterialPrepared has a bounded upload permit, not a target effect permit.
	// KubernetesResourceMaterialPrepared имеет bounded upload permit, не target effect permit.
	KubernetesResourceMaterialPrepared KubernetesResourceMaterialState = "PREPARED"
	// KubernetesResourceMaterialAttempting records the sole private storage write attempt.
	// KubernetesResourceMaterialAttempting фиксирует единственную private storage write attempt.
	KubernetesResourceMaterialAttempting KubernetesResourceMaterialState = "ATTEMPTING"
	// KubernetesResourceMaterialAvailable is an exact stored artifact, not an executed Secret change.
	// KubernetesResourceMaterialAvailable — exact stored artifact, не executed Secret change.
	KubernetesResourceMaterialAvailable KubernetesResourceMaterialState = "AVAILABLE"
	// KubernetesResourceMaterialUnknown retains an ambiguous write without permitting another attempt.
	// KubernetesResourceMaterialUnknown сохраняет ambiguous write без разрешения другой attempt.
	KubernetesResourceMaterialUnknown KubernetesResourceMaterialState = "UNKNOWN"
	// KubernetesResourceMaterialAcknowledgementRejected quarantines a malformed successful ACK.
	// KubernetesResourceMaterialAcknowledgementRejected изолирует malformed successful ACK.
	KubernetesResourceMaterialAcknowledgementRejected KubernetesResourceMaterialState = "ACK_REJECTED"
	// KubernetesResourceMaterialCancelled permanently closes future material use after owner cancellation.
	// KubernetesResourceMaterialCancelled окончательно закрывает future material use после owner cancellation.
	KubernetesResourceMaterialCancelled KubernetesResourceMaterialState = "CANCELLED"
	// KubernetesResourceMaterialExpired retains immutable history after authority expires.
	// KubernetesResourceMaterialExpired сохраняет immutable history после expiry authority.
	KubernetesResourceMaterialExpired KubernetesResourceMaterialState = "EXPIRED"
	// KubernetesResourceMaterialConsumed cannot be rebound to another operation or destination.
	// KubernetesResourceMaterialConsumed не привязывается заново к другой operation/destination.
	KubernetesResourceMaterialConsumed KubernetesResourceMaterialState = "CONSUMED"
)

// Valid rejects absent/future states before interpreting material availability.
// Valid отклоняет absent/future states до интерпретации material availability.
func (state KubernetesResourceMaterialState) Valid() bool {
	switch state {
	case KubernetesResourceMaterialPrepared, KubernetesResourceMaterialAttempting, KubernetesResourceMaterialAvailable, KubernetesResourceMaterialUnknown,
		KubernetesResourceMaterialAcknowledgementRejected, KubernetesResourceMaterialCancelled, KubernetesResourceMaterialExpired, KubernetesResourceMaterialConsumed:
		return true
	default:
		return false
	}
}

// ValidKubernetesResourceMaterialReasonCode excludes provider diagnostics and future reasons
// from public metadata until the owner and all consumers share an explicit classification.
// ValidKubernetesResourceMaterialReasonCode исключает provider diagnostics/future reasons из
// public metadata до общего explicit classification у owner и всех consumers.
func ValidKubernetesResourceMaterialReasonCode(reason string) bool {
	switch reason {
	case "", "MATERIAL_EXPIRED_PENDING_CLOSURE", "MATERIAL_COMPLETION_REQUIRED", "MATERIAL_ACK_REJECTED", "MATERIAL_ACK_CONFLICT",
		"MATERIAL_SOURCE_CHANGED", "MATERIAL_CANCELLED", "MATERIAL_CONSUMED", "MATERIAL_UNAVAILABLE", "MATERIAL_OBSERVATION_ABSENT":
		return true
	default:
		return false
	}
}
