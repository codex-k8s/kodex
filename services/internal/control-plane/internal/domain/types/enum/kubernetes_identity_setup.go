package enum

// KubernetesIdentitySetupState tracks purpose materialization independently from account/RBAC tests.
// UNKNOWN retains a possible external write and permits only separately admitted observation.
// KubernetesIdentitySetupState отслеживает purpose materialization отдельно от account/RBAC tests.
// UNKNOWN сохраняет возможную external write и допускает только отдельно выданный observation.
type KubernetesIdentitySetupState string

const (
	// KubernetesIdentitySetupPrepared has a bounded owner permit and no admitted external write.
	// KubernetesIdentitySetupPrepared имеет bounded owner permit без допущенной external write.
	KubernetesIdentitySetupPrepared KubernetesIdentitySetupState = "PREPARED"
	// KubernetesIdentitySetupAttempting records the sole write attempt before storage credit is released.
	// KubernetesIdentitySetupAttempting сохраняет единственную write attempt до выдачи storage credit.
	KubernetesIdentitySetupAttempting KubernetesIdentitySetupState = "ATTEMPTING"
	// KubernetesIdentitySetupCompleted confirms metadata binding, never target authentication or RBAC.
	// KubernetesIdentitySetupCompleted подтверждает metadata binding, не target authentication либо RBAC.
	KubernetesIdentitySetupCompleted KubernetesIdentitySetupState = "COMPLETED"
	// KubernetesIdentitySetupCancelled permanently closes setup authority after an owner cancellation.
	// KubernetesIdentitySetupCancelled окончательно закрывает setup authority после owner cancellation.
	KubernetesIdentitySetupCancelled KubernetesIdentitySetupState = "CANCELLED"
	// KubernetesIdentitySetupExpired retains history while permanently closing an expired permit.
	// KubernetesIdentitySetupExpired сохраняет историю и окончательно закрывает expired permit.
	KubernetesIdentitySetupExpired KubernetesIdentitySetupState = "EXPIRED"
	// KubernetesIdentitySetupUnknown never authorizes another write for an ambiguous external outcome.
	// KubernetesIdentitySetupUnknown не разрешает другую write при ambiguous external outcome.
	KubernetesIdentitySetupUnknown KubernetesIdentitySetupState = "UNKNOWN"
	// KubernetesIdentitySetupAcknowledgementRejected permanently refuses continuation of an invalid successful ACK.
	// KubernetesIdentitySetupAcknowledgementRejected окончательно запрещает continuation неверного successful ACK.
	KubernetesIdentitySetupAcknowledgementRejected KubernetesIdentitySetupState = "ACK_REJECTED"
)

// Valid excludes absent and future states from owner transition semantics.
// Valid исключает absent и future states из owner transition semantics.
func (state KubernetesIdentitySetupState) Valid() bool {
	return state == KubernetesIdentitySetupPrepared || state == KubernetesIdentitySetupAttempting ||
		state == KubernetesIdentitySetupCompleted || state == KubernetesIdentitySetupCancelled ||
		state == KubernetesIdentitySetupExpired || state == KubernetesIdentitySetupUnknown || state == KubernetesIdentitySetupAcknowledgementRejected
}

// Terminal identifies permanently closed setup states; UNKNOWN remains observation-only until expiry.
// Terminal задаёт окончательно закрытые setup states; UNKNOWN до expiry остаётся observation-only.
func (state KubernetesIdentitySetupState) Terminal() bool {
	return state == KubernetesIdentitySetupCompleted || state == KubernetesIdentitySetupCancelled || state == KubernetesIdentitySetupExpired || state == KubernetesIdentitySetupAcknowledgementRejected
}
