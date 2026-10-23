package platform

import _ "embed"

var (
	// queryCommandsValidateAvatarArtifact checks a project artifact for ACTIVE, CLEAN, preview-ready image type and size before avatar use, sharing the artifact-head lock.
	// queryCommandsValidateAvatarArtifact проверяет артефакт проекта на ACTIVE, CLEAN, готовность предпросмотра, тип и размер изображения перед аватаром, разделяя блокировку головы артефакта.
	//go:embed sql/commands_validate_avatar_artifact.sql
	queryCommandsValidateAvatarArtifact string

	// queryCommandsSelectAgentAvatarURL reads the stored avatar URL of one agent by organization/ref without changing its artifact binding.
	// queryCommandsSelectAgentAvatarURL читает сохранённый URL аватара одного агента по организации и ссылке без изменения привязки артефакта.
	//go:embed sql/commands_select_agent_avatar_url.sql
	queryCommandsSelectAgentAvatarURL string

	// queryCommandsChangeAgentAvatarURL changes the avatar URL of a nonsystem agent at expected version and returns its updated view.
	// queryCommandsChangeAgentAvatarURL меняет URL аватара несистемного агента при ожидаемой версии и возвращает обновлённое представление.
	//go:embed sql/commands_change_agent_avatar_url.sql
	queryCommandsChangeAgentAvatarURL string

	// queryCommandsSyncAgentAvatar synchronizes one nonsystem agent avatar binding with a validated unshared image, retiring an unshared old artifact and returning the new ref/revision.
	// queryCommandsSyncAgentAvatar синхронизирует привязку аватара несистемного агента с проверенным изображением без чужих привязок, удаляя прежний непривязанный артефакт и возвращая новую ссылку и ревизию.
	//go:embed sql/commands_sync_agent_avatar.sql
	queryCommandsSyncAgentAvatar string
)
