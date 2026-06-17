-- name: chat_participants__list :many
select cp.id, cp.chat_id, cp.role_id, ar.name, cp.enabled, cp.created_at
from matter_codex_chat_participants cp
join matter_codex_agent_roles ar on ar.id = cp.role_id
where cp.chat_id = $1
order by ar.role_type, ar.name;
