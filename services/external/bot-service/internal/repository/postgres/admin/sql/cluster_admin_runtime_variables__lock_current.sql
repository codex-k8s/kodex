-- name: cluster_admin_runtime_variables__lock_current :many
select role_variable.id, variable.id
from matter_codex_agent_role_runtime_variables role_variable
join matter_codex_project_runtime_variables variable on variable.id = role_variable.variable_id
where role_variable.role_id = $1
order by variable.id
for share of role_variable, variable;
