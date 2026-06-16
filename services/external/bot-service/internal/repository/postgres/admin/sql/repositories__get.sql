-- name: repositories__get :one
select id, provider, owner, name, default_branch, github_account_name, status, mattermost_channel, created_at, updated_at
from matter_codex_repositories
where provider = $1
	and owner = $2
	and name = $3;
