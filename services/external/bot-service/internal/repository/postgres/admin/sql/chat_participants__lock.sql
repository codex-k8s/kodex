-- name: chat_participants__lock :many
select role_id, enabled
from matter_codex_chat_participants
where chat_id = $1 and role_id = any($2::bigint[])
order by role_id
for update;
