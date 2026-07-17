-- name: cluster_admin_secret_integrity__list :many
select distinct kind, secret_ref, secret_key, content_sha256, resource_uid, resource_version
from (
	select 'openai' as kind, credential.secret_ref, 'auth.json' as secret_key,
		credential.secret_content_sha256 as content_sha256,
		credential.secret_resource_uid as resource_uid,
		credential.secret_resource_version as resource_version
	from matter_codex_agent_roles role
	join matter_codex_openai_accounts account on account.name = role.openai_account_name
	join matter_codex_credentials credential on credential.id = account.credential_id
	where role.id = $1
	union all
	select 'github', credential.secret_ref, 'github-token',
		credential.secret_content_sha256, credential.secret_resource_uid, credential.secret_resource_version
	from matter_codex_cluster_admin_dependencies dependency
	join matter_codex_github_accounts account on account.name = dependency.resource_key
	join matter_codex_credentials credential on credential.id = account.credential_id
	where dependency.role_id = $1 and dependency.resource_type = 'github_account'
	union all
	select 'runtime_variable', variable.secret_ref, variable.secret_key,
		variable.secret_content_sha256, variable.secret_resource_uid, variable.secret_resource_version
	from matter_codex_agent_role_runtime_variables binding
	join matter_codex_project_runtime_variables variable on variable.id = binding.variable_id
	where binding.role_id = $1
	union all
	select 'mattermost_bot', binding.token_secret_ref, 'token',
		binding.secret_content_sha256, binding.secret_resource_uid, binding.secret_resource_version
	from matter_codex_mattermost_bot_identities binding
	where binding.role_id = $1
	union all
	select 'session', session.token_secret_ref, 'token',
		session.secret_content_sha256, session.secret_resource_uid, session.secret_resource_version
	from matter_codex_agent_sessions session
	where session.role_id = $1 and $2 <> '' and session.session_key = $2 and session.token_secret_ref <> ''
) bindings
where secret_ref <> ''
order by kind, secret_ref, secret_key;
