package entity

// ProviderAuthorizationCleanupTarget содержит только назначенные владельцем
// ссылки и metadata объекта, прочитанные через защищённый broker.
type ProviderAuthorizationCleanupTarget struct {
	Recovery                                                             *ProviderCleanupRecoveryIdentity
	TaskRef, AccountRef, AuthorizationAttemptRef, MaterializerAttemptRef string
	Generation                                                           int64
	UID, ResourceVersion                                                 string
	Kind                                                                 string
}

// ProviderCleanupRecoveryIdentity сохраняет exact receipt прежнего immutable target.
type ProviderCleanupRecoveryIdentity struct {
	TaskRef                          string
	Generation, LegacyLastGeneration int64
}

type ProviderAuthorizationCleanupObservation struct {
	State              string
	Target             ProviderAuthorizationCleanupTarget
	ProducedCredential *ProviderCredentialDescriptor
}

type ProviderAuthorizationCleanupResult struct {
	TerminalReceipt    string
	ProducedCredential *ProviderCredentialDescriptor
	Observation        *ProviderAuthorizationCleanupObservation
}
