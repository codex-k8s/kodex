-- name: projects__get_by_slug :one
select id, name, slug, mattermost_team_id, github_account_name, description, advanced_settings::text, created_at, updated_at
from matter_codex_projects
where slug = $1;
