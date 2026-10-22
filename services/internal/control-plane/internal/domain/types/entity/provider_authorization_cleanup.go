package entity

// ProviderAuthorizationCleanupTarget carries only owner-assigned task, account, authorization and
// materializer attempt references, generation, and protected-broker object metadata. Exact target pins
// constrain cleanup; they do not grant arbitrary Kubernetes object deletion.
// ProviderAuthorizationCleanupTarget передаёт только назначенные владельцем ссылки задачи, аккаунта,
// попыток авторизации и материализации, поколение и метаданные объекта защищённого broker. Точные
// привязки ограничивают очистку и не разрешают произвольное удаление объектов Kubernetes.
type ProviderAuthorizationCleanupTarget struct {
	Recovery                                                             *ProviderCleanupRecoveryIdentity
	TaskRef, AccountRef, AuthorizationAttemptRef, MaterializerAttemptRef string
	Generation                                                           int64
	UID, ResourceVersion                                                 string
	Kind                                                                 string
}

// ProviderCleanupRecoveryIdentity preserves the exact prior task receipt and generation pins used to
// recover an immutable cleanup target. Existing generation metadata must match owner state; it is not
// a caller-selected replacement target.
// ProviderCleanupRecoveryIdentity сохраняет точные квитанцию прежней задачи и привязки поколений для
// восстановления неизменяемой цели очистки. Существующие метаданные поколений сверяются с состоянием
// владельца и не являются выбранной вызывающим кодом подменой цели.
type ProviderCleanupRecoveryIdentity struct {
	TaskRef                          string
	Generation, LegacyLastGeneration int64
}

// ProviderAuthorizationCleanupObservation reports observed cleanup state for the exact target with an
// optional produced-credential descriptor. Observation is distinct from the owner's terminal receipt
// and carries no raw credential bytes.
// ProviderAuthorizationCleanupObservation сообщает наблюдаемое состояние очистки точной цели с
// необязательным дескриптором созданных реквизитов. Наблюдение отличается от конечной квитанции
// владельца и не передаёт исходные байты реквизитов.
type ProviderAuthorizationCleanupObservation struct {
	State              string
	Target             ProviderAuthorizationCleanupTarget
	ProducedCredential *ProviderCredentialDescriptor
}

// ProviderAuthorizationCleanupResult returns a terminal cleanup receipt and optional credential and
// observation readback. A missing terminal receipt is not proof of completed cleanup merely because an
// observation is present.
// ProviderAuthorizationCleanupResult возвращает конечную квитанцию очистки и необязательные сведения
// реквизитов и наблюдения. Отсутствие конечной квитанции не доказывает завершение очистки только из-за
// наличия наблюдения.
type ProviderAuthorizationCleanupResult struct {
	TerminalReceipt    string
	ProducedCredential *ProviderCredentialDescriptor
	Observation        *ProviderAuthorizationCleanupObservation
}
