-- name: cluster_admin_openai_accounts__lock :many
select account.id
from matter_codex_cluster_admin_dependencies dependency
join matter_codex_openai_accounts account
	on dependency.resource_type = 'openai_account'
	and dependency.resource_key = account.name
where dependency.role_id = $1
order by account.id
for share of account;
