-- name: agent_role_runtime_variables__upsert :one
with upserted as (
	insert into matter_codex_agent_role_runtime_variables(role_id, variable_id)
	select r.id, v.id
	from matter_codex_agent_roles r
	join matter_codex_project_runtime_variables v on v.id = $2 and v.project_id = r.project_id
	where r.id = $1
	on conflict (role_id, variable_id) do update set
		created_at = matter_codex_agent_role_runtime_variables.created_at
	returning id, role_id, variable_id, created_at, (xmax = 0) as created
)
select
	upserted.id,
	upserted.role_id,
	r.name as role_name,
	upserted.variable_id,
	v.project_id,
	v.name,
	v.slug,
	v.description,
	v.secret_ref,
	v.secret_key,
	v.sensitive,
	v.enabled,
	upserted.created_at,
	upserted.created
from upserted
join matter_codex_agent_roles r on r.id = upserted.role_id
join matter_codex_project_runtime_variables v on v.id = upserted.variable_id;
