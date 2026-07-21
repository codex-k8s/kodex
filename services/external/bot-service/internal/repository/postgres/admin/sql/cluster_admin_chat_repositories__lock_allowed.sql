-- name: cluster_admin_chat_repositories__lock_allowed :many
select dependency.role_id, binding.repository_id
from matter_codex_cluster_admin_dependencies dependency
join matter_codex_chat_repositories binding
	on dependency.resource_type = 'chat_repository'
	and dependency.resource_key = binding.id::text
where dependency.role_id = any($1::bigint[])
	and binding.chat_id = $2
order by dependency.role_id, binding.repository_id
for share of binding;
