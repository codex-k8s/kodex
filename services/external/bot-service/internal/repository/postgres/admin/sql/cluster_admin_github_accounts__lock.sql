-- name: cluster_admin_github_accounts__lock :many
select account.id
from matter_codex_cluster_admin_dependencies dependency
join matter_codex_github_accounts account
	on dependency.resource_type = 'github_account'
	and dependency.resource_key = account.name
where dependency.role_id = $1
order by account.id
for share of account;
