package command

const (
	// PrepareRoleImageGitWriteBack selects preparation of a Git write-back proposal for a role-image
	// recipe. Preparation records the proposed content and source pins; it is not permission to create a
	// remote branch.
	// PrepareRoleImageGitWriteBack выбирает подготовку предложения обратной записи рецепта образа в Git.
	// Подготовка фиксирует предлагаемое содержимое и привязки источника, но не разрешает создание
	// удалённой ветки.
	PrepareRoleImageGitWriteBack Kind = "PREPARE_ROLE_IMAGE_GIT_WRITE_BACK"
	// PrepareIntegrationDefinitionGitWriteBack selects preparation of a Git write-back proposal for an
	// integration definition. The proposal retains that definition's source and version boundary rather
	// than enabling arbitrary repository writes.
	// PrepareIntegrationDefinitionGitWriteBack выбирает подготовку предложения обратной записи определения
	// интеграции в Git. Предложение сохраняет область источника и версии этого определения, а не разрешает
	// произвольную запись в репозиторий.
	PrepareIntegrationDefinitionGitWriteBack Kind = "PREPARE_INTEGRATION_DEFINITION_GIT_WRITE_BACK"
	// ApproveManagedConfigurationGitWriteBack selects approval of the exact managed write-back proposal
	// and approval digest. The owner verifies the decision and current source pins before making work
	// eligible.
	// ApproveManagedConfigurationGitWriteBack выбирает подтверждение точного предложения управляемой
	// обратной записи и дайджеста подтверждения. Владелец проверяет решение и текущие привязки источника
	// перед допуском работы.
	ApproveManagedConfigurationGitWriteBack Kind = "APPROVE_MANAGED_CONFIGURATION_GIT_WRITE_BACK"
	// RejectManagedConfigurationGitWriteBack selects rejection of a write-back proposal. It is a lifecycle
	// decision on the stored proposal, not an instruction to alter or delete remote repository history.
	// RejectManagedConfigurationGitWriteBack выбирает отклонение предложения обратной записи. Это решение
	// жизненного цикла сохранённого предложения, а не указание изменить или удалить историю удалённого
	// репозитория.
	RejectManagedConfigurationGitWriteBack Kind = "REJECT_MANAGED_CONFIGURATION_GIT_WRITE_BACK"
	// CancelManagedConfigurationGitWriteBack selects cancellation of stored write-back work subject to
	// owner lifecycle checks. Cancellation must not be interpreted as proof that an already-started
	// external effect did not occur.
	// CancelManagedConfigurationGitWriteBack выбирает отмену сохранённой работы обратной записи с
	// проверками жизненного цикла владельцем. Отмена не должна считаться доказательством отсутствия уже
	// начатого внешнего эффекта.
	CancelManagedConfigurationGitWriteBack Kind = "CANCEL_MANAGED_CONFIGURATION_GIT_WRITE_BACK"
)

// ConfigurationWriteBackInput pins a configuration or proposal, expected source version, proposed
// content, and approval digest. Which fields are required depends on the specialized command; the
// digest alone is not an authenticated decision.
// ConfigurationWriteBackInput закрепляет конфигурацию или предложение, ожидаемую версию источника,
// предлагаемое содержимое и дайджест подтверждения. Обязательные поля определяет специализированная
// команда; дайджест сам по себе не является удостоверенным решением.
type ConfigurationWriteBackInput struct {
	ConfigurationRef, ProposalRef, Content, ApprovalDigest string
	ExpectedSourceVersion                                  int64
}
