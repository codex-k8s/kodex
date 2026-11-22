package command

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

const (
	// CreatePersonalEmailMailboxConnection creates one self-owned fixed email connection and its exact metadata-only resource access, without project or agent grants.
	// CreatePersonalEmailMailboxConnection создаёт одно собственное фиксированное email-подключение и точный metadata-only resource access без project либо agent grants.
	CreatePersonalEmailMailboxConnection Kind = "CREATE_PERSONAL_EMAIL_MAILBOX_CONNECTION"
	// ConfigureEmailMailboxOwnership explicitly chooses personal self-ownership or a resolved service identity and fences every previous credential generation.
	// ConfigureEmailMailboxOwnership явно выбирает личное владение текущего пользователя либо разрешённую service identity и ограждает прежние поколения credentials.
	ConfigureEmailMailboxOwnership Kind = "CONFIGURE_EMAIL_MAILBOX_OWNERSHIP"
	// RevokeEmailMailboxOwnership stops new mailbox effects without deleting descriptor, invocation, consent or uncertain effect history.
	// RevokeEmailMailboxOwnership запрещает новые mailbox effects без удаления истории дескрипторов, вызовов, согласий и неопределённых эффектов.
	RevokeEmailMailboxOwnership Kind = "REVOKE_EMAIL_MAILBOX_OWNERSHIP"
)

// PersonalEmailMailboxConnectionInput contains editable display name and sender only; connection, mailbox placeholder, tenant, owner, package and ACL scope are assigned by the server.
// PersonalEmailMailboxConnectionInput содержит только редактируемые имя и sender; connection, mailbox placeholder, tenant, owner, package и ACL scope назначаются сервером.
type PersonalEmailMailboxConnectionInput struct {
	Name, Sender string
}

// EmailMailboxOwnershipInput contains only the connection locator and an explicit policy/service choice; personal subject and tenant are always resolved by the server.
// EmailMailboxOwnershipInput содержит только locator подключения и явный выбор policy/service; личный subject и tenant всегда разрешаются сервером.
type EmailMailboxOwnershipInput struct {
	ConnectionRef, ServiceSubjectRef string
	Policy                           entity.IntegrationCredentialPolicy
}
