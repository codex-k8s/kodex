-- name: agent_role_runtime_variables__list :many
select
	b.id,
	b.role_id,
	r.name as role_name,
	b.variable_id,
	v.project_id,
	v.name,
	v.slug,
	v.description,
	v.secret_ref,
	v.secret_key,
	v.sensitive,
	v.enabled,
	b.created_at
from matter_codex_agent_role_runtime_variables b
join matter_codex_agent_roles r on r.id = b.role_id
join matter_codex_project_runtime_variables v on v.id = b.variable_id
where b.role_id = $1
order by v.name;
