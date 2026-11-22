package enum

// SSHIdentitySetupState describes current setup authority separately from immutable material evidence; a closed state never proves that an unacknowledged external create did not happen.
// SSHIdentitySetupState описывает current setup authority отдельно от immutable material evidence; closed state не доказывает, что unacknowledged external create не произошёл.
type SSHIdentitySetupState string

const (
	// SSHIdentitySetupPrepared has one unspent bounded upload permit but no storage grant.
	// SSHIdentitySetupPrepared имеет один unspent bounded upload permit без storage grant.
	SSHIdentitySetupPrepared SSHIdentitySetupState = "PREPARED"
	// SSHIdentitySetupAttempting retains the sole durable material write intent before its outcome is reported.
	// SSHIdentitySetupAttempting сохраняет sole durable material write intent до сообщения outcome.
	SSHIdentitySetupAttempting SSHIdentitySetupState = "ATTEMPTING"
	// SSHIdentitySetupUnknown retains an explicit ambiguous outcome; observation still requires a committed original WRITE completion, a fresh permit and current owner admission.
	// SSHIdentitySetupUnknown сохраняет explicit ambiguous outcome; observation требует committed original WRITE completion, fresh permit и current owner admission.
	SSHIdentitySetupUnknown SSHIdentitySetupState = "UNKNOWN"
	// SSHIdentitySetupUnreported closes a missing-completion attempt without interpreting lease expiry as transport evidence or observation credit.
	// SSHIdentitySetupUnreported закрывает missing-completion attempt, не считая lease expiry transport evidence либо observation credit.
	SSHIdentitySetupUnreported SSHIdentitySetupState = "UNREPORTED"
	// SSHIdentitySetupAcknowledgementRejected preserves a terminal successful-but-damaged response and forbids material recreation or observation under that setup.
	// SSHIdentitySetupAcknowledgementRejected сохраняет terminal successful-but-damaged response и запрещает material recreation либо observation этого setup.
	SSHIdentitySetupAcknowledgementRejected SSHIdentitySetupState = "ACK_REJECTED"
	// SSHIdentitySetupCompleted records metadata attachment, not SSH authentication, operator qualification or permission verification.
	// SSHIdentitySetupCompleted фиксирует metadata attachment, а не SSH authentication, operator qualification либо permission verification.
	SSHIdentitySetupCompleted SSHIdentitySetupState = "COMPLETED"
	// SSHIdentitySetupCancelled closes the owner permit and attempts while retaining late orphan material evidence.
	// SSHIdentitySetupCancelled закрывает owner permit и attempts, сохраняя late orphan material evidence.
	SSHIdentitySetupCancelled SSHIdentitySetupState = "CANCELLED"
	// SSHIdentitySetupExpired closes the original authority deadline without refreshing it on retry or erasing unknown material.
	// SSHIdentitySetupExpired закрывает original authority deadline без обновления при retry либо удаления unknown material.
	SSHIdentitySetupExpired SSHIdentitySetupState = "EXPIRED"
	// SSHIdentitySetupQuarantined blocks the original revision after contradictory material evidence without rewriting its immutable completed history or revoking a newer replacement identity.
	// SSHIdentitySetupQuarantined блокирует original revision после contradictory material evidence без переписи immutable completed history либо отзыва новой replacement identity.
	SSHIdentitySetupQuarantined SSHIdentitySetupState = "QUARANTINED"
)

// Valid rejects unspecified or future state names rather than assigning them existing lifecycle semantics.
// Valid отклоняет unspecified либо future state names, не назначая им existing lifecycle semantics.
func (state SSHIdentitySetupState) Valid() bool {
	switch state {
	case SSHIdentitySetupPrepared, SSHIdentitySetupAttempting, SSHIdentitySetupUnknown, SSHIdentitySetupUnreported, SSHIdentitySetupAcknowledgementRejected, SSHIdentitySetupCompleted, SSHIdentitySetupCancelled, SSHIdentitySetupExpired, SSHIdentitySetupQuarantined:
		return true
	default:
		return false
	}
}

// Terminal closes upload authority only; it does not claim remote quiescence, successful verification or safe deletion of an unknown orphan.
// Terminal закрывает лишь upload authority; remote quiescence, successful verification либо safe deletion unknown orphan он не подтверждает.
func (state SSHIdentitySetupState) Terminal() bool {
	return state.Valid() && state != SSHIdentitySetupPrepared && state != SSHIdentitySetupAttempting && state != SSHIdentitySetupUnknown
}

// SSHIdentityUploadMode is an explicit owner-selected grant branch; an absent or future value never defaults to a material write.
// SSHIdentityUploadMode задаёт explicit owner-selected grant branch; absent либо future value не становятся material write по умолчанию.
type SSHIdentityUploadMode string

const (
	// SSHIdentityUploadWrite spends the sole immutable setup write intent before external storage.
	// SSHIdentityUploadWrite расходует sole immutable setup write intent до external storage.
	SSHIdentityUploadWrite SSHIdentityUploadMode = "WRITE"
	// SSHIdentityUploadObserve permits only observation backed by a durable classified original completion.
	// SSHIdentityUploadObserve допускает только observation с durable classified original completion.
	SSHIdentityUploadObserve SSHIdentityUploadMode = "OBSERVE"
)

// Valid excludes unspecified grant modes from producer and consumer admission.
// Valid исключает unspecified grant modes из producer и consumer admission.
func (mode SSHIdentityUploadMode) Valid() bool {
	return mode == SSHIdentityUploadWrite || mode == SSHIdentityUploadObserve
}
