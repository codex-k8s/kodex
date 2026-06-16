-- name: repositories__upsert :one
with upserted as (
	insert into matter_codex_repositories(provider, owner, name, default_branch, github_account_name, mattermost_channel)
	values ($1, $2, $3, $4, $5, $6)
	on conflict (provider, owner, name) do update
	set default_branch = excluded.default_branch,
		github_account_name = excluded.github_account_name,
		mattermost_channel = excluded.mattermost_channel,
		updated_at = now()
	returning id, provider, owner, name, default_branch, github_account_name, status, mattermost_channel, created_at, updated_at, (xmax = 0) as created
)
select id, provider, owner, name, default_branch, github_account_name, status, mattermost_channel, created_at, updated_at, created
from upserted;
