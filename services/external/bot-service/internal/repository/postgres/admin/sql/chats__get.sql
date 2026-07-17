-- name: chats__get :one
select id, project_id, mattermost_channel_id, name, slug, description, chat_type, root_github_issue, work_policy, settings::text, system_purpose, created_at, updated_at
from matter_codex_chats
where id = $1;
