-- name: cluster_admin_runtime_variables__lock_frozen :many
select variable_id
from matter_codex_cluster_admin_runtime_variable_bindings
where role_id = $1
order by variable_id;
