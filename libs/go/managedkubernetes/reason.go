package managedkubernetes

const (
	// ProjectStagingReconciliationReason blocks project deletion while original-root cleanup
	// still needs an immutable provider receipt; it is not a native stream termination reason.
	// ProjectStagingReconciliationReason запрещает удаление проекта, пока original-root cleanup
	// требует immutable provider receipt; это не причина завершения native stream.
	ProjectStagingReconciliationReason = "KUBERNETES_STAGING_RECONCILIATION_REQUIRED"

	// ReasonStagingQuotaExceeded identifies the explicit outstanding-storage quota, not a lifetime frame count.
	// ReasonStagingQuotaExceeded обозначает явную outstanding-storage quota, не lifetime frame count.
	ReasonStagingQuotaExceeded = "KUBERNETES_STAGING_QUOTA_EXCEEDED"
	// ReasonPortForwardProfileUnsupported distinguishes a non-lifecycle transport before any connection admission.
	// ReasonPortForwardProfileUnsupported отличает transport без connection lifecycle до любого admission.
	ReasonPortForwardProfileUnsupported = "KUBERNETES_PORT_FORWARD_PROFILE_UNSUPPORTED"
	// ReasonPortForwardCapacity exposes the simultaneous connection budget without imposing a lifetime count limit.
	// ReasonPortForwardCapacity показывает бюджет simultaneous connections без lifetime count limit.
	ReasonPortForwardCapacity = "KUBERNETES_PORT_FORWARD_CAPACITY_EXHAUSTED"

	// ReasonStreamCanceled reports an acknowledged local stream cancellation.
	// ReasonStreamCanceled сообщает подтверждённую локальную отмену stream.
	ReasonStreamCanceled = "KUBERNETES_STREAM_CANCELED"
	// ReasonGrantRevoked reports current grant revocation.
	// ReasonGrantRevoked сообщает отзыв текущего grant.
	ReasonGrantRevoked = "KUBERNETES_GRANT_REVOKED"
	// ReasonLeaseExpired reports the expired execution lease.
	// ReasonLeaseExpired сообщает истёкший execution lease.
	ReasonLeaseExpired = "KUBERNETES_LEASE_EXPIRED"
	// ReasonSizeLimit reports a bounded stream budget exhaustion.
	// ReasonSizeLimit сообщает исчерпание ограниченного stream budget.
	ReasonSizeLimit = "KUBERNETES_SIZE_LIMIT"
	// ReasonStreamLost reports lost transport or output without inventing an exit.
	// ReasonStreamLost сообщает потерю transport либо output без выдуманного exit.
	ReasonStreamLost = "KUBERNETES_STREAM_LOST"
	// ReasonRemoteFailure records a confirmed non-success remote exit or HTTP stream status.
	// ReasonRemoteFailure фиксирует подтверждённый non-success remote exit либо HTTP stream status.
	ReasonRemoteFailure = "KUBERNETES_REMOTE_FAILURE"
	// ReasonAccessDenied reports an explicit selected-identity authorization refusal.
	// ReasonAccessDenied сообщает явный отказ authorization выбранной identity.
	ReasonAccessDenied = "KUBERNETES_ACCESS_DENIED"
	// ReasonIdentityChanged closes a changed observed cluster/subject boundary.
	// ReasonIdentityChanged закрывает изменившуюся observed cluster/subject boundary.
	ReasonIdentityChanged = "KUBERNETES_IDENTITY_CHANGED"
	// ReasonScopeDenied refuses malformed or foreign immutable operation coordinates.
	// ReasonScopeDenied отклоняет malformed либо foreign immutable operation coordinates.
	ReasonScopeDenied = "KUBERNETES_SCOPE_DENIED"
	// ReasonConflict reports an exact provider concurrency conflict without its raw body.
	// ReasonConflict сообщает exact provider concurrency conflict без raw body.
	ReasonConflict = "KUBERNETES_CONFLICT"
	// ReasonNotFound reports an explicit provider absence response, not automatic create authority.
	// ReasonNotFound сообщает explicit provider absence response, не automatic create authority.
	ReasonNotFound = "KUBERNETES_NOT_FOUND"
	// ReasonTransportUnknown preserves uncertainty after an attempted external operation.
	// ReasonTransportUnknown сохраняет uncertainty после attempted external operation.
	ReasonTransportUnknown = "KUBERNETES_TRANSPORT_UNKNOWN"
	// ReasonTargetUnavailable supplies no permission proof when current target checks are unavailable.
	// ReasonTargetUnavailable не даёт permission proof при unavailable current target checks.
	ReasonTargetUnavailable = "KUBERNETES_TARGET_UNAVAILABLE"
	// ReasonResponseInvalid rejects malformed, unsanitizable or owner-incompatible safe projections.
	// ReasonResponseInvalid отклоняет malformed, unsanitizable либо owner-incompatible projections.
	ReasonResponseInvalid = "KUBERNETES_RESPONSE_INVALID"
	// ReasonResourceUnavailable refuses missing protected material or unusable resource preparation.
	// ReasonResourceUnavailable отклоняет missing protected material/unusable resource preparation.
	ReasonResourceUnavailable = "KUBERNETES_RESOURCE_UNAVAILABLE"
	// ReasonProtectedResource preserves the unconditional credential/host/protected namespace deny.
	// ReasonProtectedResource сохраняет unconditional credential/host/protected namespace deny.
	ReasonProtectedResource = "KUBERNETES_PROTECTED_RESOURCE"
	// ReasonTargetStoragePolicyRequired distinguishes absent qualified storage from target permission.
	// ReasonTargetStoragePolicyRequired отличает absent qualified storage от target permission.
	ReasonTargetStoragePolicyRequired = "KUBERNETES_TARGET_STORAGE_POLICY_REQUIRED"
	// ReasonControllerPolicyRequired requires an explicit operator-qualified custom consumer.
	// ReasonControllerPolicyRequired требует explicit operator-qualified custom consumer.
	ReasonControllerPolicyRequired = "KUBERNETES_CONTROLLER_POLICY_REQUIRED"
	// ReasonControllerRuntimeUnavailable refuses desired-only or incomplete serving evidence.
	// ReasonControllerRuntimeUnavailable отклоняет desired-only/incomplete serving evidence.
	ReasonControllerRuntimeUnavailable = "KUBERNETES_CONTROLLER_RUNTIME_UNAVAILABLE"
)

// ValidReasonCode limits receipts to registered diagnostics instead of raw Kubernetes error bodies.
// The empty success reason is checked separately against the operation outcome by the owner.
// ValidReasonCode ограничивает receipts зарегистрированными diagnostics вместо raw Kubernetes error bodies.
// Empty success reason owner отдельно сопоставляет с operation outcome.
func ValidReasonCode(reason string) bool {
	switch reason {
	case ReasonPortForwardProfileUnsupported, ReasonPortForwardCapacity, ReasonRemoteFailure, ReasonAccessDenied, ReasonIdentityChanged, ReasonScopeDenied,
		ReasonConflict, ReasonNotFound, "KUBERNETES_TIMEOUT",
		ReasonTransportUnknown, ReasonTargetUnavailable, ReasonResponseInvalid,
		ReasonGrantRevoked, ReasonLeaseExpired, ReasonStreamLost,
		ReasonStreamCanceled, ReasonSizeLimit, ReasonProtectedResource,
		"KUBERNETES_PIN_UNAVAILABLE", "KUBERNETES_PROTOCOL_UNSUPPORTED", "KUBERNETES_RECONCILE_REQUIRED",
		ReasonResourceUnavailable, "KUBERNETES_COMMAND_UNSUPPORTED",
		ReasonTargetStoragePolicyRequired, ReasonControllerPolicyRequired, ReasonControllerRuntimeUnavailable, ReasonStagingQuotaExceeded:
		return true
	default:
		return false
	}
}
