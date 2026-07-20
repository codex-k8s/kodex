-- name: cluster_admin_dependencies__lock_frozen :many
select role_id
from matter_codex_cluster_admin_dependencies
where role_id = $1
order by resource_type, resource_key
for share;
