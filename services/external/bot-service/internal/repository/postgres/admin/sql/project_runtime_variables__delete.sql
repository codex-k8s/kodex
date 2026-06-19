-- name: project_runtime_variables__delete :one
delete from matter_codex_project_runtime_variables
where id = $1
returning id, project_id, name, slug, description, secret_ref, secret_key, sensitive, enabled, created_at, updated_at;
