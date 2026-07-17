-- name: cluster_admin_github_credentials__lock :many
select credential.id
from matter_codex_cluster_admin_dependencies dependency
join matter_codex_github_accounts account
	on dependency.resource_type = 'github_account'
	and dependency.resource_key = account.name
join matter_codex_credentials credential on credential.id = account.credential_id
where dependency.role_id = $1
order by credential.id
for share of credential;
