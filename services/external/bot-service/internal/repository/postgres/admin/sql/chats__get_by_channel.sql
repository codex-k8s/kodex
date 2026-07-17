-- name: chats__get_by_channel :one
select
	id,
	project_id,
	mattermost_channel_id,
	name,
	slug,
	description,
	chat_type,
	root_github_issue,
	work_policy,
	settings::text,
	system_purpose,
	created_at,
	updated_at
from matter_codex_chats
where mattermost_channel_id = $1
limit 1;
