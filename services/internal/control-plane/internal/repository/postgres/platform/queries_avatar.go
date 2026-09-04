package platform

import _ "embed"

var (
	//go:embed sql/commands_validate_avatar_artifact.sql
	queryCommandsValidateAvatarArtifact string

	//go:embed sql/commands_select_agent_avatar_url.sql
	queryCommandsSelectAgentAvatarURL string

	//go:embed sql/commands_change_agent_avatar_url.sql
	queryCommandsChangeAgentAvatarURL string

	//go:embed sql/commands_sync_agent_avatar.sql
	queryCommandsSyncAgentAvatar string
)
