-- name: repositories__upsert :one
with upserted as (
	insert into matter_codex_repositories(provider, owner, name, default_branch, mattermost_channel)
	values ($1, $2, $3, $4, $5)
	on conflict (provider, owner, name) do update
	set default_branch = excluded.default_branch,
		mattermost_channel = excluded.mattermost_channel,
		updated_at = now()
	returning id, provider, owner, name, default_branch, status, mattermost_channel, created_at, updated_at, (xmax = 0) as created
)
select id, provider, owner, name, default_branch, status, mattermost_channel, created_at, updated_at, created
from upserted;
