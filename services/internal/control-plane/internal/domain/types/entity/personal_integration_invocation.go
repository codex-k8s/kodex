package entity

import "time"

// PersonalIntegrationInvocationAdmissionResult carries exact descriptor metadata only after commit.
// AuthorizedUntil bounds new dispatch/disclosure separately from the existing completion lease.
// A repeated or uncertain admission must not return another authorization for external work.
// PersonalIntegrationInvocationAdmissionResult передаёт exact descriptor metadata лишь после commit.
// AuthorizedUntil ограничивает новый dispatch/disclosure отдельно от прежнего completion lease.
// Повторный либо неопределённый допуск не выдаёт нового разрешения внешней работы.
type PersonalIntegrationInvocationAdmissionResult struct {
	AuthorizedUntil           time.Time                      `json:"-"`
	ResourcePermissionProfile string                         `json:"-"`
	Credential                *IntegrationCredentialRevision `json:"-"`
	RecoveryReadOnly          bool                           `json:"-"`
}
