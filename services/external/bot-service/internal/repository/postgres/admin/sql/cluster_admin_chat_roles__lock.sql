-- name: cluster_admin_chat_roles__lock :many
select role.id, lower(trim(role.kubernetes_access)) = 'cluster-admin' as cluster_admin
from matter_codex_agent_roles role
where role.project_id = $1
	and role.id = any($2::bigint[])
order by role.id
for share of role;
