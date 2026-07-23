-- name: chats__list :many
select id, project_id, mattermost_channel_id, name, slug, description, chat_type, root_github_issue, work_policy, settings::text, system_purpose, status, archived_at, created_at, updated_at
from matter_codex_chats
where status = 'active'
	and ($1::bigint = 0 or project_id = $1)
order by project_id, updated_at desc, name;
