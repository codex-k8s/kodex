-- name: project_runtime_variables__get :one
select id, project_id, name, slug, description, secret_ref, secret_key, sensitive, enabled, created_at, updated_at
from matter_codex_project_runtime_variables
where id = $1;
