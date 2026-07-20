-- name: chats__get_by_project_slug_for_update :one
select id, mattermost_channel_id
from matter_codex_chats
where project_id = $1 and slug = $2
for update;
