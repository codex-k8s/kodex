-- name: agent_roles__get_by_project_name :one
select id
from matter_codex_agent_roles
where project_id = $1 and name = $2
