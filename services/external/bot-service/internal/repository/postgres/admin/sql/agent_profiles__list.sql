-- name: agent_profiles__list :many
select id, name, role, description, enabled, created_at, updated_at
from matter_codex_agent_profiles
order by role, name;
