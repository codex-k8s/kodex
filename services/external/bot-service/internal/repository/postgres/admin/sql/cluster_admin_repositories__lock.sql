-- name: cluster_admin_repositories__lock :many
select repository.id
from matter_codex_cluster_admin_dependencies dependency
join matter_codex_repositories repository
	on dependency.resource_type = 'repository'
	and dependency.resource_key = repository.id::text
where dependency.role_id = $1
order by repository.id
for share of repository;
