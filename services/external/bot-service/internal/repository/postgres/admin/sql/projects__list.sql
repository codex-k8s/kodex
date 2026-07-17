-- name: projects__list :many
select id, name, slug, mattermost_team_id, mattermost_runs_channel_id, github_account_name, github_owner, github_owner_type, description, advanced_settings::text, created_at, updated_at
from matter_codex_projects
order by updated_at desc, name
limit $1;
