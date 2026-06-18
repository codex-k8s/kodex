select
	id,
	project_id,
	role_id,
	username,
	display_name,
	mattermost_user_id,
	token_secret_ref,
	status,
	last_error,
	created_at,
	updated_at
from matter_codex_mattermost_bot_identities
where mattermost_user_id = $1;
