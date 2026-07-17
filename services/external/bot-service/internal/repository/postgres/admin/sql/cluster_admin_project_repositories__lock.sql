-- name: cluster_admin_project_repositories__lock :many
select binding.id
from matter_codex_cluster_admin_dependencies dependency
join matter_codex_project_repositories binding
	on dependency.resource_type = 'project_repository'
	and dependency.resource_key = binding.id::text
where dependency.role_id = $1
order by binding.id
for share of binding;
