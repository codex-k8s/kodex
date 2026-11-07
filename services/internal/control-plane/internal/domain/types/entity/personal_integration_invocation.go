package entity

// PersonalIntegrationInvocationAdmissionResult carries exact descriptor metadata only after commit.
// A repeated or uncertain admission must not return another authorization for external work.
// PersonalIntegrationInvocationAdmissionResult передаёт exact descriptor metadata лишь после commit.
// Повторный либо неопределённый допуск не выдаёт нового разрешения внешней работы.
type PersonalIntegrationInvocationAdmissionResult struct {
	ResourcePermissionProfile string                         `json:"-"`
	Credential                *IntegrationCredentialRevision `json:"-"`
	RecoveryReadOnly          bool                           `json:"-"`
}
