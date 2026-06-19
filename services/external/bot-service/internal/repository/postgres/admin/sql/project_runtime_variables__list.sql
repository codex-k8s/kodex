-- name: project_runtime_variables__list :many
select id, project_id, name, slug, description, secret_ref, secret_key, sensitive, enabled, created_at, updated_at
from matter_codex_project_runtime_variables
where project_id = $1
order by enabled desc, name;
