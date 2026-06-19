-- name: agent_role_runtime_variables__delete :one
with deleted as (
	delete from matter_codex_agent_role_runtime_variables
	where role_id = $1 and variable_id = $2
	returning id, role_id, variable_id, created_at
)
select
	deleted.id,
	deleted.role_id,
	r.name as role_name,
	deleted.variable_id,
	v.project_id,
	v.name,
	v.slug,
	v.description,
	v.secret_ref,
	v.secret_key,
	v.sensitive,
	v.enabled,
	deleted.created_at
from deleted
join matter_codex_agent_roles r on r.id = deleted.role_id
join matter_codex_project_runtime_variables v on v.id = deleted.variable_id;
