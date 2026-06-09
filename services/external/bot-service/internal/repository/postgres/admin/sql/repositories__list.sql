-- name: repositories__list :many
select id, provider, owner, name, default_branch, status, mattermost_channel, created_at, updated_at
from matter_codex_repositories
order by updated_at desc, id desc
limit $1::int;
